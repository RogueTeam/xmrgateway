package controller

import (
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/RogueTeam/xmrgateway/hashcash"
	"github.com/RogueTeam/xmrgateway/sources"
)

var (
	t0     = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	logger = slog.New(slog.DiscardHandler)
	secret = []byte("0123456789abcdef0123456789abcdef")
)

type stub struct {
	name  string
	rates []sources.Rate
	err   error
}

func (s stub) Name() string                                  { return s.name }
func (s stub) Fetch(context.Context) ([]sources.Rate, error) { return slices.Clone(s.rates), s.err }

func xmr(source, quote string, price uint64, at time.Time) sources.Rate {
	return sources.Rate{Base: "XMR", Quote: quote, Price: price, Time: at, Source: source}
}

func options(max int) Options {
	return Options{
		Secret:            secret,
		TokenValidity:     24 * time.Hour,
		ChallengeLifetime: 5 * time.Minute,
		ChallengeMinWait:  30 * time.Second,
		Difficulty:        8,
		MaxChallenges:     max,
		PriceRefresh:      time.Minute,
		PriceMaxAge:       10 * time.Minute,
		FetchTimeout:      time.Second,
	}
}

func newController(max int, srcs ...sources.Source) *Controller { return New(options(max), srcs) }

func stamp(t *testing.T, payload string, bits int) string {
	t.Helper()
	d := hashcash.Details{LeadingZeroes: bits, Payload: payload, Time: t0}
	hc, err := d.Hashcash(t.Context(), sha256.Sum224)
	if err != nil {
		t.Fatal(err)
	}
	return hc
}

func ctxFor(t *testing.T, cancel bool) context.Context {
	if !cancel {
		return t.Context()
	}
	ctx, stop := context.WithCancel(t.Context())
	stop()
	return ctx
}

func register(t *testing.T, c *Controller, at ...time.Duration) []Challenge {
	t.Helper()
	var out []Challenge
	for _, a := range at {
		ch, err := c.RegisterChallenge(t.Context(), logger, t0.Add(a))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, ch)
	}
	return out
}

func TestRegisterChallenge(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Max      int
				Existing []time.Duration
				At       time.Duration
			}
			Expect struct{ Pending int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "first challenge", Input: Input{Max: 10}, Expect: Expect{1}},
			{Name: "under the limit nothing is cleaned", Input: Input{10, []time.Duration{0}, time.Hour}, Expect: Expect{2}},
			{Name: "full but everything expired", Input: Input{2, []time.Duration{0, 0}, 5 * time.Minute}, Expect: Expect{1}},
			{Name: "full and one expired", Input: Input{2, []time.Duration{0, 4 * time.Minute}, 5 * time.Minute}, Expect: Expect{2}},
			{Name: "limit of one", Input: Input{1, []time.Duration{0}, 6 * time.Minute}, Expect: Expect{1}},
			{Name: "a thousand distinct ids", Input: Input{1000, slices.Repeat([]time.Duration{0}, 999), 0}, Expect: Expect{1000}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := newController(test.Input.Max)
				seen := map[uuid.UUID]bool{}
				for _, ch := range register(t, c, test.Input.Existing...) {
					seen[ch.ID] = true
				}
				now := t0.Add(test.Input.At)
				ch, err := c.RegisterChallenge(t.Context(), logger, now)
				if err != nil {
					t.Fatal(err)
				}
				if seen[ch.ID] || ch.ID == uuid.Nil() {
					t.Fatalf("id %s reused", ch.ID)
				}
				want := Challenge{ID: ch.ID, Difficulty: 8, NotBefore: now.Add(30 * time.Second), ExpiresAt: now.Add(5 * time.Minute)}
				if ch != want {
					t.Fatalf("got  %+v\nwant %+v", ch, want)
				}
				if got, ok := c.Challenge(t.Context(), logger, ch.ID, now); !ok || got != ch {
					t.Fatalf("lookup %+v %v", got, ok)
				}
				if n := c.PendingChallenges(); n != test.Expect.Pending {
					t.Fatalf("pending %d, want %d", n, test.Expect.Pending)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Max      int
				Existing []time.Duration
				At       time.Duration
				Canceled bool
			}
			Expect struct {
				Err     error
				Pending int
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "full", Input: Input{2, []time.Duration{0, 0}, time.Minute, false}, Expect: Expect{ErrChallengesFull, 2}},
			{Name: "full a nanosecond before expiry", Input: Input{2, []time.Duration{0, 0}, 5*time.Minute - 1, false}, Expect: Expect{ErrChallengesFull, 2}},
			{Name: "canceled context", Input: Input{10, nil, 0, true}, Expect: Expect{context.Canceled, 0}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := newController(test.Input.Max)
				register(t, c, test.Input.Existing...)
				ch, err := c.RegisterChallenge(ctxFor(t, test.Input.Canceled), logger, t0.Add(test.Input.At))
				if !errors.Is(err, test.Expect.Err) || ch != (Challenge{}) {
					t.Fatalf("got %+v, %v", ch, err)
				}
				if n := c.PendingChallenges(); n != test.Expect.Pending {
					t.Fatalf("pending %d, want %d", n, test.Expect.Pending)
				}
			})
		}
	})
}

