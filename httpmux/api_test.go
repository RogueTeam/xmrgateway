package httpmux

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/RogueTeam/xmrgateway/api"
	"github.com/RogueTeam/xmrgateway/internal/config"
)

func checkJSON(t *testing.T, r reply) {
	t.Helper()
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type %q", ct)
	}
	if cc := r.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control %q", cc)
	}
}

func checkError(t *testing.T, r reply, status int, msg string) {
	t.Helper()
	if r.Status != status {
		t.Fatalf("status %d, want %d: %s", r.Status, status, r.Body)
	}
	checkJSON(t, r)
	if got := decode[api.Error](t, r.Body).Message; !strings.HasPrefix(got, msg) {
		t.Fatalf("error %q, want it to start with %q", got, msg)
	}
}

func TestCreateChallenge(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Mod   func(*config.Config)
				Clock time.Duration
			}
			Expect struct {
				Difficulty           int
				NotBefore, ExpiresAt time.Time
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "defaults", Input: Input{}, Expect: Expect{8, t0.Add(30 * time.Second), t0.Add(time.Hour)}},
			{
				Name: "from the config",
				Input: Input{func(c *config.Config) {
					c.PowDifficulty, c.ChallengeMinWait, c.ChallengeLifetime = 12, config.Duration{Duration: 10 * time.Second}, config.Duration{Duration: 2 * time.Hour}
				}, 5 * time.Minute},
				Expect: Expect{12, t0.Add(5*time.Minute + 10*time.Second), t0.Add(2*time.Hour + 5*time.Minute)},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				e := newEnv(t, test.Input.Mod)
				e.clock.Set(test.Input.Clock)
				r := e.do(t, t.Context(), http.MethodPost, "/api/v1/challenges", nil, "")
				if r.Status != http.StatusCreated {
					t.Fatalf("status %d: %s", r.Status, r.Body)
				}
				checkJSON(t, r)
				ch := decode[api.Challenge](t, r.Body)
				if _, err := uuid.Parse(ch.ID); err != nil {
					t.Fatalf("id %q: %v", ch.ID, err)
				}
				want := api.Challenge{ID: ch.ID, Difficulty: test.Expect.Difficulty, NotBefore: test.Expect.NotBefore, ExpiresAt: test.Expect.ExpiresAt}
				if ch != want {
					t.Fatalf("got  %+v\nwant %+v", ch, want)
				}
				if again := e.get(t, "/api/v1/challenges/"+ch.ID); again.Body != r.Body {
					t.Fatalf("lookup differs: %s", again.Body)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Mod      func(*config.Config)
				Existing int
				Canceled bool
				Method   string
			}
			Expect struct {
				Status     int
				Error      string
				RetryAfter string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:   "too many pending",
				Input:  Input{Mod: func(c *config.Config) { c.MaxChallenges = 2 }, Existing: 2},
				Expect: Expect{http.StatusServiceUnavailable, "too many pending challenges", "60"},
			},
			{Name: "canceled request", Input: Input{Canceled: true}, Expect: Expect{http.StatusServiceUnavailable, "context canceled", ""}},
			{Name: "wrong method", Input: Input{Method: http.MethodPut}, Expect: Expect{Status: http.StatusMethodNotAllowed}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				e := newEnv(t, test.Input.Mod)
				for range test.Input.Existing {
					e.register(t)
				}
				ctx := t.Context()
				if test.Input.Canceled {
					ctx = canceled(t)
				}
				method := test.Input.Method
				if method == "" {
					method = http.MethodPost
				}
				r := e.do(t, ctx, method, "/api/v1/challenges", nil, "")
				if test.Expect.Error == "" {
					if r.Status != test.Expect.Status {
						t.Fatalf("status %d", r.Status)
					}
				} else {
					checkError(t, r, test.Expect.Status, test.Expect.Error)
				}
				if ra := r.Header.Get("Retry-After"); ra != test.Expect.RetryAfter {
					t.Fatalf("Retry-After %q", ra)
				}
				if n := e.ctrl.PendingChallenges(); n != test.Input.Existing {
					t.Fatalf("pending %d", n)
				}
			})
		}
	})
}

