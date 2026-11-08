package sources

import (
	"context"
	"strings"
)

// Binance: BTC/fiat only, since XMR was delisted. Blocks US addresses.
type Binance struct {
	BaseURL string
	Client  HTTPClient
}

func (Binance) Name() string   { return "binance" }
func (Binance) BlueRate() bool { return true }

func (b Binance) Fetch(ctx context.Context) (rates []Rate, err error) {
	var resp []struct {
		Symbol   string `json:"symbol"`
		BidPrice num    `json:"bidPrice"`
		AskPrice num    `json:"askPrice"`
	}
	url := urlOr(b.BaseURL, "https://api.binance.com") + "/api/v3/ticker/bookTicker"
	if err = getJSON(ctx, b.Client, url, nil, &resp); err != nil {
		return nil, err
	}
	for _, t := range resp {
		if quote, ok := strings.CutPrefix(t.Symbol, "BTC"); ok {
			rates = add(rates, b.Name(), "BTC", quote, t.BidPrice, t.AskPrice, "", zeroTime)
		}
	}
	return rates, nil
}
