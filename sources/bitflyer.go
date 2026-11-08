package sources

import (
	"context"
	"errors"
	"strings"
	"time"
)

// bitFlyer: BTC/JPY.
type Bitflyer struct {
	BaseURL  string
	Client   HTTPClient
	Products []string
}

func (Bitflyer) Name() string { return "bitflyer" }

func (b Bitflyer) Fetch(ctx context.Context) (rates []Rate, err error) {
	products := b.Products
	if len(products) == 0 {
		products = []string{"BTC_JPY"}
	}
	var errs []error
	for _, product := range products {
		var t struct {
			Timestamp string `json:"timestamp"`
			BestBid   num    `json:"best_bid"`
			BestAsk   num    `json:"best_ask"`
			LTP       num    `json:"ltp"`
		}
		url := urlOr(b.BaseURL, "https://api.bitflyer.com") + "/v1/getticker?product_code=" + product
		if err := getJSON(ctx, b.Client, url, nil, &t); err != nil {
			errs = append(errs, err)
			continue
		}
		ts, _ := time.ParseInLocation("2006-01-02T15:04:05.999999999", t.Timestamp, time.UTC)
		base, quote, _ := strings.Cut(product, "_")
		rates = add(rates, b.Name(), base, quote, t.BestBid, t.BestAsk, t.LTP, ts)
	}
	if len(rates) == 0 {
		return nil, errors.Join(errs...)
	}
	return rates, nil
}
