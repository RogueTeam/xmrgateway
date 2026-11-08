package sources

import (
	"context"
	"strings"
)

// Mercado Bitcoin: BTC/BRL through API v4 (Haveno still calls the legacy one).
type MercadoBitcoin struct {
	BaseURL string
	Client  HTTPClient
}

func (MercadoBitcoin) Name() string { return "mercadobitcoin" }

func (m MercadoBitcoin) Fetch(ctx context.Context) (rates []Rate, err error) {
	var resp []struct {
		Pair string `json:"pair"`
		Buy  num    `json:"buy"`
		Sell num    `json:"sell"`
		Last num    `json:"last"`
		Date int64  `json:"date"`
	}
	url := urlOr(m.BaseURL, "https://api.mercadobitcoin.net") + "/api/v4/tickers?symbols=BTC-BRL"
	if err = getJSON(ctx, m.Client, url, nil, &resp); err != nil {
		return nil, err
	}
	for _, t := range resp {
		if base, quote, ok := strings.Cut(t.Pair, "-"); ok {
			rates = add(rates, m.Name(), base, quote, t.Buy, t.Sell, t.Last, unixSec(t.Date))
		}
	}
	return rates, nil
}
