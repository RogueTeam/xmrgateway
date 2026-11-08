package controller

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/RogueTeam/xmrgateway/sources"
)

type Quote struct {
	Currency  string
	Price     string
	UpdatedAt time.Time
}

type Report struct {
	Results []sources.Result
	Prices  map[string]sources.Price
}

type snapshot struct {
	prices map[string]sources.Price
	at     time.Time
}

// RefreshPrices polls every source, builds the consensus and swaps it in.
// When nothing usable comes back the previous prices stay.
func (c *Controller) RefreshPrices(ctx context.Context, logger *slog.Logger, now time.Time) (Report, error) {
	logger = logger.WithGroup("RefreshPrices")
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	results := sources.FetchAll(ctx, c.opt.FetchTimeout, c.sources...)
	opt := c.opt.Aggregate
	opt.Now = func() time.Time { return now }
	report := Report{Results: results, Prices: sources.Aggregate(sources.AllRates(results), opt)}
	for quote, p := range report.Prices {
		if len(p.Rejected) > 0 {
			logger.Warn("quotes rejected as too far from consensus", "currency", quote, "sources", p.Rejected)
		}
	}
	if len(report.Prices) == 0 {
		return report, ErrNoQuotes
	}
	c.prices.Store(&snapshot{prices: report.Prices, at: now})
	logger.Info("prices updated", "currencies", len(report.Prices))
	return report, nil
}

func (c *Controller) current(now time.Time) *snapshot {
	s := c.prices.Load()
	if s == nil || now.Sub(s.at) > c.opt.PriceMaxAge {
		return nil
	}
	return s
}

func (c *Controller) Price(ctx context.Context, logger *slog.Logger, currency string, now time.Time) (Quote, error) {
	logger = logger.WithGroup("Price")
	if err := ctx.Err(); err != nil {
		return Quote{}, err
	}
	s := c.current(now)
	if s == nil {
		logger.Warn("no recent prices in cache")
		return Quote{}, ErrNoPrice
	}
	currency = strings.ToUpper(currency)
	p, ok := s.prices[currency]
	if !ok {
		return Quote{}, ErrUnknownCurrency
	}
	return Quote{Currency: currency, Price: p.String(), UpdatedAt: s.at}, nil
}

func (c *Controller) Currencies(ctx context.Context, logger *slog.Logger, now time.Time) []string {
	logger = logger.WithGroup("Currencies")
	if ctx.Err() != nil {
		return nil
	}
	s := c.current(now)
	if s == nil {
		logger.Debug("no recent prices in cache")
		return nil
	}
	return slices.Sorted(maps.Keys(s.prices))
}
