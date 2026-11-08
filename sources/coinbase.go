package sources

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Coinbase Exchange: BTC/USD, BTC/EUR and BTC/GBP, one request per product.
type Coinbase struct {
	BaseURL  string
	Client   HTTPClient
	Products []string
}

func (Coinbase) Name() string { return "coinbase" }

func (c Coinbase) Fetch(ctx context.Context) (rates []Rate, err error) {
	products := c.Products
	if len(products) == 0 {
		products = []string{"BTC-USD", "BTC-EUR", "BTC-GBP"}
	}
	var errs []error
	for _, product := range products {
		var t struct {
			Bid   num       `json:"bid"`
			Ask   num       `json:"ask"`
			Price num       `json:"price"`
			Time  time.Time `json:"time"`
		}
		url := urlOr(c.BaseURL, "https://api.exchange.coinbase.com") + "/products/" + product + "/ticker"
		if err := getJSON(ctx, c.Client, url, nil, &t); err != nil {
			errs = append(errs, err)
			continue
		}
		base, quote, _ := strings.Cut(product, "-")
		rates = add(rates, c.Name(), base, quote, t.Bid, t.Ask, t.Price, t.Time)
	}
	if len(rates) == 0 {
		return nil, errors.Join(errs...)
	}
	return rates, nil
}
