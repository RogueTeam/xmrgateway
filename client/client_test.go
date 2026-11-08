package client

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RogueTeam/xmrgateway/api"
	"github.com/RogueTeam/xmrgateway/hashcash"
)

const challengeID = "0d5e4bd2-2c32-4f3e-9a7a-2f6a0e1c9b11"

var t0 = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

type step struct {
	Status int
	Body   string
}

type seen struct {
	Method, URI, ContentType, Token, Body string
}

// replay answers every request with reply and records what it got.
func replay(t *testing.T, reply step) (*httptest.Server, *seen) {
	t.Helper()
	got := &seen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*got = seen{r.Method, r.URL.RequestURI(), r.Header.Get("Content-Type"), r.Header.Get(api.TokenHeader), string(b)}
		w.WriteHeader(reply.Status)
		io.WriteString(w, reply.Body)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestCalls(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Reply step
				Slash bool
				Call  func(ctx context.Context, c *Client) (any, error)
			}
			Expect struct {
				Request seen
				Result  any
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		challengeJSON := `{"id":"` + challengeID + `","difficulty":24,"not_before":"2026-11-07T12:00:30Z","expires_at":"2026-11-07T13:00:00Z"}`
		challenge := api.Challenge{ID: challengeID, Difficulty: 24, NotBefore: t0.Add(30 * time.Second), ExpiresAt: t0.Add(time.Hour)}
		tests := []Test{
			{
				Name: "new challenge",
				Input: Input{Reply: step{201, challengeJSON}, Call: func(ctx context.Context, c *Client) (any, error) {
					return c.NewChallenge(ctx)
				}},
				Expect: Expect{seen{Method: "POST", URI: "/api/v1/challenges"}, challenge},
			},
			{
				Name: "existing challenge",
				Input: Input{Reply: step{200, challengeJSON}, Call: func(ctx context.Context, c *Client) (any, error) {
					return c.Challenge(ctx, challengeID)
				}},
				Expect: Expect{seen{Method: "GET", URI: "/api/v1/challenges/" + challengeID}, challenge},
			},
			{
				Name: "challenge id is escaped",
				Input: Input{Reply: step{200, challengeJSON}, Call: func(ctx context.Context, c *Client) (any, error) {
					return c.Challenge(ctx, "../tokens?x")
				}},
				Expect: Expect{seen{Method: "GET", URI: "/api/v1/challenges/..%2Ftokens%3Fx"}, challenge},
			},
			{
				Name: "redeem",
				Input: Input{Reply: step{201, `{"token":"abc","expires_at":"2027-02-05T12:00:00Z"}`}, Call: func(ctx context.Context, c *Client) (any, error) {
					return c.Redeem(ctx, "1:24:t:p::r:c")
				}},
				Expect: Expect{
					seen{Method: "POST", URI: "/api/v1/tokens", ContentType: "application/json", Body: `{"stamp":"1:24:t:p::r:c"}`},
					api.Token{Token: "abc", ExpiresAt: time.Date(2027, 2, 5, 12, 0, 0, 0, time.UTC)},
				},
			},
			{
				Name: "price",
				Input: Input{Reply: step{200, `{"currency":"USD","price":"312.45","updated_at":"2026-11-07T12:00:00Z","next_request_at":"2026-11-07T12:00:30Z"}`}, Call: func(ctx context.Context, c *Client) (any, error) {
					return c.Price(ctx, "tok", "usd")
				}},
				Expect: Expect{
					seen{Method: "GET", URI: "/api/v1/price?currency=usd", Token: "tok"},
					api.Price{Currency: "USD", Price: "312.45", UpdatedAt: t0, NextRequestAt: t0.Add(30 * time.Second)},
				},
			},
			{
				Name: "currency is escaped",
				Input: Input{Reply: step{200, `{"currency":"USD","price":"1.00"}`}, Call: func(ctx context.Context, c *Client) (any, error) {
					return c.Price(ctx, "tok", "usd&x=1")
				}},
				Expect: Expect{seen{Method: "GET", URI: "/api/v1/price?currency=usd%26x%3D1", Token: "tok"}, api.Price{Currency: "USD", Price: "1.00"}},
			},
			{
				Name: "base url with trailing slash",
				Input: Input{Reply: step{201, challengeJSON}, Slash: true, Call: func(ctx context.Context, c *Client) (any, error) {
					return c.NewChallenge(ctx)
				}},
				Expect: Expect{seen{Method: "POST", URI: "/api/v1/challenges"}, challenge},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				srv, got := replay(t, test.Input.Reply)
				base := srv.URL
				if test.Input.Slash {
					base += "/"
				}
				res, err := test.Input.Call(t.Context(), &Client{BaseURL: base})
				if err != nil {
					t.Fatal(err)
				}
				if *got != test.Expect.Request {
					t.Errorf("request %+v\nwant    %+v", *got, test.Expect.Request)
				}
				if !reflect.DeepEqual(res, test.Expect.Result) {
					t.Errorf("result %+v\nwant   %+v", res, test.Expect.Result)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Reply    step
				BaseURL  string
				Closed   bool
				Canceled bool
			}
			Expect struct {
				API         *APIError
				Message     string
				ErrContains string
				Err         error
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		retry := t0.Add(30 * time.Second)
		tests := []Test{
			{
				Name:   "rate limited",
				Input:  Input{Reply: step{429, `{"error":"rate limited","retry_at":"2026-11-07T12:00:30Z"}`}},
				Expect: Expect{API: &APIError{429, api.Error{Message: "rate limited", RetryAt: &retry}}, Message: "429 Too Many Requests: rate limited"},
			},
			{
				Name:   "error without json",
				Input:  Input{Reply: step{502, "<html>bad gateway</html>"}},
				Expect: Expect{API: &APIError{502, api.Error{Message: "502 Bad Gateway"}}, Message: "502 Bad Gateway: 502 Bad Gateway"},
			},
			{
				Name:   "error with empty body",
				Input:  Input{Reply: step{404, ""}},
				Expect: Expect{API: &APIError{404, api.Error{Message: "404 Not Found"}}, Message: "404 Not Found: 404 Not Found"},
			},
			{
				Name:   "redirects are errors",
				Input:  Input{Reply: step{304, ""}},
				Expect: Expect{API: &APIError{304, api.Error{Message: "304 Not Modified"}}},
			},
			{Name: "success with broken json", Input: Input{Reply: step{200, `{"currency":`}}, Expect: Expect{ErrContains: "unexpected EOF"}},
			{Name: "success with empty body", Input: Input{Reply: step{200, ``}}, Expect: Expect{Err: io.EOF}},
			{Name: "server gone", Input: Input{Closed: true}, Expect: Expect{ErrContains: "connect"}},
			{Name: "bad base url", Input: Input{BaseURL: "http://[::1"}, Expect: Expect{ErrContains: "missing ']'"}},
			{Name: "canceled context", Input: Input{Reply: step{200, `{}`}, Canceled: true}, Expect: Expect{Err: context.Canceled}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				srv, _ := replay(t, test.Input.Reply)
				base := srv.URL
				if test.Input.BaseURL != "" {
					base = test.Input.BaseURL
				}
				if test.Input.Closed {
					srv.Close()
				}
				ctx := t.Context()
				if test.Input.Canceled {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				p, err := (&Client{BaseURL: base, HTTP: &http.Client{Timeout: 5 * time.Second}}).Price(ctx, "tok", "USD")
				if err == nil || p != (api.Price{}) {
					t.Fatalf("got %+v, %v", p, err)
				}
				var apiErr *APIError
				isAPI := errors.As(err, &apiErr)
				switch {
				case test.Expect.API != nil:
					if !isAPI || !reflect.DeepEqual(apiErr, test.Expect.API) {
						t.Fatalf("got %#v, want %#v", err, test.Expect.API)
					}
					if test.Expect.Message != "" && err.Error() != test.Expect.Message {
						t.Fatalf("message %q", err.Error())
					}
				case isAPI:
					t.Fatalf("unexpected API error %v", err)
				case test.Expect.Err != nil && !errors.Is(err, test.Expect.Err):
					t.Fatalf("got %v, want %v", err, test.Expect.Err)
				case !strings.Contains(err.Error(), test.Expect.ErrContains):
					t.Fatalf("got %v, want %q", err, test.Expect.ErrContains)
				}
			})
		}
	})
}

func TestSolve(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				ID         string
				Difficulty int
			}
			Expect struct{}
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "zero", Input: Input{challengeID, 0}},
			{Name: "8 bits", Input: Input{challengeID, 8}},
			{Name: "16 bits", Input: Input{challengeID, 16}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				stamp, err := Solve(t.Context(), test.Input.ID, test.Input.Difficulty)
				if err != nil {
					t.Fatal(err)
				}
				if err := hashcash.Verify(sha256.Sum224, stamp, test.Input.Difficulty); err != nil {
					t.Fatal(err)
				}
				var d hashcash.Details
				if err := d.FromHashcash(stamp); err != nil || d.Payload != test.Input.ID || d.LeadingZeroes != test.Input.Difficulty {
					t.Fatalf("details %+v %v", d, err)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Difficulty int
				Timeout    time.Duration
			}
			Expect struct{ Err error }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "canceled", Input: Input{60, 0}, Expect: Expect{context.Canceled}},
			{Name: "too hard for the deadline", Input: Input{60, 30 * time.Millisecond}, Expect: Expect{context.DeadlineExceeded}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				if test.Input.Timeout > 0 {
					ctx, cancel = context.WithTimeout(t.Context(), test.Input.Timeout)
				} else {
					cancel()
				}
				defer cancel()
				stamp, err := Solve(ctx, challengeID, test.Input.Difficulty)
				if !errors.Is(err, test.Expect.Err) || stamp != "" {
					t.Fatalf("got %q, %v", stamp, err)
				}
			})
		}
	})
}

