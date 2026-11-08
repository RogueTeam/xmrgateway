package httpmux

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/RogueTeam/xmrgateway/api"
	"github.com/RogueTeam/xmrgateway/hashcash"
	"github.com/RogueTeam/xmrgateway/internal/config"
)

// examples are the sample bodies on the pages, built from the config so every
// interval and time shown matches what the server does.
type examples struct {
	Price      string
	TooMany    string
	RetryAfter int
	// fields overrides the OpenAPI examples: model -> field -> value.
	fields map[string]map[string]string
}

var exampleStart = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func newExamples(cfg config.Config) examples {
	issued := exampleStart
	redeemed := issued.Add(cfg.ChallengeMinWait.Duration)
	updated := redeemed.Add(-cfg.PriceRefresh.Duration / 2).Truncate(time.Second)
	next := redeemed.Add(cfg.RateLimit.Duration)
	tooEarly := redeemed.Add(cfg.RateLimit.Duration / 3).Truncate(time.Second)
	retryAfter := int((next.Sub(tooEarly) + time.Second - 1) / time.Second)

	price, _ := json.MarshalIndent(api.Price{Currency: "USD", Price: "312.45", UpdatedAt: updated, NextRequestAt: next}, "", "  ")
	tooMany, _ := json.MarshalIndent(api.Error{Message: "rate limited", RetryAt: &next}, "", "  ")

	stamp := fmt.Sprintf("1:%d:%s:0d5e4bd2-2c32-4f3e-9a7a-2f6a0e1c9b11::0:AAAAAAAB4zA=",
		cfg.PowDifficulty, issued.Add(time.Second).Format(hashcash.TimeLayout))
	rfc := func(t time.Time) string { return t.Format(time.RFC3339) }

	return examples{
		Price:      string(price),
		TooMany:    string(tooMany),
		RetryAfter: retryAfter,
		fields: map[string]map[string]string{
			"Challenge": {
				"difficulty": strconv.Itoa(cfg.PowDifficulty),
				"not_before": rfc(redeemed),
				"expires_at": rfc(issued.Add(cfg.ChallengeLifetime.Duration)),
			},
			"RedeemRequest": {"stamp": stamp},
			"Token":         {"expires_at": rfc(redeemed.Add(cfg.TokenValidity.Duration))},
			"Price":         {"updated_at": rfc(updated), "next_request_at": rfc(next)},
			"Error":         {"retry_at": rfc(next)},
		},
	}
}