func TestChallenge(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ At time.Duration }
			Expect struct{}
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "right away", Input: Input{0}},
			{Name: "before the minimum wait", Input: Input{10 * time.Second}},
			{Name: "last nanosecond", Input: Input{5*time.Minute - 1}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := newController(10)
				want := register(t, c, 0)[0]
				if got, ok := c.Challenge(t.Context(), logger, want.ID, t0.Add(test.Input.At)); !ok || got != want {
					t.Fatalf("got %+v %v", got, ok)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Unknown, Denied, Canceled bool
				At                        time.Duration
			}
			Expect struct{ Found bool }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "unknown id", Input: Input{Unknown: true}, Expect: Expect{false}},
			{Name: "expired", Input: Input{At: 5 * time.Minute}, Expect: Expect{false}},
			{Name: "denied", Input: Input{Denied: true}, Expect: Expect{false}},
			{Name: "canceled context", Input: Input{Canceled: true}, Expect: Expect{false}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := newController(10)
				id := register(t, c, 0)[0].ID
				if test.Input.Unknown {
					id = uuid.NewV4()
				}
				if test.Input.Denied {
					c.DenyChallenge(t.Context(), logger, id)
				}
				got, ok := c.Challenge(ctxFor(t, test.Input.Canceled), logger, id, t0.Add(test.Input.At))
				if ok != test.Expect.Found || got != (Challenge{}) {
					t.Fatalf("got %+v %v", got, ok)
				}
			})
		}
	})
}

