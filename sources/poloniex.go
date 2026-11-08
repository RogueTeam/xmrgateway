package sources

import (
	"context"
	"strings"
)

// Poloniex: XMR/BTC, last trade only.
type Poloniex struct {
	BaseURL string
	Client  HTTPClient
}

func (Poloniex) Name() string { return "poloniex" }

func (p Poloniex) Fetch(ctx context.Context) (rates []Rate, err error) {
	var resp []struct {
		Symbol string `json:"symbol"`
		Price  num    `json:"price"`
		Ts     int64  `json:"ts"`
	}
	url := urlOr(p.BaseURL, "https://api.poloniex.com") + "/markets/price"
	if err = getJSON(ctx, p.Client, url, nil, &resp); err != nil {
		return nil, err
	}
	for _, t := range resp {
		if base, quote, ok := strings.Cut(t.Symbol, "_"); ok {
			rates = add(rates, p.Name(), base, quote, "", "", t.Price, unixMilli(t.Ts))
		}
	}
	return rates, nil
}
