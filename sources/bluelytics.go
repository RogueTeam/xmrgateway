package sources

import (
	"context"
	"errors"
)

// BlueLytics publishes Argentina's official and blue USD rates. Their ratio
// converts ARS quotes from official-rate sources (see Options.ARSGap).
type BlueLytics struct {
	BaseURL string
	Client  HTTPClient
}

type Gap struct {
	Blue     uint64
	Official uint64
}

func (b BlueLytics) Gap(ctx context.Context) (Gap, error) {
	type rate struct {
		ValueSell num `json:"value_sell"`
	}
	var resp struct {
		Oficial rate `json:"oficial"`
		Blue    rate `json:"blue"`
	}
	url := urlOr(b.BaseURL, "https://api.bluelytics.com.ar") + "/v2/latest"
	if err := getJSON(ctx, b.Client, url, nil, &resp); err != nil {
		return Gap{}, err
	}
	blue, okBlue := resp.Blue.ValueSell.fixed(FiatScale)
	official, okOfficial := resp.Oficial.ValueSell.fixed(FiatScale)
	if !okBlue || !okOfficial {
		return Gap{}, errors.New("bluelytics: missing sell values")
	}
	return Gap{Blue: blue, Official: official}, nil
}