func TestResolveChallenge(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Bits int
				At   time.Duration
			}
			Expect struct{ Expires time.Duration }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "exactly at the minimum wait", Input: Input{8, 30 * time.Second}, Expect: Expect{30*time.Second + 24*time.Hour}},
			{Name: "stronger stamp than asked", Input: Input{12, time.Minute}, Expect: Expect{time.Minute + 24*time.Hour}},
			{Name: "last second", Input: Input{8, 5*time.Minute - time.Second}, Expect: Expect{5*time.Minute - time.Second + 24*time.Hour}},
			{Name: "expiry drops sub-seconds", Input: Input{8, 45*time.Second + 500*time.Millisecond}, Expect: Expect{45*time.Second + 24*time.Hour}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := newController(10)
				ch := register(t, c, 0)[0]
				hc := stamp(t, ch.ID.String(), test.Input.Bits)
				now := t0.Add(test.Input.At)

				tok, err := c.ResolveChallenge(t.Context(), logger, hc, now)
				if err != nil {
					t.Fatal(err)
				}
				if !tok.ExpiresAt.Equal(t0.Add(test.Expect.Expires)) || tok.ID == ch.ID || tok.Value == "" {
					t.Fatalf("token %+v", tok)
				}
				id, exp, err := c.VerifyToken(t.Context(), logger, tok.Value, now)
				if err != nil || id != tok.ID || !exp.Equal(tok.ExpiresAt) {
					t.Fatalf("verify %s %v %v", id, exp, err)
				}
				if n := c.PendingChallenges(); n != 0 {
					t.Fatalf("challenge not consumed, %d pending", n)
				}
			})
		}
	})

	t.Run("Succeed under contention", func(t *testing.T) {
		type (
			Input  struct{ Goroutines int }
			Expect struct{ Tokens int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "one stamp, many redeemers", Input: Input{64}, Expect: Expect{1}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := newController(10)
				hc := stamp(t, register(t, c, 0)[0].ID.String(), 8)
				var mu sync.Mutex
				var tokens int
				var wg sync.WaitGroup
				for range test.Input.Goroutines {
					wg.Go(func() {
						_, err := c.ResolveChallenge(t.Context(), logger, hc, t0.Add(time.Minute))
						if err != nil && !errors.Is(err, ErrUnknownChallenge) {
							t.Error(err)
						}
						if err == nil {
							mu.Lock()
							tokens++
							mu.Unlock()
						}
					})
				}
				wg.Wait()
				if tokens != test.Expect.Tokens {
					t.Fatalf("%d tokens issued for one stamp", tokens)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Stamp    func(t *testing.T, id uuid.UUID) string
				At       time.Duration
				Canceled bool
				Twice    bool
			}
			Expect struct {
				Err     error
				RetryAt time.Duration
				Pending int
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		good := func(t *testing.T, id uuid.UUID) string { return stamp(t, id.String(), 8) }
		tests := []Test{
			{
				Name:   "garbage",
				Input:  Input{Stamp: func(*testing.T, uuid.UUID) string { return "garbage" }, At: time.Minute},
				Expect: Expect{Err: ErrInvalidStamp, Pending: 1},
			},
			{
				Name:   "payload is not a challenge id",
				Input:  Input{Stamp: func(t *testing.T, _ uuid.UUID) string { return stamp(t, "hello", 8) }, At: time.Minute},
				Expect: Expect{Err: ErrInvalidStamp, Pending: 1},
			},
			{
				Name:   "unknown challenge",
				Input:  Input{Stamp: func(t *testing.T, _ uuid.UUID) string { return good(t, uuid.NewV4()) }, At: time.Minute},
				Expect: Expect{Err: ErrUnknownChallenge, Pending: 1},
			},
			{
				Name:   "too early keeps the challenge",
				Input:  Input{Stamp: good, At: 10 * time.Second},
				Expect: Expect{RetryAt: 30 * time.Second, Pending: 1},
			},
			{
				Name:   "a nanosecond too early",
				Input:  Input{Stamp: good, At: 30*time.Second - 1},
				Expect: Expect{RetryAt: 30 * time.Second, Pending: 1},
			},
			{
				Name:   "expired",
				Input:  Input{Stamp: good, At: 5 * time.Minute},
				Expect: Expect{Err: ErrChallengeExpired, Pending: 0},
			},
			{
				Name: "claims more work than done",
				Input: Input{Stamp: func(t *testing.T, id uuid.UUID) string {
					return strings.Replace(good(t, id), "1:8:", "1:30:", 1)
				}, At: time.Minute},
				Expect: Expect{Err: ErrInvalidProof, Pending: 0},
			},
			{
				Name:   "below the difficulty",
				Input:  Input{Stamp: func(t *testing.T, id uuid.UUID) string { return stamp(t, id.String(), 4) }, At: time.Minute},
				Expect: Expect{Err: ErrInvalidProof, Pending: 0},
			},
			{
				Name:   "redeemed twice",
				Input:  Input{Stamp: good, At: time.Minute, Twice: true},
				Expect: Expect{Err: ErrUnknownChallenge, Pending: 0},
			},
			{
				Name:   "canceled context",
				Input:  Input{Stamp: good, At: time.Minute, Canceled: true},
				Expect: Expect{Err: context.Canceled, Pending: 1},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := newController(10)
				hc := test.Input.Stamp(t, register(t, c, 0)[0].ID)
				now := t0.Add(test.Input.At)
				if test.Input.Twice {
					if _, err := c.ResolveChallenge(t.Context(), logger, hc, now); err != nil {
						t.Fatal(err)
					}
				}
				tok, err := c.ResolveChallenge(ctxFor(t, test.Input.Canceled), logger, hc, now)
				if tok != (Token{}) {
					t.Fatalf("token issued: %+v", tok)
				}
				if test.Expect.RetryAt != 0 {
					var early *TooEarlyError
					if !errors.As(err, &early) || !early.RetryAt.Equal(t0.Add(test.Expect.RetryAt)) {
						t.Fatalf("got %v, want retry at %v", err, test.Expect.RetryAt)
					}
				} else if !errors.Is(err, test.Expect.Err) {
					t.Fatalf("got %v, want %v", err, test.Expect.Err)
				}
				if n := c.PendingChallenges(); n != test.Expect.Pending {
					t.Fatalf("pending %d, want %d", n, test.Expect.Pending)
				}
			})
		}
	})
}

func TestVerifyToken(t *testing.T) {
	issuer := newController(1)
	tok, err := issuer.issueToken(t.Context(), logger, t0)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Other bool
				At    time.Duration
			}
			Expect struct{ Expires time.Time }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "right after issuing", Input: Input{false, 0}, Expect: Expect{t0.Add(24 * time.Hour)}},
			{Name: "last nanosecond", Input: Input{false, 24*time.Hour - 1}, Expect: Expect{t0.Add(24 * time.Hour)}},
			{Name: "another instance with the same secret", Input: Input{true, time.Hour}, Expect: Expect{t0.Add(24 * time.Hour)}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := issuer
				if test.Input.Other {
					c = newController(1)
				}
				id, exp, err := c.VerifyToken(t.Context(), logger, tok.Value, t0.Add(test.Input.At))
				if err != nil || id != tok.ID || !exp.Equal(test.Expect.Expires) {
					t.Fatalf("got %s %v %v", id, exp, err)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Token    string
				Secret   []byte
				At       time.Duration
				Canceled bool
			}
			Expect struct{ Err error }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "expired", Input: Input{Token: tok.Value, At: 24 * time.Hour}, Expect: Expect{ErrInvalidToken}},
			{Name: "tampered", Input: Input{Token: tok.Value + "x"}, Expect: Expect{ErrInvalidToken}},
			{Name: "truncated", Input: Input{Token: tok.Value[:len(tok.Value)/2]}, Expect: Expect{ErrInvalidToken}},
			{Name: "empty", Input: Input{Token: ""}, Expect: Expect{ErrInvalidToken}},
			{Name: "challenge id instead of a token", Input: Input{Token: uuid.NewV4().String()}, Expect: Expect{ErrInvalidToken}},
			{Name: "rotated secret", Input: Input{Token: tok.Value, Secret: []byte("fedcba9876543210fedcba9876543210")}, Expect: Expect{ErrInvalidToken}},
			{Name: "canceled context", Input: Input{Token: tok.Value, Canceled: true}, Expect: Expect{context.Canceled}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				opt := options(1)
				if test.Input.Secret != nil {
					opt.Secret = test.Input.Secret
				}
				id, exp, err := New(opt, nil).VerifyToken(ctxFor(t, test.Input.Canceled), logger, test.Input.Token, t0.Add(test.Input.At))
				if !errors.Is(err, test.Expect.Err) || id != uuid.Nil() || !exp.IsZero() {
					t.Fatalf("got %s %v %v", id, exp, err)
				}
			})
		}
	})
}