func TestGetChallenge(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Clock time.Duration }
			Expect struct{ Status int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "right away", Input: Input{0}, Expect: Expect{http.StatusOK}},
			{Name: "after the window opened", Input: Input{time.Minute}, Expect: Expect{http.StatusOK}},
			{Name: "last second", Input: Input{time.Hour - time.Second}, Expect: Expect{http.StatusOK}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				e := newEnv(t, nil)
				ch := e.register(t)
				e.clock.Set(test.Input.Clock)
				r := e.get(t, "/api/v1/challenges/"+ch.ID.String())
				if r.Status != test.Expect.Status {
					t.Fatalf("status %d: %s", r.Status, r.Body)
				}
				checkJSON(t, r)
				want := api.Challenge{ID: ch.ID.String(), Difficulty: 8, NotBefore: ch.NotBefore, ExpiresAt: ch.ExpiresAt}
				if got := decode[api.Challenge](t, r.Body); got != want {
					t.Fatalf("got %+v", got)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				ID       func(real string) string
				Clock    time.Duration
				Redeemed bool
			}
			Expect struct {
				Status int
				Error  string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		same := func(id string) string { return id }
		notFound := Expect{http.StatusNotFound, "unknown or already used challenge"}
		tests := []Test{
			{Name: "unknown id", Input: Input{ID: func(string) string { return uuid.NewV4().String() }}, Expect: notFound},
			{Name: "not a uuid", Input: Input{ID: func(string) string { return "nope" }}, Expect: notFound},
			{Name: "truncated id", Input: Input{ID: func(id string) string { return id[:35] }}, Expect: notFound},
			{Name: "expired", Input: Input{ID: same, Clock: time.Hour}, Expect: notFound},
			{Name: "already redeemed", Input: Input{ID: same, Redeemed: true}, Expect: notFound},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				e := newEnv(t, nil)
				ch := e.register(t)
				if test.Input.Redeemed {
					if _, err := e.ctrl.ResolveChallenge(t.Context(), logger, solve(t, ch.ID.String(), 8), ch.NotBefore); err != nil {
						t.Fatal(err)
					}
				}
				e.clock.Set(test.Input.Clock)
				checkError(t, e.get(t, "/api/v1/challenges/"+test.Input.ID(ch.ID.String())), test.Expect.Status, test.Expect.Error)
			})
		}
	})
}

