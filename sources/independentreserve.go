package sources

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Independent Reserve: BTC/AUD, BTC/NZD and BTC/USD, one request per currency.
type IndependentReserve struct {
	BaseURL    string
	Client     HTTPClient
	Currencies []string
}

func (IndependentReserve) Name() string { return "independentreserve" }

func (r IndependentReserve) Fetch(ctx context.Context) (rates []Rate, err error) {
	currencies := r.Currencies
	if len(currencies) == 0 {
		currencies = []string{"AUD", "NZD", "USD"}
	}
	var errs []error
	for _, currency := range currencies {
		var t struct {
			Bid     num       `json:"CurrentHighestBidPrice"`
			Ask     num       `json:"CurrentLowestOfferPrice"`
			Last    num       `json:"LastPrice"`
			Created time.Time `json:"CreatedTimestampUtc"`
		}
		code := currency[:1] + strings.ToLower(currency[1:])
		url := urlOr(r.BaseURL, "https://api.independentreserve.com") +
			"/Public/GetMarketSummary?primaryCurrencyCode=Xbt&secondaryCurrencyCode=" + code
		if err := getJSON(ctx, r.Client, url, nil, &t); err != nil {
			errs = append(errs, err)
			continue
		}
		rates = add(rates, r.Name(), "BTC", currency, t.Bid, t.Ask, t.Last, t.Created)
	}
	if len(rates) == 0 {
		return nil, errors.Join(errs...)
	}
	return rates, nil
}