func TestDenyChallenge(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{}
			Expect struct{ Pending int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "known challenge", Expect: Expect{Pending: 1}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := newController(10)
				ch := register(t, c, 0, 0)[0]
				if !c.DenyChallenge(t.Context(), logger, ch.ID) {
					t.Fatal("not removed")
				}
				if n := c.PendingChallenges(); n != test.Expect.Pending {
					t.Fatalf("pending %d", n)
				}
				if _, err := c.ResolveChallenge(t.Context(), logger, stamp(t, ch.ID.String(), 8), t0.Add(time.Minute)); !errors.Is(err, ErrUnknownChallenge) {
					t.Fatalf("denied challenge redeemed: %v", err)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Unknown bool
				Times   int
			}
			Expect struct {
				Removed []bool
				Pending int
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "unknown challenge", Input: Input{Unknown: true, Times: 1}, Expect: Expect{[]bool{false}, 2}},
			{Name: "denied twice", Input: Input{Times: 2}, Expect: Expect{[]bool{true, false}, 1}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := newController(10)
				id := register(t, c, 0, 0)[0].ID
				if test.Input.Unknown {
					id = uuid.NewV4()
				}
				var got []bool
				for range test.Input.Times {
					got = append(got, c.DenyChallenge(t.Context(), logger, id))
				}
				if !slices.Equal(got, test.Expect.Removed) || c.PendingChallenges() != test.Expect.Pending {
					t.Fatalf("removed %v, pending %d", got, c.PendingChallenges())
				}
			})
		}
	})
}

func TestCleanChallenges(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Registered []time.Duration
				At         time.Duration
			}
			Expect struct{ Removed, Pending int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "nothing registered", Input: Input{At: time.Hour}, Expect: Expect{0, 0}},
			{Name: "all expired", Input: Input{Registered: []time.Duration{0, time.Minute}, At: 6 * time.Minute}, Expect: Expect{2, 0}},
			{Name: "only expired ones", Input: Input{Registered: []time.Duration{0, 3 * time.Minute}, At: 5 * time.Minute}, Expect: Expect{1, 1}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := newController(10)
				register(t, c, test.Input.Registered...)
				removed := c.CleanChallenges(t.Context(), logger, t0.Add(test.Input.At))
				if removed != test.Expect.Removed || c.PendingChallenges() != test.Expect.Pending {
					t.Fatalf("removed %d, pending %d", removed, c.PendingChallenges())
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Registered []time.Duration
				At         time.Duration
				Canceled   bool
			}
			Expect struct{ Pending int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "nothing expired yet", Input: Input{Registered: []time.Duration{0}, At: 5*time.Minute - 1}, Expect: Expect{1}},
			{Name: "canceled context", Input: Input{Registered: []time.Duration{0}, At: time.Hour, Canceled: true}, Expect: Expect{1}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := newController(10)
				register(t, c, test.Input.Registered...)
				if removed := c.CleanChallenges(ctxFor(t, test.Input.Canceled), logger, t0.Add(test.Input.At)); removed != 0 {
					t.Fatalf("removed %d", removed)
				}
				if n := c.PendingChallenges(); n != test.Expect.Pending {
					t.Fatalf("pending %d", n)
				}
			})
		}
	})
}