func TestRedeem(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Bits  int
				Clock time.Duration
			}
			Expect struct{ ExpiresAt time.Time }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		validity := 2160 * time.Hour
		tests := []Test{
			{Name: "when the window opens", Input: Input{8, 30 * time.Second}, Expect: Expect{t0.Add(30*time.Second + validity)}},
			{Name: "more work than asked", Input: Input{12, time.Minute}, Expect: Expect{t0.Add(time.Minute + validity)}},
			{Name: "last second", Input: Input{8, time.Hour - time.Second}, Expect: Expect{t0.Add(time.Hour - time.Second + validity)}},
			{Name: "expiry drops sub-seconds", Input: Input{8, 30*time.Second + 900*time.Millisecond}, Expect: Expect{t0.Add(30*time.Second + validity)}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				e := newEnv(t, nil)
				ch := e.register(t)
				e.clock.Set(test.Input.Clock)
				body := `{"stamp":"` + solve(t, ch.ID.String(), test.Input.Bits) + `"}`
				r := e.do(t, t.Context(), http.MethodPost, "/api/v1/tokens", nil, body)
				if r.Status != http.StatusCreated {
					t.Fatalf("status %d: %s", r.Status, r.Body)
				}
				checkJSON(t, r)
				tok := decode[api.Token](t, r.Body)
				if !tok.ExpiresAt.Equal(test.Expect.ExpiresAt) || tok.Token == "" {
					t.Fatalf("token %+v", tok)
				}
				price := e.do(t, t.Context(), http.MethodGet, "/api/v1/price", http.Header{api.TokenHeader: {tok.Token}}, "")
				if price.Status == http.StatusUnauthorized || price.Status == http.StatusTooManyRequests {
					t.Fatalf("new token refused: %d %s", price.Status, price.Body)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Body     func(t *testing.T, stamp string) string
				Clock    time.Duration
				Canceled bool
				Twice    bool
			}
			Expect struct {
				Status     int
				Error      string
				RetryAfter string
				RetryAt    time.Time
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		valid := func(_ *testing.T, stamp string) string { return `{"stamp":"` + stamp + `"}` }
		raw := func(body string) func(*testing.T, string) string {
			return func(*testing.T, string) string { return body }
		}
		badBody := `body must be {"stamp": "..."}`
		tests := []Test{
			{Name: "not json", Input: Input{Body: raw("stamp=1:8:x"), Clock: time.Minute}, Expect: Expect{Status: 400, Error: badBody}},
			{Name: "empty body", Input: Input{Body: raw(""), Clock: time.Minute}, Expect: Expect{Status: 400, Error: badBody}},
			{Name: "stamp is not a string", Input: Input{Body: raw(`{"stamp":1}`), Clock: time.Minute}, Expect: Expect{Status: 400, Error: badBody}},
			{Name: "body over 4 KiB", Input: Input{Body: raw(`{"stamp":"` + strings.Repeat("x", 5000) + `"}`), Clock: time.Minute}, Expect: Expect{Status: 400, Error: badBody}},
			{Name: "malformed stamp", Input: Input{Body: raw(`{"stamp":"1:8:x"}`), Clock: time.Minute}, Expect: Expect{Status: 400, Error: "malformed stamp: incomplete hashcash"}},
			{Name: "missing stamp", Input: Input{Body: raw(`{}`), Clock: time.Minute}, Expect: Expect{Status: 400, Error: "malformed stamp"}},
			{
				Name:   "payload is not a challenge",
				Input:  Input{Body: func(t *testing.T, _ string) string { return valid(t, solve(t, "hello", 8)) }, Clock: time.Minute},
				Expect: Expect{Status: 400, Error: "malformed stamp: payload is not a challenge id"},
			},
			{
				Name:   "too early",
				Input:  Input{Body: valid, Clock: 10 * time.Second},
				Expect: Expect{Status: 425, Error: "challenge cannot be redeemed before 2026-11-07T12:00:30Z", RetryAfter: "20", RetryAt: t0.Add(30 * time.Second)},
			},
			{
				Name:   "a nanosecond too early",
				Input:  Input{Body: valid, Clock: 30*time.Second - 1},
				Expect: Expect{Status: 425, Error: "challenge cannot be redeemed", RetryAfter: "1", RetryAt: t0.Add(30 * time.Second)},
			},
			{
				Name:   "claims more work than done",
				Input:  Input{Body: func(t *testing.T, stamp string) string { return valid(t, strings.Replace(stamp, "1:8:", "1:30:", 1)) }, Clock: time.Minute},
				Expect: Expect{Status: 403, Error: "stamp does not meet the difficulty"},
			},
			{
				Name:   "below the difficulty",
				Input:  Input{Body: func(t *testing.T, stamp string) string { return valid(t, strings.Replace(stamp, "1:8:", "1:4:", 1)) }, Clock: time.Minute},
				Expect: Expect{Status: 403, Error: "stamp does not meet the difficulty: expecting at least a difficulty of: 8"},
			},
			{
				Name:   "unknown challenge",
				Input:  Input{Body: func(t *testing.T, _ string) string { return valid(t, solve(t, uuid.NewV4().String(), 8)) }, Clock: time.Minute},
				Expect: Expect{Status: 404, Error: "unknown or already used challenge"},
			},
			{Name: "redeemed twice", Input: Input{Body: valid, Clock: time.Minute, Twice: true}, Expect: Expect{Status: 404, Error: "unknown or already used challenge"}},
			{Name: "expired", Input: Input{Body: valid, Clock: time.Hour}, Expect: Expect{Status: 410, Error: "challenge expired"}},
			{Name: "canceled request", Input: Input{Body: valid, Clock: time.Minute, Canceled: true}, Expect: Expect{Status: 503, Error: "request timed out"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				e := newEnv(t, nil)
				ch := e.register(t)
				e.clock.Set(test.Input.Clock)
				body := test.Input.Body(t, solve(t, ch.ID.String(), 8))
				if test.Input.Twice {
					if r := e.do(t, t.Context(), http.MethodPost, "/api/v1/tokens", nil, body); r.Status != http.StatusCreated {
						t.Fatalf("first redeem: %d %s", r.Status, r.Body)
					}
				}
				ctx := t.Context()
				if test.Input.Canceled {
					ctx = canceled(t)
				}
				r := e.do(t, ctx, http.MethodPost, "/api/v1/tokens", nil, body)
				checkError(t, r, test.Expect.Status, test.Expect.Error)
				if ra := r.Header.Get("Retry-After"); ra != test.Expect.RetryAfter {
					t.Errorf("Retry-After %q, want %q", ra, test.Expect.RetryAfter)
				}
				retryAt := decode[api.Error](t, r.Body).RetryAt
				if test.Expect.RetryAt.IsZero() != (retryAt == nil) || (retryAt != nil && !retryAt.Equal(test.Expect.RetryAt)) {
					t.Errorf("retry_at %v, want %v", retryAt, test.Expect.RetryAt)
				}
			})
		}
	})
}

