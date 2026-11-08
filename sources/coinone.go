package sources

import (
	"context"
	"fmt"
)

// Coinone: BTC/KRW through public API v2 (Haveno still calls the legacy one).
type Coinone struct {
	BaseURL string
	Client  HTTPClient
}

func (Coinone) Name() string { return "coinone" }

func (c Coinone) Fetch(ctx context.Context) (rates []Rate, err error) {
	type level struct {
		Price num `json:"price"`
	}
	var resp struct {
		Result    string `json:"result"`
		ErrorCode string `json:"error_code"`
		Tickers   []struct {
			QuoteCurrency  string  `json:"quote_currency"`
			TargetCurrency string  `json:"target_currency"`
			Timestamp      int64   `json:"timestamp"`
			Last           num     `json:"last"`
			BestAsks       []level `json:"best_asks"`
			BestBids       []level `json:"best_bids"`
		} `json:"tickers"`
	}
	url := urlOr(c.BaseURL, "https://api.coinone.co.kr") + "/public/v2/ticker_new/KRW/BTC"
	if err = getJSON(ctx, c.Client, url, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Result != "success" {
		return nil, fmt.Errorf("coinone: result %q, error_code %s", resp.Result, resp.ErrorCode)
	}
	for _, t := range resp.Tickers {
		var bid, ask num
		if len(t.BestBids) > 0 && len(t.BestAsks) > 0 {
			bid, ask = t.BestBids[0].Price, t.BestAsks[0].Price
		}
		rates = add(rates, c.Name(), t.TargetCurrency, t.QuoteCurrency, bid, ask, t.Last, unixMilli(t.Timestamp))
	}
	return rates, nil
}