func priced() []sources.Source {
	return []sources.Source{stub{name: "a", rates: []sources.Rate{
		xmr("a", "USD", 31250, t0),
		xmr("a", "EUR", 28000, t0),
		xmr("a", "BTC", 305000, t0),
	}}}
}

func TestRefreshPrices(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Sources []sources.Source }
			Expect struct {
				Prices   map[string]string
				Failed   []string
				Rejected []string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name: "two sources are averaged",
				Input: Input{[]sources.Source{
					stub{name: "a", rates: []sources.Rate{xmr("a", "USD", 31245, t0)}},
					stub{name: "b", rates: []sources.Rate{xmr("b", "USD", 31255, t0)}},
				}},
				Expect: Expect{Prices: map[string]string{"USD": "312.50"}},
			},
			{
				Name: "failing source is reported",
				Input: Input{[]sources.Source{
					stub{name: "a", rates: []sources.Rate{xmr("a", "USD", 30000, t0)}},
					stub{name: "down", err: errors.New("connection refused")},
					stub{name: "empty"},
				}},
				Expect: Expect{Prices: map[string]string{"USD": "300.00"}, Failed: []string{"down", "empty"}},
			},
			{
				Name: "outlier is rejected",
				Input: Input{[]sources.Source{
					stub{name: "a", rates: []sources.Rate{xmr("a", "USD", 30000, t0)}},
					stub{name: "b", rates: []sources.Rate{xmr("b", "USD", 30100, t0)}},
					stub{name: "c", rates: []sources.Rate{xmr("c", "USD", 30200, t0)}},
					stub{name: "evil", rates: []sources.Rate{xmr("evil", "USD", 90000, t0)}},
				}},
				Expect: Expect{Prices: map[string]string{"USD": "301.00"}, Rejected: []string{"evil"}},
			},
			{
				Name:   "several currencies",
				Input:  Input{priced()},
				Expect: Expect{Prices: map[string]string{"USD": "312.50", "EUR": "280.00", "BTC": "0.00305000"}},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := newController(1, test.Input.Sources...)
				report, err := c.RefreshPrices(t.Context(), logger, t0)
				if err != nil {
					t.Fatal(err)
				}
				var failed []string
				for _, r := range report.Results {
					if r.Err != nil {
						failed = append(failed, r.Source)
					}
				}
				if !slices.Equal(failed, test.Expect.Failed) {
					t.Errorf("failed %v, want %v", failed, test.Expect.Failed)
				}
				var currencies []string
				for cur, want := range test.Expect.Prices {
					currencies = append(currencies, cur)
					q, err := c.Price(t.Context(), logger, cur, t0)
					if err != nil || q.Price != want || report.Prices[cur].String() != want || !q.UpdatedAt.Equal(t0) {
						t.Errorf("%s: %+v %v, want %s", cur, q, err, want)
					}
				}
				if test.Expect.Rejected != nil && !slices.Equal(report.Prices["USD"].Rejected, test.Expect.Rejected) {
					t.Errorf("rejected %v", report.Prices["USD"].Rejected)
				}
				slices.Sort(currencies)
				if got := c.Currencies(t.Context(), logger, t0); !slices.Equal(got, currencies) {
					t.Errorf("currencies %v, want %v", got, currencies)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Sources  []sources.Source
				Canceled bool
			}
			Expect struct{ Err error }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		later := t0.Add(time.Minute)
		tests := []Test{
			{Name: "no sources", Input: Input{}, Expect: Expect{ErrNoQuotes}},
			{
				Name: "every source fails",
				Input: Input{Sources: []sources.Source{
					stub{name: "a", err: errors.New("timeout")},
					stub{name: "b", err: errors.New("451")},
				}},
				Expect: Expect{ErrNoQuotes},
			},
			{
				Name:   "only stale rates",
				Input:  Input{Sources: []sources.Source{stub{name: "a", rates: []sources.Rate{xmr("a", "USD", 40000, later.Add(-time.Hour))}}}},
				Expect: Expect{ErrNoQuotes},
			},
			{
				Name: "only btc markets",
				Input: Input{Sources: []sources.Source{stub{name: "a", rates: []sources.Rate{
					{Base: "BTC", Quote: "USD", Price: 10000000, Time: later, Source: "a"},
				}}}},
				Expect: Expect{ErrNoQuotes},
			},
			{Name: "canceled context", Input: Input{Sources: priced(), Canceled: true}, Expect: Expect{context.Canceled}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := newController(1, priced()...)
				if _, err := c.RefreshPrices(t.Context(), logger, t0); err != nil {
					t.Fatal(err)
				}
				c.sources = test.Input.Sources
				if _, err := c.RefreshPrices(ctxFor(t, test.Input.Canceled), logger, later); !errors.Is(err, test.Expect.Err) {
					t.Fatalf("got %v, want %v", err, test.Expect.Err)
				}
				q, err := c.Price(t.Context(), logger, "USD", later)
				if err != nil || q.Price != "312.50" || !q.UpdatedAt.Equal(t0) {
					t.Fatalf("previous prices lost: %+v %v", q, err)
				}
			})
		}
	})
}

