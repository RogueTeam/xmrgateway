package sources

import (
	"context"
	"strings"
)

// Luno: BTC against ZAR, NGN, MYR, IDR and others in one call.
type Luno struct {
	BaseURL string
	Client  HTTPClient
}

func (Luno) Name() string { return "luno" }

func (l Luno) Fetch(ctx context.Context) (rates []Rate, err error) {
	var resp struct {
		Tickers []struct {
			Pair      string `json:"pair"`
			Timestamp int64  `json:"timestamp"`
			Bid       num    `json:"bid"`
			Ask       num    `json:"ask"`
			LastTrade num    `json:"last_trade"`
			Status    string `json:"status"`
		} `json:"tickers"`
	}
	url := urlOr(l.BaseURL, "https://api.luno.com") + "/api/1/tickers"
	if err = getJSON(ctx, l.Client, url, nil, &resp); err != nil {
		return nil, err
	}
	for _, t := range resp.Tickers {
		quote, ok := strings.CutPrefix(t.Pair, "XBT")
		if !ok || t.Status == "DISABLED" {
			continue
		}
		rates = add(rates, l.Name(), "BTC", quote, t.Bid, t.Ask, t.LastTrade, unixMilli(t.Timestamp))
	}
	return rates, nil
}
