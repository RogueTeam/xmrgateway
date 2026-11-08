package sources

import (
	"context"
	"time"
)

// CriptoYa: BTC/ARS at the blue rate, one quote per whitelisted exchange,
// using the ask price like Haveno does.
type CriptoYa struct {
	BaseURL   string
	Client    HTTPClient
	Exchanges []string
}

var criptoYaWhitelist = []string{
	"argenbtc", "buenbit", "ripio", "ripioexchange", "satoshitango", "cryptomkt",
	"decrypto", "latamex", "bitso", "letsbit", "fiwind", "lemoncash", "bitmonedero",
	"belo", "tiendacrypto", "saldo", "kriptonmarket", "calypso", "bybit",
}

func (CriptoYa) Name() string   { return "criptoya" }
func (CriptoYa) BlueRate() bool { return true }

func (c CriptoYa) Fetch(ctx context.Context) (rates []Rate, err error) {
	var resp map[string]struct {
		Ask  num   `json:"ask"`
		Time int64 `json:"time"`
	}
	url := urlOr(c.BaseURL, "https://criptoya.com") + "/api/btc/ars/0.1"
	if err = getJSON(ctx, c.Client, url, nil, &resp); err != nil {
		return nil, err
	}
	exchanges := c.Exchanges
	if len(exchanges) == 0 {
		exchanges = criptoYaWhitelist
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, exchange := range exchanges {
		t, ok := resp[exchange]
		if !ok || !unixSec(t.Time).After(cutoff) {
			continue
		}
		rates = add(rates, c.Name()+":"+exchange, "BTC", "ARS", "", "", t.Ask, unixSec(t.Time))
	}
	return rates, nil
}
