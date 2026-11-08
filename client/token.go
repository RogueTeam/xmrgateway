package client

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/RogueTeam/xmrgateway/api"
	"github.com/RogueTeam/xmrgateway/hashcash"
)

// Solve finds a stamp for the challenge on all CPUs. Each worker searches its
// own space by using its index as the stamp's random field.
func Solve(ctx context.Context, challengeID string, difficulty int) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		stamp string
		err   error
	}
	workers := runtime.NumCPU()
	results := make(chan result, workers)
	now := time.Now()
	for w := range workers {
		go func() {
			d := hashcash.Details{LeadingZeroes: difficulty, Payload: challengeID, Random: strconv.Itoa(w), Time: now}
			stamp, err := d.Hashcash(ctx, sha256.Sum224)
			results <- result{stamp, err}
		}()
	}
	var err error
	for range workers {
		r := <-results
		if r.err == nil {
			return r.stamp, nil
		}
		err = r.err
	}
	return "", err
}

// Token solves a challenge and redeems it. An empty challengeID requests a
// new challenge.
func (c *Client) Token(ctx context.Context, challengeID string) (api.Token, error) {
	var ch api.Challenge
	var err error
	if challengeID == "" {
		ch, err = c.NewChallenge(ctx)
	} else {
		ch, err = c.Challenge(ctx, challengeID)
	}
	if err != nil {
		return api.Token{}, err
	}
	stamp, err := Solve(ctx, ch.ID, ch.Difficulty)
	if err != nil {
		return api.Token{}, err
	}
	if err := sleepUntil(ctx, ch.NotBefore); err != nil {
		return api.Token{}, err
	}
	token, err := c.Redeem(ctx, stamp)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusTooEarly && apiErr.Body.RetryAt != nil {
		if err := sleepUntil(ctx, apiErr.Body.RetryAt.Add(time.Second)); err != nil {
			return api.Token{}, err
		}
		return c.Redeem(ctx, stamp)
	}
	return token, err
}

func sleepUntil(ctx context.Context, t time.Time) error {
	wait := time.Until(t)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