// gateway fakes the server side of the token flow. Redeem answers are used in
// order; a zero step means "accept the stamp".
type gateway struct {
	t         *testing.T
	challenge step
	redeems   []step

	mu       sync.Mutex
	paths    []string
	redeemed []time.Time
}

func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.paths = append(g.paths, r.Method+" "+r.URL.Path)
	if r.URL.Path != "/api/v1/tokens" {
		w.WriteHeader(g.challenge.Status)
		io.WriteString(w, g.challenge.Body)
		return
	}
	g.redeemed = append(g.redeemed, time.Now())
	var req api.RedeemRequest
	json.NewDecoder(r.Body).Decode(&req)
	var d hashcash.Details
	if err := d.FromHashcash(req.Stamp); err != nil || d.Payload != challengeID || hashcash.Verify(sha256.Sum224, req.Stamp, 8) != nil {
		g.t.Errorf("bad stamp %q", req.Stamp)
	}
	next := step{}
	if len(g.redeems) > 0 {
		next, g.redeems = g.redeems[0], g.redeems[1:]
	}
	if next.Status == 0 {
		next = step{201, `{"token":"granted","expires_at":"2027-02-05T12:00:00Z"}`}
	}
	w.WriteHeader(next.Status)
	io.WriteString(w, next.Body)
}

