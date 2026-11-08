package sources

import (
	"context"
	"encoding/json"
	"strings"
)

// Bitfinex: XMR/USD, XMR/BTC and BTC/fiat.
type Bitfinex struct {
	BaseURL string
	Client  HTTPClient
}

func (Bitfinex) Name() string { return "bitfinex" }

// Rows are [SYMBOL, BID, BID_SIZE, ASK, ASK_SIZE, CHANGE, CHANGE_REL, LAST, ...].
func (b Bitfinex) Fetch(ctx context.Context) (rates []Rate, err error) {
	var rows [][]json.RawMessage
	url := urlOr(b.BaseURL, "https://api-pub.bitfinex.com") + "/v2/tickers?symbols=ALL"
	if err = getJSON(ctx, b.Client, url, nil, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if len(row) < 8 {
			continue
		}
		var symbol string
		if json.Unmarshal(row[0], &symbol) != nil || !strings.HasPrefix(symbol, "t") {
			continue
		}
		base, quote, ok := bitfinexPair(symbol[1:])
		if !ok {
			continue
		}
		rates = add(rates, b.Name(), base, quote, rawNum(row[1]), rawNum(row[3]), rawNum(row[7]), zeroTime)
	}
	return rates, nil
}

func rawNum(raw json.RawMessage) (n num) {
	_ = n.UnmarshalJSON(raw)
	return n
}

func bitfinexPair(s string) (base, quote string, ok bool) {
	if base, quote, ok = strings.Cut(s, ":"); ok {
		return base, quote, true
	}
	if len(s) == 6 {
		return s[:3], s[3:], true
	}
	return "", "", false
}
