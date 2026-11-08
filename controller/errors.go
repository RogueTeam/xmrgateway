package controller

import (
	"errors"
	"time"
)

var (
	ErrChallengesFull   = errors.New("too many pending challenges")
	ErrUnknownChallenge = errors.New("unknown or already used challenge")
	ErrChallengeExpired = errors.New("challenge expired")
	ErrInvalidStamp     = errors.New("malformed stamp")
	ErrInvalidProof     = errors.New("stamp does not meet the difficulty")
	ErrInvalidToken     = errors.New("invalid or expired token")
	ErrUnknownCurrency  = errors.New("unknown currency")
	ErrNoPrice          = errors.New("no recent price available")
	ErrNoQuotes         = errors.New("no source returned a usable price")
)

// TooEarlyError is returned when a stamp arrives before the minimum wait.
type TooEarlyError struct {
	RetryAt time.Time
}

func (e *TooEarlyError) Error() string {
	return "challenge cannot be redeemed before " + e.RetryAt.UTC().Format(time.RFC3339)
}
