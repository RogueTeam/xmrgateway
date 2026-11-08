package sources

import (
	"context"
	"fmt"
	"time"
)

// Yadio: street rates for currencies whose official rate is unrealistic.
// BTC/<cur> = USD[cur] * BTC, where USD[cur] is units per dollar.
type Yadio struct {
	BaseURL    string
	Client     HTTPClient
	Currencies []string
}

var yadioWhitelist = []string{"ARS", "BOB", "DOP", "EGP", "LBP", "PYG"}

const yadioRateScale = 8

func (Yadio) Name() string   { return "yadio" }
func (Yadio) BlueRate() bool { return true }

func (y Yadio) Fetch(ctx context.Context) (rates []Rate, err error) {
	var resp struct {
		BTC       num            `json:"BTC"`
		USD       map[string]num `json:"USD"`
		Base      string         `json:"base"`
		Timestamp int64          `json:"timestamp"`
	}
	url := urlOr(y.BaseURL, "https://api.yadio.io") + "/exrates"
	if err = getJSON(ctx, y.Client, url, nil, &resp); err != nil {
		return nil, err
	}
	ts := unixMilli(resp.Timestamp)
	btcUSD, ok := resp.BTC.fixed(FiatScale)
	if resp.Base != "USD" || !ok || !ts.After(time.Now().Add(-24*time.Hour)) {
		return nil, fmt.Errorf("yadio: unusable data (base %q, time %v)", resp.Base, ts)
	}
	currencies := y.Currencies
	if len(currencies) == 0 {
		currencies = yadioWhitelist
	}
	for _, currency := range currencies {
		perUSD, ok := resp.USD[currency].fixed(yadioRateScale)
		if !ok {
			continue
		}
		if price, ok := mulDiv(perUSD, btcUSD, pow10(yadioRateScale)); ok {
			rates = addFixed(rates, y.Name(), "BTC", currency, price, ts)
		}
	}
	return rates, nil
}
