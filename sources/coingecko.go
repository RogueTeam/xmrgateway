package sources

import (
	"context"
	"net/http"
	"strings"
)

// CoinGecko: BTC against about 60 fiat currencies in one call.
// APIKey is an optional free "demo" key for a higher rate limit.
type CoinGecko struct {
	BaseURL string
	Client  HTTPClient
	APIKey  string
}

func (CoinGecko) Name() string { return "coingecko" }

func (c CoinGecko) Fetch(ctx context.Context) (rates []Rate, err error) {
	var resp struct {
		Rates map[string]struct {
			Value num    `json:"value"`
			Type  string `json:"type"`
		} `json:"rates"`
	}
	var header http.Header
	if c.APIKey != "" {
		header = http.Header{"X-Cg-Demo-Api-Key": {c.APIKey}}
	}
	url := urlOr(c.BaseURL, "https://api.coingecko.com") + "/api/v3/exchange_rates"
	if err = getJSON(ctx, c.Client, url, header, &resp); err != nil {
		return nil, err
	}
	for code, r := range resp.Rates {
		code = strings.ToUpper(code)
		switch {
		case r.Type == "fiat":
			rates = add(rates, c.Name(), "BTC", code, "", "", r.Value, zeroTime)
		case code == "XMR":
			if perBTC, ok := r.Value.fixed(BTCScale); ok {
				sats, _ := mulDiv(pow10(BTCScale), pow10(BTCScale), perBTC)
				rates = addFixed(rates, c.Name(), "XMR", "BTC", sats, zeroTime)
			}
		}
	}
	return rates, nil
}
