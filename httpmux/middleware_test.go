package httpmux

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/RogueTeam/xmrgateway/client"
	"github.com/RogueTeam/xmrgateway/internal/config"
	"github.com/RogueTeam/xmrgateway/ratelimit"
)

func TestOnionLocation(t *testing.T) {
	header := func(t *testing.T, mod func(*config.Config), host, target string) string {
		e := newEnv(t, mod)
		return e.do(t, t.Context(), http.MethodGet, target, http.Header{"Host": {host}}, "").Header.Get("Onion-Location")
	}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Mod          func(*config.Config)
				Host, Target string
			}
			Expect struct{ Header string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "listed host", Input: Input{Host: "xmrgateway.com", Target: "/docs?x=1"}, Expect: Expect{onion + "/docs?x=1"}},
			{Name: "upper case with port", Input: Input{Host: "XMRGateway.COM:443", Target: "/"}, Expect: Expect{onion + "/"}},
			{Name: "also on api errors", Input: Input{Host: "xmrgateway.com", Target: "/api/v1/price?currency=eur"}, Expect: Expect{onion + "/api/v1/price?currency=eur"}},
			{
				Name:   "onion url with trailing slash",
				Input:  Input{Mod: func(c *config.Config) { c.OnionURL = onion + "/" }, Host: "xmrgateway.com", Target: "/docs"},
				Expect: Expect{onion + "/docs"},
			},
			{
				Name:   "second listed host",
				Input:  Input{Mod: func(c *config.Config) { c.ClearnetHosts = []string{"xmrgateway.com", "www.xmrgateway.com"} }, Host: "www.xmrgateway.com", Target: "/"},
				Expect: Expect{onion + "/"},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := header(t, test.Input.Mod, test.Input.Host, test.Input.Target); got != test.Expect.Header {
					t.Fatalf("Onion-Location %q, want %q", got, test.Expect.Header)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Mod          func(*config.Config)
				Host, Target string
			}
			Expect struct{}
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "unlisted host", Input: Input{Host: "example.com", Target: "/"}},
			{Name: "lookalike host", Input: Input{Host: "evilxmrgateway.com", Target: "/"}},
			{Name: "subdomain not listed", Input: Input{Host: "www.xmrgateway.com", Target: "/"}},
			{Name: "the onion itself", Input: Input{Host: strings.TrimPrefix(onion, "http://"), Target: "/"}},
			{Name: "ip literal", Input: Input{Host: "[::1]:8080", Target: "/"}},
			{Name: "no onion configured", Input: Input{Mod: func(c *config.Config) { c.OnionURL = "" }, Host: "xmrgateway.com", Target: "/"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := header(t, test.Input.Mod, test.Input.Host, test.Input.Target); got != "" {
					t.Fatalf("Onion-Location %q", got)
				}
			})
		}
	})
}

func TestSecurityHeaders(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Method, Path string }
			Expect struct{ Headers map[string]string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		want := map[string]string{"Content-Security-Policy": csp, "X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer"}
		tests := []Test{
			{Name: "home", Input: Input{"GET", "/"}, Expect: Expect{want}},
			{Name: "api error", Input: Input{"GET", "/api/v1/price"}, Expect: Expect{want}},
			{Name: "api success", Input: Input{"POST", "/api/v1/challenges"}, Expect: Expect{want}},
			{Name: "static", Input: Input{"GET", "/static/logo.svg"}, Expect: Expect{want}},
			{Name: "spec", Input: Input{"GET", "/docs/swagger.yaml"}, Expect: Expect{want}},
			{Name: "not found", Input: Input{"GET", "/nope"}, Expect: Expect{want}},
			{Name: "method not allowed", Input: Input{"PATCH", "/"}, Expect: Expect{want}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				r := newEnv(t, nil).do(t, t.Context(), test.Input.Method, test.Input.Path, nil, "")
				for k, v := range test.Expect.Headers {
					if got := r.Header.Get(k); got != v {
						t.Errorf("%s = %q, want %q", k, got, v)
					}
				}
			})
		}
	})
}

func TestHandle(t *testing.T) {
	// serve runs one request through m.handle and returns what the handler saw.
	serve := func(t *testing.T, timeout time.Duration, parent context.Context) (left time.Duration, err error) {
		var cfg config.Config
		cfg.RequestTimeout = config.Duration{Duration: timeout}
		m := &Mux{cfg: cfg, logger: logger}
		called := false
		h := m.handle(func(ctx context.Context, l *slog.Logger, w http.ResponseWriter, r *http.Request) {
			called = true
			if r.Context() != ctx || l == nil {
				t.Error("request does not carry the handler context")
			}
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Error("no deadline")
			}
			left, err = time.Until(deadline), ctx.Err()
		})
		h(httptest.NewRecorder(), httptest.NewRequestWithContext(parent, http.MethodGet, "/", nil))
		if !called {
			t.Fatal("handler not called")
		}
		return left, err
	}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Timeout time.Duration }
			Expect struct{}
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "default timeout", Input: Input{30 * time.Second}},
			{Name: "short timeout", Input: Input{2 * time.Second}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				left, err := serve(t, test.Input.Timeout, t.Context())
				if err != nil || left > test.Input.Timeout || left < test.Input.Timeout-time.Second {
					t.Fatalf("deadline in %v (%v), want about %v", left, err, test.Input.Timeout)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ Timeout time.Duration }
			Expect struct{ Err error }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "canceled parent", Input: Input{30 * time.Second}, Expect: Expect{context.Canceled}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if _, err := serve(t, test.Input.Timeout, canceled(t)); !errors.Is(err, test.Expect.Err) {
					t.Fatalf("ctx.Err() = %v, want %v", err, test.Expect.Err)
				}
			})
		}
	})
}

