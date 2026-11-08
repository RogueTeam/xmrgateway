package sources

import (
	"context"
	"fmt"
	"strings"
)

// KuCoin: XMR/BTC and a few BTC/fiat pairs.
type KuCoin struct {
	BaseURL string
	Client  HTTPClient
}

func (KuCoin) Name() string { return "kucoin" }

func (k KuCoin) Fetch(ctx context.Context) (rates []Rate, err error) {
	var resp struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Time   int64 `json:"time"`
			Ticker []struct {
				Symbol string `json:"symbol"`
				Buy    num    `json:"buy"`
				Sell   num    `json:"sell"`
				Last   num    `json:"last"`
			} `json:"ticker"`
		} `json:"data"`
	}
	url := urlOr(k.BaseURL, "https://api.kucoin.com") + "/api/v1/market/allTickers"
	if err = getJSON(ctx, k.Client, url, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Code != "200000" {
		return nil, fmt.Errorf("kucoin: code %s: %s", resp.Code, resp.Msg)
	}
	ts := unixMilli(resp.Data.Time)
	for _, t := range resp.Data.Ticker {
		if base, quote, ok := strings.Cut(t.Symbol, "-"); ok {
			rates = add(rates, k.Name(), base, quote, t.Buy, t.Sell, t.Last, ts)
		}
	}
	return rates, nil
}