func TestPrice(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Query   string
				Clock   time.Duration
				Earlier []time.Duration
			}
			Expect struct{ Price api.Price }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		quote := func(currency, price string, next time.Duration) api.Price {
			return api.Price{Currency: currency, Price: price, UpdatedAt: t0, NextRequestAt: t0.Add(next)}
		}
		tests := []Test{
			{Name: "usd by default", Input: Input{Query: ""}, Expect: Expect{quote("USD", "312.45", 30*time.Second)}},
			{Name: "lower case", Input: Input{Query: "?currency=eur", Clock: time.Minute}, Expect: Expect{quote("EUR", "280.00", 90*time.Second)}},
			{Name: "btc", Input: Input{Query: "?currency=BTC"}, Expect: Expect{quote("BTC", "0.00305000", 30*time.Second)}},
			{Name: "empty currency means usd", Input: Input{Query: "?currency="}, Expect: Expect{quote("USD", "312.45", 30*time.Second)}},
			{Name: "again once the window passed", Input: Input{Clock: 30 * time.Second, Earlier: []time.Duration{0}}, Expect: Expect{quote("USD", "312.45", time.Minute)}},
			{Name: "cache at its max age", Input: Input{Clock: 30 * time.Minute}, Expect: Expect{quote("USD", "312.45", 30*time.Minute+30*time.Second)}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				e := newEnv(t, nil)
				header := http.Header{api.TokenHeader: {e.token(t)}}
				for _, at := range test.Input.Earlier {
					e.clock.Set(at)
					if r := e.do(t, t.Context(), http.MethodGet, "/api/v1/price"+test.Input.Query, header, ""); r.Status != http.StatusOK {
						t.Fatalf("earlier request: %d %s", r.Status, r.Body)
					}
				}
				e.clock.Set(test.Input.Clock)
				r := e.do(t, t.Context(), http.MethodGet, "/api/v1/price"+test.Input.Query, header, "")
				if r.Status != http.StatusOK {
					t.Fatalf("status %d: %s", r.Status, r.Body)
				}
				checkJSON(t, r)
				if got := decode[api.Price](t, r.Body); got != test.Expect.Price {
					t.Fatalf("got  %+v\nwant %+v", got, test.Expect.Price)
				}
				if ra := r.Header.Get("Retry-After"); ra != "" {
					t.Fatalf("Retry-After %q on success", ra)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Token   func(real string) string
				Query   string
				Clock   time.Duration
				Earlier []time.Duration
			}
			Expect struct {
				Status     int
				Error      string
				RetryAfter string
				RetryAt    time.Time
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		same := func(tok string) string { return tok }
		tests := []Test{
			{
				Name:   "no token",
				Input:  Input{Token: func(string) string { return "" }},
				Expect: Expect{Status: 429, Error: "missing X-API-Token header, get a token at /token"},
			},
			{Name: "forged token", Input: Input{Token: func(string) string { return "forged" }}, Expect: Expect{Status: 401, Error: "invalid or expired token"}},
			{Name: "tampered token", Input: Input{Token: func(tok string) string {
				b := []byte(tok)
				b[len(b)/2] ^= 1
				return string(b)
			}}, Expect: Expect{Status: 401, Error: "invalid or expired token"}},
			{Name: "expired token", Input: Input{Token: same, Clock: 2160 * time.Hour}, Expect: Expect{Status: 401, Error: "invalid or expired token"}},
			{Name: "token from the future", Input: Input{Token: same, Clock: -time.Second}, Expect: Expect{Status: 401, Error: "invalid or expired token"}},
			{
				Name:   "rate limited",
				Input:  Input{Token: same, Clock: 10 * time.Second, Earlier: []time.Duration{0}},
				Expect: Expect{Status: 429, Error: "rate limited", RetryAfter: "20", RetryAt: t0.Add(30 * time.Second)},
			},
			{
				Name:   "a nanosecond early",
				Input:  Input{Token: same, Clock: 30*time.Second - 1, Earlier: []time.Duration{0}},
				Expect: Expect{Status: 429, Error: "rate limited", RetryAfter: "1", RetryAt: t0.Add(30 * time.Second)},
			},
			{
				Name:   "refusals do not extend the wait",
				Input:  Input{Token: same, Clock: 25 * time.Second, Earlier: []time.Duration{0, 5 * time.Second, 20 * time.Second}},
				Expect: Expect{Status: 429, Error: "rate limited", RetryAfter: "5", RetryAt: t0.Add(30 * time.Second)},
			},
			{Name: "unknown currency", Input: Input{Token: same, Query: "?currency=JPY"}, Expect: Expect{Status: 404, Error: "unknown currency"}},
			{Name: "xmr in xmr", Input: Input{Token: same, Query: "?currency=XMR"}, Expect: Expect{Status: 404, Error: "unknown currency"}},
			{Name: "stale cache", Input: Input{Token: same, Clock: 30*time.Minute + time.Second}, Expect: Expect{Status: 503, Error: "no recent price available"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				e := newEnv(t, nil)
				header := http.Header{}
				if tok := test.Input.Token(e.token(t)); tok != "" {
					header.Set(api.TokenHeader, tok)
				}
				for _, at := range test.Input.Earlier {
					e.clock.Set(at)
					e.do(t, t.Context(), http.MethodGet, "/api/v1/price"+test.Input.Query, header, "")
				}
				e.clock.Set(test.Input.Clock)
				r := e.do(t, t.Context(), http.MethodGet, "/api/v1/price"+test.Input.Query, header, "")
				checkError(t, r, test.Expect.Status, test.Expect.Error)
				if ra := r.Header.Get("Retry-After"); ra != test.Expect.RetryAfter {
					t.Errorf("Retry-After %q, want %q", ra, test.Expect.RetryAfter)
				}
				retryAt := decode[api.Error](t, r.Body).RetryAt
				if test.Expect.RetryAt.IsZero() != (retryAt == nil) || (retryAt != nil && !retryAt.Equal(test.Expect.RetryAt)) {
					t.Errorf("retry_at %v, want %v", retryAt, test.Expect.RetryAt)
				}
			})
		}
	})
}

// TestTokensAreIndependent makes sure one client's window does not block another's.
func TestTokensAreIndependent(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Tokens int }
			Expect struct{ OK int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "three tokens, same second", Input: Input{3}, Expect: Expect{3}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				e := newEnv(t, nil)
				ok := 0
				for range test.Input.Tokens {
					r := e.do(t, context.Background(), http.MethodGet, "/api/v1/price", http.Header{api.TokenHeader: {e.token(t)}}, "")
					if r.Status == http.StatusOK {
						ok++
					}
				}
				if ok != test.Expect.OK {
					t.Fatalf("%d of %d tokens answered", ok, test.Input.Tokens)
				}
			})
		}
	})
}
