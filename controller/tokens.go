package controller

import (
	"context"
	"log/slog"
	"time"
	"uuid"
)

// Access is the signed token payload.
type Access struct {
	ID uuid.UUID `json:"id"`
}

type Token struct {
	ID        uuid.UUID
	Value     string
	ExpiresAt time.Time
}

func (c *Controller) issueToken(ctx context.Context, logger *slog.Logger, now time.Time) (Token, error) {
	logger = logger.WithGroup("issueToken")
	if err := ctx.Err(); err != nil {
		return Token{}, err
	}
	id := uuid.NewV4()
	value, payload, err := c.signer.Sign(Access{ID: id}, now)
	if err != nil {
		logger.Error("signing failed", "error", err)
		return Token{}, err
	}
	logger.Debug("issued", "token", id, "expires", payload.Expires)
	return Token{ID: id, Value: value, ExpiresAt: payload.Expires}, nil
}

// VerifyToken returns the token's ID and expiry.
func (c *Controller) VerifyToken(ctx context.Context, logger *slog.Logger, value string, now time.Time) (uuid.UUID, time.Time, error) {
	logger = logger.WithGroup("VerifyToken")
	if err := ctx.Err(); err != nil {
		return uuid.Nil(), time.Time{}, err
	}
	payload, err := c.signer.Verify(value, now)
	if err != nil {
		logger.Debug("rejected", "error", err)
		return uuid.Nil(), time.Time{}, ErrInvalidToken
	}
	return payload.Data.ID, payload.Expires, nil
}