func TestPrice(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Currency string
				At       time.Duration
			}
			Expect struct{ Quote Quote }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "usd", Input: Input{"USD", 0}, Expect: Expect{Quote{"USD", "312.50", t0}}},
			{Name: "lower case", Input: Input{"usd", time.Minute}, Expect: Expect{Quote{"USD", "312.50", t0}}},
			{Name: "mixed case", Input: Input{"Eur", 0}, Expect: Expect{Quote{"EUR", "280.00", t0}}},
			{Name: "btc has eight decimals", Input: Input{"btc", 0}, Expect: Expect{Quote{"BTC", "0.00305000", t0}}},
			{Name: "exactly at max age", Input: Input{"USD", 10 * time.Minute}, Expect: Expect{Quote{"USD", "312.50", t0}}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := newController(1, priced()...)
				if _, err := c.RefreshPrices(t.Context(), logger, t0); err != nil {
					t.Fatal(err)
				}
				q, err := c.Price(t.Context(), logger, test.Input.Currency, t0.Add(test.Input.At))
				if err != nil || q != test.Expect.Quote {
					t.Fatalf("got %+v %v", q, err)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Currency  string
				At        time.Duration
				Unrefresh bool
				Canceled  bool
			}
			Expect struct{ Err error }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "before the first refresh", Input: Input{Currency: "USD", Unrefresh: true}, Expect: Expect{ErrNoPrice}},
			{Name: "stale cache", Input: Input{Currency: "USD", At: 10*time.Minute + 1}, Expect: Expect{ErrNoPrice}},
			{Name: "unknown currency", Input: Input{Currency: "JPY"}, Expect: Expect{ErrUnknownCurrency}},
			{Name: "xmr itself", Input: Input{Currency: "XMR"}, Expect: Expect{ErrUnknownCurrency}},
			{Name: "empty currency", Input: Input{Currency: ""}, Expect: Expect{ErrUnknownCurrency}},
			{Name: "padded currency", Input: Input{Currency: " USD"}, Expect: Expect{ErrUnknownCurrency}},
			{Name: "canceled context", Input: Input{Currency: "USD", Canceled: true}, Expect: Expect{context.Canceled}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := newController(1, priced()...)
				if !test.Input.Unrefresh {
					if _, err := c.RefreshPrices(t.Context(), logger, t0); err != nil {
						t.Fatal(err)
					}
				}
				q, err := c.Price(ctxFor(t, test.Input.Canceled), logger, test.Input.Currency, t0.Add(test.Input.At))
				if !errors.Is(err, test.Expect.Err) || q != (Quote{}) {
					t.Fatalf("got %+v %v", q, err)
				}
			})
		}
	})
}

