package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/RogueTeam/xmrgateway/hashcash"
)

// Challenge is solved by a hashcash stamp whose payload is ID.
type Challenge struct {
	ID         uuid.UUID
	Difficulty int
	NotBefore  time.Time
	ExpiresAt  time.Time
}

type pending struct {
	notBefore time.Time
	expires   time.Time
}

func (c *Controller) challenge(id uuid.UUID, p pending) Challenge {
	return Challenge{ID: id, Difficulty: c.opt.Difficulty, NotBefore: p.notBefore, ExpiresAt: p.expires}
}

func (c *Controller) RegisterChallenge(ctx context.Context, logger *slog.Logger, now time.Time) (Challenge, error) {
	logger = logger.WithGroup("RegisterChallenge")
	if err := ctx.Err(); err != nil {
		return Challenge{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.challenges) >= c.opt.MaxChallenges {
		c.cleanLocked(now)
		if len(c.challenges) >= c.opt.MaxChallenges {
			logger.Warn("challenge limit reached", "pending", len(c.challenges))
			return Challenge{}, ErrChallengesFull
		}
	}
	id := uuid.NewV4()
	p := pending{notBefore: now.Add(c.opt.ChallengeMinWait), expires: now.Add(c.opt.ChallengeLifetime)}
	c.challenges[id] = p
	logger.Debug("registered", "id", id)
	return c.challenge(id, p), nil
}

func (c *Controller) Challenge(ctx context.Context, logger *slog.Logger, id uuid.UUID, now time.Time) (Challenge, bool) {
	logger = logger.WithGroup("Challenge")
	if ctx.Err() != nil {
		return Challenge{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.challenges[id]
	if !ok || !now.Before(p.expires) {
		logger.Debug("not found", "id", id)
		return Challenge{}, false
	}
	return c.challenge(id, p), true
}

func (c *Controller) CleanChallenges(ctx context.Context, logger *slog.Logger, now time.Time) int {
	logger = logger.WithGroup("CleanChallenges")
	if ctx.Err() != nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := c.cleanLocked(now)
	logger.Debug("expired challenges removed", "removed", removed, "pending", len(c.challenges))
	return removed
}

func (c *Controller) cleanLocked(now time.Time) (removed int) {
	for id, p := range c.challenges {
		if !now.Before(p.expires) {
			delete(c.challenges, id)
			removed++
		}
	}
	return removed
}

// DenyChallenge removes a challenge so it can no longer be redeemed.
func (c *Controller) DenyChallenge(ctx context.Context, logger *slog.Logger, id uuid.UUID) bool {
	logger = logger.WithGroup("DenyChallenge")
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.challenges[id]
	delete(c.challenges, id)
	logger.Debug("removed", "id", id, "existed", ok)
	return ok
}

func (c *Controller) PendingChallenges() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.challenges)
}

// ResolveChallenge redeems a stamp for a token. A stamp that fails the proof
// of work burns its challenge.
func (c *Controller) ResolveChallenge(ctx context.Context, logger *slog.Logger, stamp string, now time.Time) (Token, error) {
	logger = logger.WithGroup("ResolveChallenge")
	if err := ctx.Err(); err != nil {
		return Token{}, err
	}
	var d hashcash.Details
	if err := d.FromHashcash(stamp); err != nil {
		return Token{}, fmt.Errorf("%w: %v", ErrInvalidStamp, err)
	}
	id, err := uuid.Parse(d.Payload)
	if err != nil {
		return Token{}, fmt.Errorf("%w: payload is not a challenge id", ErrInvalidStamp)
	}
	logger = logger.With("id", id)

	c.mu.Lock()
	p, ok := c.challenges[id]
	expired := ok && !now.Before(p.expires)
	if expired {
		delete(c.challenges, id)
	}
	c.mu.Unlock()
	switch {
	case expired:
		return Token{}, ErrChallengeExpired
	case !ok:
		return Token{}, ErrUnknownChallenge
	case now.Before(p.notBefore):
		return Token{}, &TooEarlyError{RetryAt: p.notBefore}
	}

	if err := hashcash.Verify(sha256.Sum224, stamp, c.opt.Difficulty); err != nil {
		c.DenyChallenge(ctx, logger, id)
		logger.Info("proof of work rejected", "error", err)
		return Token{}, fmt.Errorf("%w: %v", ErrInvalidProof, err)
	}
	if !c.DenyChallenge(ctx, logger, id) {
		return Token{}, ErrUnknownChallenge
	}
	return c.issueToken(ctx, logger, now)
}
