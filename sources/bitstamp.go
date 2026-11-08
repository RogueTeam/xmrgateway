package sources

import (
	"context"
	"strconv"
	"strings"
)

// Bitstamp: BTC/USD, BTC/EUR and BTC/GBP.
type Bitstamp struct {
	BaseURL string
	Client  HTTPClient
}

func (Bitstamp) Name() string { return "bitstamp" }

func (b Bitstamp) Fetch(ctx context.Context) (rates []Rate, err error) {
	var resp []struct {
		Pair      string `json:"pair"`
		Bid       num    `json:"bid"`
		Ask       num    `json:"ask"`
		Last      num    `json:"last"`
		Timestamp string `json:"timestamp"`
	}
	url := urlOr(b.BaseURL, "https://www.bitstamp.net") + "/api/v2/ticker/"
	if err = getJSON(ctx, b.Client, url, nil, &resp); err != nil {
		return nil, err
	}
	for _, t := range resp {
		base, quote, ok := strings.Cut(t.Pair, "/")
		if !ok {
			continue
		}
		sec, _ := strconv.ParseInt(t.Timestamp, 10, 64)
		rates = add(rates, b.Name(), base, quote, t.Bid, t.Ask, t.Last, unixSec(sec))
	}
	return rates, nil
}
