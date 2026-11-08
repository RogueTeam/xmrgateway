// Package controller holds the gateway's state and rules. It knows nothing
// about HTTP: callers pass a context, a logger and plain values.
package controller

import (
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/RogueTeam/xmrgateway/jwt"
	"github.com/RogueTeam/xmrgateway/sources"
)

type Options struct {
	Secret            []byte
	TokenValidity     time.Duration
	ChallengeLifetime time.Duration
	ChallengeMinWait  time.Duration
	Difficulty        int
	MaxChallenges     int
	PriceRefresh      time.Duration
	PriceMaxAge       time.Duration
	FetchTimeout      time.Duration
	Aggregate         sources.Options
}

type Controller struct {
	opt     Options
	signer  *jwt.Signer[Access]
	sources []sources.Source

	mu         sync.Mutex
	challenges map[uuid.UUID]pending

	// prices is replaced as a whole by the price worker, so readers never lock.
	prices atomic.Pointer[snapshot]
}

func New(opt Options, srcs []sources.Source) *Controller {
	return &Controller{
		opt:        opt,
		signer:     jwt.New[Access](opt.Secret, opt.TokenValidity),
		sources:    srcs,
		challenges: map[uuid.UUID]pending{},
	}
}

func (c *Controller) Difficulty() int { return c.opt.Difficulty }
