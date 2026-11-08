package sources

import (
	"context"
	"strings"
)

// Paribu: BTC/TRY. Undocumented endpoint, the same one Haveno uses.
type Paribu struct {
	BaseURL string
	Client  HTTPClient
}

func (Paribu) Name() string { return "paribu" }

func (p Paribu) Fetch(ctx context.Context) (rates []Rate, err error) {
	var resp map[string]struct {
		Last       num `json:"last"`
		LowestAsk  num `json:"lowestAsk"`
		HighestBid num `json:"highestBid"`
	}
	url := urlOr(p.BaseURL, "https://www.paribu.com") + "/ticker"
	if err = getJSON(ctx, p.Client, url, nil, &resp); err != nil {
		return nil, err
	}
	for symbol, t := range resp {
		base, quote, ok := strings.Cut(symbol, "_")
		if !ok {
			continue
		}
		if quote == "TL" {
			quote = "TRY"
		}
		rates = add(rates, p.Name(), base, quote, t.HighestBid, t.LowestAsk, t.Last, zeroTime)
	}
	return rates, nil
}
