// Package sources implements a client for every price source used by the
// Haveno price node and combines their quotes into one XMR price per currency.
//
// Clients keep only XMR/<fiat>, XMR/BTC and BTC/<fiat>; the last two derive
// XMR/<fiat> where no direct market exists.
package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Rate is one quote: 1 Base = Price/10^Scale(Quote) Quote.
type Rate struct {
	Base   string
	Quote  string
	Price  uint64
	Time   time.Time
	Source string
	Blue   bool // ARS quoted at the free-market rate
}

type Source interface {
	Name() string
	Fetch(ctx context.Context) ([]Rate, error)
}

// BlueRateSource marks sources whose ARS prices already use the blue rate.
type BlueRateSource interface {
	BlueRate() bool
}

// HTTPClient is satisfied by *http.Client.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

var UserAgent = "xmrgateway"

var zeroTime time.Time

const maxBody = 8 << 20

var defaultClient HTTPClient = &http.Client{Timeout: 30 * time.Second}

func clientOr(c HTTPClient) HTTPClient {
	if c != nil {
		return c
	}
	return defaultClient
}

func urlOr(base, def string) string {
	if base != "" {
		return strings.TrimRight(base, "/")
	}
	return def
}

func getJSON(ctx context.Context, c HTTPClient, url string, header http.Header, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := clientOr(c).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body := io.LimitReader(resp.Body, maxBody)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(body, 256))
		return fmt.Errorf("GET %s: %s: %s", url, resp.Status, strings.TrimSpace(string(snippet)))
	}
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	return nil
}

// num keeps a JSON number or numeric string as text, so prices are parsed
// into fixed point without passing through float64.
type num string

func (n *num) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" {
		s = ""
	}
	*n = num(s)
	return nil
}

func (n num) fixed(scale int) (uint64, bool) {
	v, err := ParseDecimal(string(n), scale)
	return v, err == nil && v > 0
}

// quote picks the bid/ask midpoint, falling back to the last trade.
func quote(scale int, bid, ask, last num) (uint64, bool) {
	b, okB := bid.fixed(scale)
	a, okA := ask.fixed(scale)
	if okB && okA {
		return b/2 + a/2 + (b%2+a%2)/2, true
	}
	return last.fixed(scale)
}

func normalize(code string) string {
	code = strings.ToUpper(code)
	if code == "XBT" {
		return "BTC"
	}
	return code
}

func wanted(base, quote string) bool {
	switch base {
	case "XMR":
		return quote == "BTC" || IsFiat(quote)
	case "BTC":
		return IsFiat(quote)
	}
	return false
}

func add(rates []Rate, source, base, quoteCode string, bid, ask, last num, t time.Time) []Rate {
	base, quoteCode = normalize(base), normalize(quoteCode)
	if !wanted(base, quoteCode) {
		return rates
	}
	p, ok := quote(Scale(quoteCode), bid, ask, last)
	if !ok {
		return rates
	}
	return addFixed(rates, source, base, quoteCode, p, t)
}

func addFixed(rates []Rate, source, base, quoteCode string, price uint64, t time.Time) []Rate {
	if !wanted(base, quoteCode) || price == 0 {
		return rates
	}
	if t.IsZero() {
		t = time.Now()
	}
	return append(rates, Rate{Base: base, Quote: quoteCode, Price: price, Time: t, Source: source})
}

func unixSec(s int64) time.Time {
	if s <= 0 {
		return time.Time{}
	}
	return time.Unix(s, 0)
}

func unixMilli(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

type Result struct {
	Source string
	Rates  []Rate
	Err    error
	Took   time.Duration
}

// FetchAll queries all sources concurrently, each with its own timeout.
func FetchAll(ctx context.Context, timeout time.Duration, srcs ...Source) []Result {
	results := make([]Result, len(srcs))
	var wg sync.WaitGroup
	for i, s := range srcs {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			start := time.Now()
			rates, err := s.Fetch(ctx)
			if err == nil && len(rates) == 0 {
				err = errors.New("no usable rates")
			}
			if b, ok := s.(BlueRateSource); ok && b.BlueRate() {
				for j := range rates {
					rates[j].Blue = true
				}
			}
			results[i] = Result{Source: s.Name(), Rates: rates, Err: err, Took: time.Since(start)}
		})
	}
	wg.Wait()
	return results
}

func AllRates(results []Result) (rates []Rate) {
	for _, r := range results {
		rates = append(rates, r.Rates...)
	}
	return rates
}