func TestCurrencies(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ At time.Duration }
			Expect struct{ Currencies []string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "sorted", Input: Input{0}, Expect: Expect{[]string{"BTC", "EUR", "USD"}}},
			{Name: "at max age", Input: Input{10 * time.Minute}, Expect: Expect{[]string{"BTC", "EUR", "USD"}}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := newController(1, priced()...)
				if _, err := c.RefreshPrices(t.Context(), logger, t0); err != nil {
					t.Fatal(err)
				}
				if got := c.Currencies(t.Context(), logger, t0.Add(test.Input.At)); !slices.Equal(got, test.Expect.Currencies) {
					t.Fatalf("got %v", got)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Refreshed, Canceled bool
				At                  time.Duration
			}
			Expect struct{}
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "before the first refresh", Input: Input{}},
			{Name: "stale cache", Input: Input{Refreshed: true, At: 11 * time.Minute}},
			{Name: "canceled context", Input: Input{Refreshed: true, Canceled: true}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := newController(1, priced()...)
				if test.Input.Refreshed {
					if _, err := c.RefreshPrices(t.Context(), logger, t0); err != nil {
						t.Fatal(err)
					}
				}
				if got := c.Currencies(ctxFor(t, test.Input.Canceled), logger, t0.Add(test.Input.At)); got != nil {
					t.Fatalf("got %v", got)
				}
			})
		}
	})
}

func TestTooEarlyError(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ RetryAt time.Time }
			Expect struct{ Message string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "utc", Input: Input{t0.Add(30 * time.Second)}, Expect: Expect{"challenge cannot be redeemed before 2026-11-07T12:00:30Z"}},
			{Name: "converted to utc", Input: Input{t0.In(time.FixedZone("ART", -3*3600))}, Expect: Expect{"challenge cannot be redeemed before 2026-11-07T12:00:00Z"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := (&TooEarlyError{RetryAt: test.Input.RetryAt}).Error(); got != test.Expect.Message {
					t.Fatalf("got %q", got)
				}
			})
		}
	})
}

func TestPriceWorker(t *testing.T) {
	// start runs the worker every 20ms and returns a stop that fails the test
	// if the worker does not return.
	start := func(t *testing.T, srcs []sources.Source) (*Controller, func()) {
		opt := options(1)
		opt.PriceRefresh = 20 * time.Millisecond
		c := New(opt, srcs)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			c.PriceWorker(ctx, logger)
			close(done)
		}()
		return c, func() {
			cancel()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("worker did not stop")
			}
		}
	}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Sources []sources.Source }
			Expect struct{ Price string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:   "publishes and stops",
				Input:  Input{[]sources.Source{stub{name: "a", rates: []sources.Rate{xmr("a", "EUR", 28000, time.Now())}}}},
				Expect: Expect{"280.00"},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c, stop := start(t, test.Input.Sources)
				defer stop()
				deadline := time.Now().Add(2 * time.Second)
				for {
					q, err := c.Price(t.Context(), logger, "EUR", time.Now())
					if err == nil {
						if q.Price != test.Expect.Price {
							t.Fatalf("got %+v", q)
						}
						return
					}
					if time.Now().After(deadline) {
						t.Fatal("worker never published a price")
					}
					time.Sleep(5 * time.Millisecond)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ Sources []sources.Source }
			Expect struct{ Err error }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "failing sources keep it running", Input: Input{[]sources.Source{stub{name: "a", err: errors.New("down")}}}, Expect: Expect{ErrNoPrice}},
			{Name: "no sources at all", Input: Input{nil}, Expect: Expect{ErrNoPrice}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c, stop := start(t, test.Input.Sources)
				defer stop()
				time.Sleep(60 * time.Millisecond)
				if _, err := c.Price(t.Context(), logger, "EUR", time.Now()); !errors.Is(err, test.Expect.Err) {
					t.Fatalf("got %v", err)
				}
			})
		}
	})
}

func TestChallengeJanitor(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Lifetime time.Duration }
			Expect struct{ Within time.Duration }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "cleans and stops", Input: Input{20 * time.Millisecond}, Expect: Expect{2 * time.Second}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				opt := options(10)
				opt.ChallengeLifetime, opt.ChallengeMinWait = test.Input.Lifetime, 0
				c := New(opt, nil)
				if _, err := c.RegisterChallenge(t.Context(), logger, time.Now()); err != nil {
					t.Fatal(err)
				}
				ctx, stop := context.WithCancel(t.Context())
				done := make(chan struct{})
				go func() {
					c.ChallengeJanitor(ctx, logger)
					close(done)
				}()
				deadline := time.Now().Add(test.Expect.Within)
				for c.PendingChallenges() != 0 {
					if time.Now().After(deadline) {
						t.Fatal("expired challenge never cleaned")
					}
					time.Sleep(5 * time.Millisecond)
				}
				stop()
				select {
				case <-done:
				case <-time.After(test.Expect.Within):
					t.Fatal("janitor did not stop")
				}
			})
		}
	})
}

type record struct {
	groups []string
	msg    string
}

type records struct {
	mu   sync.Mutex
	list []record
}

func (r *records) all() []record {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.list)
}