func TestNew(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Mod func(*config.Config) }
			Expect struct {
				Addr           string
				Timeout, Idle  time.Duration
				MaxHeaderBytes int
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "defaults", Expect: Expect{"127.0.0.1:8080", 30 * time.Second, 2 * time.Minute, 16 << 10}},
			{
				Name: "from the config",
				Input: Input{func(c *config.Config) {
					c.Listen, c.RequestTimeout = "[::]:9000", config.Duration{Duration: 5 * time.Second}
				}},
				Expect: Expect{"[::]:9000", 5 * time.Second, 20 * time.Second, 16 << 10},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				s := newEnv(t, test.Input.Mod).server
				if s.Addr != test.Expect.Addr || s.ReadTimeout != test.Expect.Timeout || s.ReadHeaderTimeout != test.Expect.Timeout ||
					s.WriteTimeout != test.Expect.Timeout || s.IdleTimeout != test.Expect.Idle || s.MaxHeaderBytes != test.Expect.MaxHeaderBytes {
					t.Fatalf("server %q read %v header %v write %v idle %v max %d", s.Addr, s.ReadTimeout, s.ReadHeaderTimeout, s.WriteTimeout, s.IdleTimeout, s.MaxHeaderBytes)
				}
				if s.ErrorLog == nil || s.BaseContext == nil || s.Handler == nil {
					t.Fatal("server is missing its error log, base context or handler")
				}
			})
		}
	})
}

func TestBaseContext(t *testing.T) {
	// post asks a real server for a challenge, canceling its base context first
	// when cancel is set.
	post := func(t *testing.T, cancel bool) (status, pending int) {
		cfg := testConfig()
		ctrl := controllerFor(cfg, t0)
		base, stop := context.WithCancel(t.Context())
		defer stop()
		server, err := New(base, logger, ctrl, ratelimit.New[uuid.UUID](time.Second), Options{Config: cfg})
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewUnstartedServer(server.Handler)
		srv.Config.BaseContext = server.BaseContext
		srv.Start()
		defer srv.Close()
		if cancel {
			stop()
		}
		resp, err := http.Post(srv.URL+"/api/v1/challenges", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode, ctrl.PendingChallenges()
	}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{}
			Expect struct{ Status, Pending int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "live base context", Expect: Expect{http.StatusCreated, 1}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if status, pending := post(t, false); status != test.Expect.Status || pending != test.Expect.Pending {
					t.Fatalf("status %d, pending %d", status, pending)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{}
			Expect struct{ Status, Pending int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "canceled base context reaches handlers", Expect: Expect{http.StatusServiceUnavailable, 0}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if status, pending := post(t, true); status != test.Expect.Status || pending != test.Expect.Pending {
					t.Fatalf("status %d, pending %d", status, pending)
				}
			})
		}
	})
}

// TestEndToEnd runs the real client against a real server and clock.
func TestEndToEnd(t *testing.T) {
	// gateway serves fresh prices on the real clock with a 200ms minimum wait.
	gateway := func(t *testing.T) *client.Client {
		cfg := testConfig()
		cfg.ChallengeMinWait = config.Duration{Duration: 200 * time.Millisecond}
		now := time.Now()
		ctrl := controllerFor(cfg, now)
		if _, err := ctrl.RefreshPrices(t.Context(), logger, now); err != nil {
			t.Fatal(err)
		}
		server, err := New(t.Context(), logger, ctrl, ratelimit.New[uuid.UUID](cfg.RateLimit.Duration), Options{Config: cfg})
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(server.Handler)
		t.Cleanup(srv.Close)
		return &client.Client{BaseURL: srv.URL}
	}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Currency string }
			Expect struct{ Price string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "token then usd", Input: Input{"usd"}, Expect: Expect{"312.45"}},
			{Name: "token then eur", Input: Input{"EUR"}, Expect: Expect{"280.00"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := gateway(t)
				tok, err := c.Token(t.Context(), "")
				if err != nil {
					t.Fatal(err)
				}
				p, err := c.Price(t.Context(), tok.Token, test.Input.Currency)
				if err != nil || p.Price != test.Expect.Price {
					t.Fatalf("got %+v, %v", p, err)
				}
				if wait := time.Until(p.NextRequestAt); wait < 29*time.Second || wait > 30*time.Second {
					t.Fatalf("next_request_at is %v away, want 30s", wait)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ Requests int }
			Expect struct{ Status int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "second request in the window", Input: Input{2}, Expect: Expect{http.StatusTooManyRequests}},
			{Name: "third request in the window", Input: Input{3}, Expect: Expect{http.StatusTooManyRequests}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				c := gateway(t)
				tok, err := c.Token(t.Context(), "")
				if err != nil {
					t.Fatal(err)
				}
				for range test.Input.Requests {
					_, err = c.Price(t.Context(), tok.Token, "usd")
				}
				var apiErr *client.APIError
				if !errors.As(err, &apiErr) || apiErr.Status != test.Expect.Status || apiErr.Body.RetryAt == nil {
					t.Fatalf("got %v, want status %d with retry_at", err, test.Expect.Status)
				}
			})
		}
	})
}
