package sources

import (
	"context"
	"net/url"
	"strings"
	"time"
)

// BTC Markets: BTC/AUD through the v3 API (Haveno still calls the legacy one).
type BTCMarkets struct {
	BaseURL string
	Client  HTTPClient
	Markets []string
}

func (BTCMarkets) Name() string { return "btcmarkets" }

func (b BTCMarkets) Fetch(ctx context.Context) (rates []Rate, err error) {
	markets := b.Markets
	if len(markets) == 0 {
		markets = []string{"BTC-AUD"}
	}
	var resp []struct {
		MarketID  string    `json:"marketId"`
		BestBid   num       `json:"bestBid"`
		BestAsk   num       `json:"bestAsk"`
		LastPrice num       `json:"lastPrice"`
		Timestamp time.Time `json:"timestamp"`
	}
	u := urlOr(b.BaseURL, "https://api.btcmarkets.net") + "/v3/markets/tickers?" + url.Values{"marketId": markets}.Encode()
	if err = getJSON(ctx, b.Client, u, nil, &resp); err != nil {
		return nil, err
	}
	for _, t := range resp {
		if base, quote, ok := strings.Cut(t.MarketID, "-"); ok {
			rates = add(rates, b.Name(), base, quote, t.BestBid, t.BestAsk, t.LastPrice, t.Timestamp)
		}
	}
	return rates, nil
}