// recorder keeps the group chain of every record, which slog's own handlers
// drop when a group has no attributes.
type recorder struct {
	log    *records
	groups []string
}

func (h *recorder) Enabled(context.Context, slog.Level) bool { return true }
func (h *recorder) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *recorder) WithGroup(name string) slog.Handler {
	return &recorder{log: h.log, groups: append(slices.Clip(h.groups), name)}
}
func (h *recorder) Handle(_ context.Context, r slog.Record) error {
	h.log.mu.Lock()
	defer h.log.mu.Unlock()
	h.log.list = append(h.log.list, record{groups: h.groups, msg: r.Message})
	return nil
}

// TestLogGroups checks every method logs under its own group.
func TestLogGroups(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Call func(t *testing.T, c *Controller, l *slog.Logger)
			}
			Expect struct {
				Path []string
				Msg  string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:   "RegisterChallenge",
				Input:  Input{func(t *testing.T, c *Controller, l *slog.Logger) { c.RegisterChallenge(t.Context(), l, t0) }},
				Expect: Expect{[]string{"RegisterChallenge"}, "registered"},
			},
			{
				Name:   "Challenge",
				Input:  Input{func(t *testing.T, c *Controller, l *slog.Logger) { c.Challenge(t.Context(), l, uuid.NewV4(), t0) }},
				Expect: Expect{[]string{"Challenge"}, "not found"},
			},
			{
				Name:   "CleanChallenges",
				Input:  Input{func(t *testing.T, c *Controller, l *slog.Logger) { c.CleanChallenges(t.Context(), l, t0) }},
				Expect: Expect{[]string{"CleanChallenges"}, "expired challenges removed"},
			},
			{
				Name:   "DenyChallenge",
				Input:  Input{func(t *testing.T, c *Controller, l *slog.Logger) { c.DenyChallenge(t.Context(), l, uuid.NewV4()) }},
				Expect: Expect{[]string{"DenyChallenge"}, "removed"},
			},
			{
				Name: "ResolveChallenge",
				Input: Input{func(t *testing.T, c *Controller, l *slog.Logger) {
					hc := stamp(t, register(t, c, 0)[0].ID.String(), 4)
					c.ResolveChallenge(t.Context(), l, hc, t0.Add(time.Minute))
				}},
				Expect: Expect{[]string{"ResolveChallenge"}, "proof of work rejected"},
			},
			{
				Name: "DenyChallenge inside ResolveChallenge",
				Input: Input{func(t *testing.T, c *Controller, l *slog.Logger) {
					hc := stamp(t, register(t, c, 0)[0].ID.String(), 4)
					c.ResolveChallenge(t.Context(), l, hc, t0.Add(time.Minute))
				}},
				Expect: Expect{[]string{"ResolveChallenge", "DenyChallenge"}, "removed"},
			},
			{
				Name:   "VerifyToken",
				Input:  Input{func(t *testing.T, c *Controller, l *slog.Logger) { c.VerifyToken(t.Context(), l, "bad", t0) }},
				Expect: Expect{[]string{"VerifyToken"}, "rejected"},
			},
			{
				Name:   "issueToken",
				Input:  Input{func(t *testing.T, c *Controller, l *slog.Logger) { c.issueToken(t.Context(), l, t0) }},
				Expect: Expect{[]string{"issueToken"}, "issued"},
			},
			{
				Name:   "Price",
				Input:  Input{func(t *testing.T, c *Controller, l *slog.Logger) { c.Price(t.Context(), l, "USD", t0) }},
				Expect: Expect{[]string{"Price"}, "no recent prices in cache"},
			},
			{
				Name:   "Currencies",
				Input:  Input{func(t *testing.T, c *Controller, l *slog.Logger) { c.Currencies(t.Context(), l, t0) }},
				Expect: Expect{[]string{"Currencies"}, "no recent prices in cache"},
			},
			{
				Name: "RefreshPrices",
				Input: Input{func(t *testing.T, c *Controller, l *slog.Logger) {
					c.sources = priced()
					c.RefreshPrices(t.Context(), l, t0)
				}},
				Expect: Expect{[]string{"RefreshPrices"}, "prices updated"},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				rec := &recorder{log: &records{}}
				test.Input.Call(t, newController(10), slog.New(rec))
				for _, r := range rec.log.all() {
					if r.msg == test.Expect.Msg && slices.Equal(r.groups, test.Expect.Path) {
						return
					}
				}
				t.Fatalf("no %q under %v in %+v", test.Expect.Msg, test.Expect.Path, rec.log.all())
			})
		}
	})
}
