package controller

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// PriceWorker refreshes the price cache now and then every PriceRefresh
// until ctx is done.
func (c *Controller) PriceWorker(ctx context.Context, logger *slog.Logger) {
	logger = logger.WithGroup("PriceWorker")
	logger.Info("started", "interval", c.opt.PriceRefresh)
	defer logger.Info("stopped")

	ticker := time.NewTicker(c.opt.PriceRefresh)
	defer ticker.Stop()
	for {
		report, err := c.RefreshPrices(ctx, logger, time.Now())
		for _, r := range report.Results {
			if r.Err != nil {
				logger.Warn("source failed", "source", r.Source, "error", strings.ReplaceAll(r.Err.Error(), "\n", "; "))
			}
		}
		if err != nil && ctx.Err() == nil {
			logger.Error("refresh failed, keeping the previous prices", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ChallengeJanitor drops expired challenges every ChallengeLifetime until
// ctx is done.
func (c *Controller) ChallengeJanitor(ctx context.Context, logger *slog.Logger) {
	logger = logger.WithGroup("ChallengeJanitor")
	ticker := time.NewTicker(c.opt.ChallengeLifetime)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			c.CleanChallenges(ctx, logger, now)
		}
	}
}
