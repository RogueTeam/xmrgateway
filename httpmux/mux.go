// Package httpmux serves the API, the HTML pages and the static assets.
package httpmux

//go:generate go run github.com/swaggo/swag/cmd/swag@latest init -d . -g mux.go -o docs --outputTypes json,yaml --parseDependency --parseInternal

import (
	"context"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"time"
	"uuid"

	"github.com/RogueTeam/xmrgateway/controller"
	"github.com/RogueTeam/xmrgateway/internal/config"
	"github.com/RogueTeam/xmrgateway/ratelimit"
)

// @title                      xmrgateway
// @version                    0.1.2
// @description                Monero price API for everyone, on the clearnet and on an onion address for hidden services.
// @BasePath                   /api/v1
// @securityDefinitions.apikey APIToken
// @in                         header
// @name                       X-API-Token

type Options struct {
	Config  config.Config
	Version string
	Now     func() time.Time
}

type Mux struct {
	ctrl    *controller.Controller
	limiter *ratelimit.Limiter[uuid.UUID]
	cfg     config.Config
	version string
	logger  *slog.Logger
	now     func() time.Time
	pages   map[string]*template.Template
	docs    docsPage
	samples examples
}

// handlerFunc is implemented by every route. ctx carries the request
// deadline and logger is already scoped to the request.
type handlerFunc func(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, r *http.Request)

// New builds the HTTP server from already configured parts. ctx is the base
// context of every request, so cancelling it reaches all handlers.
func New(ctx context.Context, logger *slog.Logger, ctrl *controller.Controller, limiter *ratelimit.Limiter[uuid.UUID], opt Options) (*http.Server, error) {
	logger = logger.WithGroup("http")
	m := &Mux{ctrl: ctrl, limiter: limiter, cfg: opt.Config, version: opt.Version, logger: logger, now: opt.Now}
	if m.now == nil {
		m.now = time.Now
	}
	m.samples = newExamples(m.cfg)
	var err error
	if m.pages, err = loadPages(); err != nil {
		return nil, err
	}
	if m.docs, err = loadDocs(m.samples.fields); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/challenges", m.handle(m.createChallenge))
	mux.HandleFunc("GET /api/v1/challenges/{id}", m.handle(m.getChallenge))
	mux.HandleFunc("POST /api/v1/tokens", m.handle(m.redeem))
	mux.HandleFunc("GET /api/v1/price", m.handle(m.rateLimited(m.price)))

	mux.HandleFunc("GET /{$}", m.handle(m.home))
	mux.HandleFunc("POST /token", m.handle(m.newToken))
	mux.HandleFunc("GET /token/{id}", m.handle(m.tokenPage))
	mux.HandleFunc("GET /docs", m.handle(m.docsPage))
	mux.HandleFunc("GET /docs/{file}", m.spec)
	mux.Handle("GET /static/", staticFiles(m.cfg.StaticDir))

	timeout := m.cfg.RequestTimeout.Duration
	return &http.Server{
		Addr:              m.cfg.Listen,
		Handler:           securityHeaders(m.onionLocation(mux)),
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: timeout,
		ReadTimeout:       timeout,
		WriteTimeout:      timeout,
		IdleTimeout:       4 * timeout,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}, nil
}

// handle gives every request its own deadline and logger.
func (m *Mux) handle(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), m.cfg.RequestTimeout.Duration)
		defer cancel()
		logger := m.logger.With("method", r.Method, "path", r.URL.Path)
		h(ctx, logger, w, r.WithContext(ctx))
	}
}
