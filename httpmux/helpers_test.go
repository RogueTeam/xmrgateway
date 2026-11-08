package httpmux

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/RogueTeam/xmrgateway/controller"
	"github.com/RogueTeam/xmrgateway/hashcash"
	"github.com/RogueTeam/xmrgateway/internal/config"
	"github.com/RogueTeam/xmrgateway/ratelimit"
	"github.com/RogueTeam/xmrgateway/sources"
)

var (
	t0     = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	logger = slog.New(slog.DiscardHandler)
	onion  = "http://" + strings.Repeat("a", 56) + ".onion"
)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set moves the clock to t0+d.
func (c *clock) Set(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t0.Add(d)
}

type fixed []sources.Rate

func (fixed) Name() string                                    { return "fixed" }
func (f fixed) Fetch(context.Context) ([]sources.Rate, error) { return f, nil }

type env struct {
	cfg    config.Config
	ctrl   *controller.Controller
	clock  *clock
	server *http.Server
}

func testConfig() config.Config {
	var cfg config.Config
	cfg.Default()
	cfg.Secret = strings.Repeat("s", 32)
	cfg.PowDifficulty = 8
	cfg.OnionURL = onion
	cfg.ClearnetHosts = []string{"xmrgateway.com"}
	cfg.StaticDir = "/nonexistent"
	return cfg
}

// controllerFor quotes USD 312.45, EUR 280.00 and BTC 0.00305000 as seen at.
func controllerFor(cfg config.Config, at time.Time) *controller.Controller {
	return controller.New(controller.Options{
		Secret:            []byte(cfg.Secret),
		TokenValidity:     cfg.TokenValidity.Duration,
		ChallengeLifetime: cfg.ChallengeLifetime.Duration,
		ChallengeMinWait:  cfg.ChallengeMinWait.Duration,
		Difficulty:        cfg.PowDifficulty,
		MaxChallenges:     cfg.MaxChallenges,
		PriceRefresh:      cfg.PriceRefresh.Duration,
		PriceMaxAge:       cfg.PriceMaxAge.Duration,
		FetchTimeout:      time.Second,
	}, []sources.Source{fixed{
		{Base: "XMR", Quote: "USD", Price: 31245, Time: at, Source: "fixed"},
		{Base: "XMR", Quote: "EUR", Price: 28000, Time: at, Source: "fixed"},
		{Base: "XMR", Quote: "BTC", Price: 305000, Time: at, Source: "fixed"},
	}})
}

// newEnv builds the server on a fake clock at t0 with prices already cached.
func newEnv(t *testing.T, mod func(*config.Config)) *env {
	t.Helper()
	cfg := testConfig()
	if mod != nil {
		mod(&cfg)
	}
	ctrl := controllerFor(cfg, t0)
	if _, err := ctrl.RefreshPrices(t.Context(), logger, t0); err != nil {
		t.Fatal(err)
	}
	clk := &clock{now: t0}
	server, err := New(t.Context(), logger, ctrl, ratelimit.New[uuid.UUID](cfg.RateLimit.Duration), Options{Config: cfg, Version: "test", Now: clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	return &env{cfg: cfg, ctrl: ctrl, clock: clk, server: server}
}

type reply struct {
	Status int
	Header http.Header
	Body   string
}

func (e *env) do(t *testing.T, ctx context.Context, method, target string, header http.Header, body string) reply {
	t.Helper()
	req := httptest.NewRequestWithContext(ctx, method, target, strings.NewReader(body))
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if h := header.Get("Host"); h != "" {
		req.Host = h
	}
	rec := httptest.NewRecorder()
	e.server.Handler.ServeHTTP(rec, req)
	return reply{rec.Code, rec.Header(), rec.Body.String()}
}

func (e *env) get(t *testing.T, target string) reply {
	t.Helper()
	return e.do(t, t.Context(), http.MethodGet, target, nil, "")
}

// register adds a challenge at t0 directly through the controller.
func (e *env) register(t *testing.T) controller.Challenge {
	t.Helper()
	ch, err := e.ctrl.RegisterChallenge(t.Context(), logger, t0)
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

// token goes through the whole challenge flow on the controller, leaving
// the mux clock alone. It is issued at t0.
func (e *env) token(t *testing.T) string {
	t.Helper()
	ch, err := e.ctrl.RegisterChallenge(t.Context(), logger, t0.Add(-e.cfg.ChallengeMinWait.Duration))
	if err != nil {
		t.Fatal(err)
	}
	tok, err := e.ctrl.ResolveChallenge(t.Context(), logger, solve(t, ch.ID.String(), 8), t0)
	if err != nil {
		t.Fatal(err)
	}
	return tok.Value
}

func solve(t *testing.T, payload string, bits int) string {
	t.Helper()
	d := hashcash.Details{LeadingZeroes: bits, Payload: payload, Time: t0}
	hc, err := d.Hashcash(t.Context(), sha256.Sum224)
	if err != nil {
		t.Fatal(err)
	}
	return hc
}

func canceled(t *testing.T) context.Context {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	return ctx
}

func decode[T any](t *testing.T, body string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("%v in %q", err, body)
	}
	return v
}

// text is a page body with HTML entities decoded, for matching commands.
func text(body string) string { return html.UnescapeString(body) }
