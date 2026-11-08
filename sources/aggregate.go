package sources

import (
	"encoding/json"
	"math/bits"
	"slices"
	"time"
)

type Options struct {
	MaxAge time.Duration
	// MaxDeviationBps rejects quotes further than this from the median, in
	// basis points (500 = 5%).
	MaxDeviationBps uint64
	ExcludeQuotes   []string
	ExcludeBySource map[string][]string
	ARSGap          *Gap
	Now             func() time.Time
}

// Haveno's defaults, plus a 5% consensus band.
func (o Options) withDefaults() Options {
	if o.MaxAge <= 0 {
		o.MaxAge = 10 * time.Minute
	}
	if o.MaxDeviationBps == 0 {
		o.MaxDeviationBps = 500
	}
	if o.ExcludeQuotes == nil {
		o.ExcludeQuotes = []string{"LBP"}
	}
	if o.ExcludeBySource == nil {
		o.ExcludeBySource = map[string][]string{"kraken": {"AED"}, "binance": {"XMR"}}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

func (o Options) excluded(r Rate) bool {
	if slices.Contains(o.ExcludeQuotes, r.Quote) {
		return true
	}
	name := r.Source
	for i := range len(name) {
		if name[i] == ':' {
			name = name[:i]
			break
		}
	}
	list := o.ExcludeBySource[name]
	return slices.Contains(list, r.Base) || slices.Contains(list, r.Quote)
}

// Price is the consensus price of 1 XMR in Quote.
type Price struct {
	Quote    string
	Value    uint64
	Time     time.Time
	Sources  []string
	Rejected []string
}

func (p Price) String() string { return FormatDecimal(p.Value, Scale(p.Quote)) }

func (p Price) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Quote    string    `json:"quote"`
		Price    string    `json:"price"`
		Time     time.Time `json:"time"`
		Sources  []string  `json:"sources"`
		Rejected []string  `json:"rejected,omitempty"`
	}{p.Quote, p.String(), p.Time, p.Sources, p.Rejected})
}

type vote struct {
	source string
	value  uint64
	time   time.Time
}

// Aggregate turns rates into one price per quote currency. Every source
// votes: direct XMR markets with their own price, BTC markets through the
// XMR/BTC consensus. Votes too far from the median are rejected as bad or
// malicious, and the rest are averaged.
func Aggregate(rates []Rate, opt Options) map[string]Price {
	opt = opt.withDefaults()
	now := opt.Now()

	direct := map[string][]vote{}
	viaBTC := map[string][]vote{}
	for _, r := range rates {
		if now.Sub(r.Time) > opt.MaxAge || opt.excluded(r) {
			continue
		}
		if r.Quote == "ARS" && opt.ARSGap != nil && !r.Blue {
			p, ok := mulDiv(r.Price, opt.ARSGap.Blue, opt.ARSGap.Official)
			if !ok {
				continue
			}
			r.Price = p
		}
		v := vote{source: r.Source, value: r.Price, time: r.Time}
		switch r.Base {
		case "XMR":
			direct[r.Quote] = append(direct[r.Quote], v)
		case "BTC":
			viaBTC[r.Quote] = append(viaBTC[r.Quote], v)
		}
	}

	out := map[string]Price{}
	xmrBTC, haveBTC := consensus("BTC", direct["BTC"], opt.MaxDeviationBps)
	if haveBTC {
		out["BTC"] = xmrBTC
	}
	quotes := map[string]bool{}
	for quote := range direct {
		quotes[quote] = quote != "BTC"
	}
	if haveBTC {
		for quote := range viaBTC {
			quotes[quote] = true
		}
	}
	for quote, fiat := range quotes {
		if !fiat {
			continue
		}
		votes := slices.Clone(direct[quote])
		if haveBTC {
			for _, v := range viaBTC[quote] {
				if p, ok := mulDiv(xmrBTC.Value, v.value, pow10(BTCScale)); ok {
					votes = append(votes, vote{source: v.source + "/btc", value: p, time: earliest(v.time, xmrBTC.Time)})
				}
			}
		}
		if p, ok := consensus(quote, votes, opt.MaxDeviationBps); ok {
			out[quote] = p
		}
	}
	return out
}

func consensus(quote string, votes []vote, maxBps uint64) (Price, bool) {
	if len(votes) == 0 {
		return Price{}, false
	}
	values := make([]uint64, len(votes))
	for i, v := range votes {
		values[i] = v.value
	}
	slices.Sort(values)
	mid := len(values) / 2
	median := values[mid]
	if len(values)%2 == 0 {
		a, b := values[mid-1], values[mid]
		median = a/2 + b/2 + (a%2+b%2)/2
	}

	p := Price{Quote: quote}
	var sum, carry uint64
	for _, v := range votes {
		if !within(v.value, median, maxBps) {
			p.Rejected = append(p.Rejected, v.source)
			continue
		}
		sum, carry = bits.Add64(sum, v.value, 0)
		if carry != 0 {
			return Price{}, false
		}
		p.Sources = append(p.Sources, v.source)
		if v.time.After(p.Time) {
			p.Time = v.time
		}
	}
	n := uint64(len(p.Sources))
	if n == 0 {
		return Price{}, false
	}
	p.Value = sum / n
	if rem := sum % n; rem >= n-rem {
		p.Value++
	}
	slices.Sort(p.Sources)
	slices.Sort(p.Rejected)
	return p, true
}

// within reports |v-ref|/ref <= bps/10000 without overflow.
func within(v, ref, bps uint64) bool {
	diff := v - ref
	if v < ref {
		diff = ref - v
	}
	hiA, loA := bits.Mul64(diff, 10000)
	hiB, loB := bits.Mul64(ref, bps)
	return hiA < hiB || (hiA == hiB && loA <= loB)
}

func earliest(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