func challengeBody(notBefore time.Time) string {
	b, _ := json.Marshal(api.Challenge{ID: challengeID, Difficulty: 8, NotBefore: notBefore, ExpiresAt: notBefore.Add(time.Hour)})
	return string(b)
}

func TestToken(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				ChallengeID string
				NotBefore   time.Duration // from now
				Redeems     func(now time.Time) []step
			}
			Expect struct {
				Paths   []string
				MinWait time.Duration
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:   "new challenge",
				Input:  Input{NotBefore: -time.Minute},
				Expect: Expect{Paths: []string{"POST /api/v1/challenges", "POST /api/v1/tokens"}},
			},
			{
				Name:   "given challenge",
				Input:  Input{ChallengeID: challengeID, NotBefore: -time.Minute},
				Expect: Expect{Paths: []string{"GET /api/v1/challenges/" + challengeID, "POST /api/v1/tokens"}},
			},
			{
				Name:   "waits for not_before",
				Input:  Input{NotBefore: 150 * time.Millisecond},
				Expect: Expect{Paths: []string{"POST /api/v1/challenges", "POST /api/v1/tokens"}, MinWait: 150 * time.Millisecond},
			},
			{
				Name: "retries once after too early",
				Input: Input{NotBefore: -time.Minute, Redeems: func(now time.Time) []step {
					retry := now.Add(-800 * time.Millisecond).UTC().Format(time.RFC3339Nano)
					return []step{{425, `{"error":"too early","retry_at":"` + retry + `"}`}}
				}},
				Expect: Expect{Paths: []string{"POST /api/v1/challenges", "POST /api/v1/tokens", "POST /api/v1/tokens"}, MinWait: 150 * time.Millisecond},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				now := time.Now()
				g := &gateway{t: t, challenge: step{201, challengeBody(now.Add(test.Input.NotBefore))}}
				if test.Input.Redeems != nil {
					g.redeems = test.Input.Redeems(now)
				}
				srv := httptest.NewServer(g)
				defer srv.Close()

				tok, err := (&Client{BaseURL: srv.URL}).Token(t.Context(), test.Input.ChallengeID)
				if err != nil {
					t.Fatal(err)
				}
				if tok.Token != "granted" {
					t.Fatalf("token %+v", tok)
				}
				if !reflect.DeepEqual(g.paths, test.Expect.Paths) {
					t.Fatalf("paths %v", g.paths)
				}
				if waited := g.redeemed[len(g.redeemed)-1].Sub(now); waited < test.Expect.MinWait {
					t.Fatalf("redeemed after %v, want at least %v", waited, test.Expect.MinWait)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Challenge   step
				ChallengeID string
				Redeems     []step
				Timeout     time.Duration
				Difficulty  int
			}
			Expect struct {
				Status   int
				Err      error
				Redeemed int
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		past := challengeBody(time.Now().Add(-time.Minute))
		tests := []Test{
			{Name: "no challenge slots", Input: Input{Challenge: step{503, `{"error":"too many pending challenges"}`}}, Expect: Expect{Status: 503}},
			{Name: "unknown challenge id", Input: Input{Challenge: step{404, `{"error":"unknown challenge"}`}, ChallengeID: "nope"}, Expect: Expect{Status: 404}},
			{Name: "broken challenge json", Input: Input{Challenge: step{201, `{"id":`}}, Expect: Expect{Err: io.ErrUnexpectedEOF}},
			{Name: "proof rejected", Input: Input{Challenge: step{201, past}, Redeems: []step{{403, `{"error":"stamp does not meet the difficulty"}`}}}, Expect: Expect{Status: 403, Redeemed: 1}},
			{Name: "too early without a retry time", Input: Input{Challenge: step{201, past}, Redeems: []step{{425, `{"error":"too early"}`}}}, Expect: Expect{Status: 425, Redeemed: 1}},
			{
				Name: "too early twice",
				Input: Input{Challenge: step{201, past}, Redeems: []step{
					{425, `{"error":"too early","retry_at":"2000-01-01T00:00:00Z"}`},
					{425, `{"error":"too early","retry_at":"2000-01-01T00:00:00Z"}`},
				}},
				Expect: Expect{Status: 425, Redeemed: 2},
			},
			{
				Name:   "deadline while waiting for not_before",
				Input:  Input{Challenge: step{201, challengeBody(time.Now().Add(time.Hour))}, Timeout: 50 * time.Millisecond},
				Expect: Expect{Err: context.DeadlineExceeded},
			},
			{
				Name: "deadline while waiting to retry",
				Input: Input{Challenge: step{201, past}, Timeout: 300 * time.Millisecond, Redeems: []step{
					{425, `{"error":"too early","retry_at":"2099-01-01T00:00:00Z"}`},
				}},
				Expect: Expect{Err: context.DeadlineExceeded, Redeemed: 1},
			},
			{
				Name:   "deadline while solving",
				Input:  Input{Challenge: step{201, strings.Replace(past, `"difficulty":8`, `"difficulty":60`, 1)}, Timeout: 50 * time.Millisecond},
				Expect: Expect{Err: context.DeadlineExceeded},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				g := &gateway{t: t, challenge: test.Input.Challenge, redeems: test.Input.Redeems}
				srv := httptest.NewServer(g)
				defer srv.Close()
				ctx := t.Context()
				if test.Input.Timeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, test.Input.Timeout)
					defer cancel()
				}

				tok, err := (&Client{BaseURL: srv.URL}).Token(ctx, test.Input.ChallengeID)
				if err == nil || tok != (api.Token{}) {
					t.Fatalf("got %+v, %v", tok, err)
				}
				var apiErr *APIError
				if test.Expect.Status != 0 && (!errors.As(err, &apiErr) || apiErr.Status != test.Expect.Status) {
					t.Fatalf("got %v, want status %d", err, test.Expect.Status)
				}
				if test.Expect.Err != nil && !errors.Is(err, test.Expect.Err) {
					t.Fatalf("got %v, want %v", err, test.Expect.Err)
				}
				g.mu.Lock()
				defer g.mu.Unlock()
				if len(g.redeemed) != test.Expect.Redeemed {
					t.Fatalf("redeemed %d times, want %d", len(g.redeemed), test.Expect.Redeemed)
				}
			})
		}
	})
}
