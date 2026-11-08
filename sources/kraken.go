package sources

import (
	"context"
	"errors"
	"strings"
)

// Kraken: XMR/USD, XMR/EUR, XMR/BTC and BTC/fiat, all pairs in one call.
type Kraken struct {
	BaseURL string
	Client  HTTPClient
}

func (Kraken) Name() string { return "kraken" }

func (k Kraken) Fetch(ctx context.Context) (rates []Rate, err error) {
	var resp struct {
		Error  []string `json:"error"`
		Result map[string]struct {
			Ask  []num `json:"a"`
			Bid  []num `json:"b"`
			Last []num `json:"c"`
		} `json:"result"`
	}
	url := urlOr(k.BaseURL, "https://api.kraken.com") + "/0/public/Ticker"
	if err = getJSON(ctx, k.Client, url, nil, &resp); err != nil {
		return nil, err
	}
	if len(resp.Error) > 0 {
		return nil, errors.New("kraken: " + strings.Join(resp.Error, "; "))
	}
	for key, t := range resp.Result {
		base, quote, ok := krakenPair(key)
		if !ok || len(t.Ask) == 0 || len(t.Bid) == 0 || len(t.Last) == 0 {
			continue
		}
		rates = add(rates, k.Name(), base, quote, t.Bid[0], t.Ask[0], t.Last[0], zeroTime)
	}
	return rates, nil
}

// krakenPair splits legacy names (XXMRZUSD, XXMRXXBT) and newer ones (XBTCHF).
func krakenPair(key string) (base, quote string, ok bool) {
	if strings.ContainsAny(key, "./") {
		return "", "", false
	}
	for _, prefix := range []string{"XXBT", "XXMR", "XBT", "XMR"} {
		rest, found := strings.CutPrefix(key, prefix)
		if !found {
			continue
		}
		if len(rest) == 4 && (rest[0] == 'Z' || rest[0] == 'X') {
			rest = rest[1:]
		}
		return prefix[len(prefix)-3:], rest, true
	}
	return "", "", false
}
