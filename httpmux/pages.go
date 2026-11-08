package httpmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/RogueTeam/xmrgateway/controller"
	"github.com/RogueTeam/xmrgateway/web"
)

var funcs = template.FuncMap{
	"human": human,
	"every": every,
	"utc":   func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 UTC") },
	"bps":   func(bps uint64) string { return strings.TrimSuffix(fmt.Sprintf("%.2f", float64(bps)/100), ".00") + "%" },
}

func loadPages() (map[string]*template.Template, error) {
	pages := map[string]*template.Template{}
	for _, name := range []string{"home", "token", "docs"} {
		t, err := template.New(name).Funcs(funcs).ParseFS(web.Templates(), "layout.html", name+".html")
		if err != nil {
			return nil, err
		}
		pages[name] = t
	}
	return pages, nil
}

type site struct {
	Version         string
	OnionURL        string
	DonationAddress string
}

func (m *Mux) site() site {
	return site{Version: m.version, OnionURL: m.cfg.OnionURL, DonationAddress: m.cfg.DonationAddress}
}

func (m *Mux) render(logger *slog.Logger, w http.ResponseWriter, status int, name string, data any) {
	logger = logger.WithGroup("render")
	var buf bytes.Buffer
	if err := m.pages[name].ExecuteTemplate(&buf, "layout", data); err != nil {
		logger.Error("template failed", "page", name, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (m *Mux) home(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	logger = logger.WithGroup("home")
	m.render(logger, w, http.StatusOK, "home", map[string]any{
		"Site":            m.site(),
		"Difficulty":      m.cfg.PowDifficulty,
		"MinWait":         m.cfg.ChallengeMinWait.Duration,
		"TokenValidity":   m.cfg.TokenValidity.Duration,
		"RateLimit":       m.cfg.RateLimit.Duration,
		"Refresh":         m.cfg.PriceRefresh.Duration,
		"MaxDeviationBps": m.cfg.MaxDeviationBps,
		"Currencies":      m.ctrl.Currencies(ctx, logger, m.now()),
		"Examples":        m.samples,
	})
}

// newToken registers a challenge from the home page form and redirects to
// its instructions, so reloading the page does not create another one.
func (m *Mux) newToken(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	logger = logger.WithGroup("newToken")
	ch, err := m.ctrl.RegisterChallenge(ctx, logger, m.now())
	if errors.Is(err, controller.ErrChallengesFull) {
		m.render(logger, w, http.StatusServiceUnavailable, "token", map[string]any{"Site": m.site(), "Notice": "Too many pending challenges"})
		return
	}
	if err != nil {
		m.render(logger, w, http.StatusServiceUnavailable, "token", map[string]any{"Site": m.site(), "Notice": "Could not create a challenge"})
		return
	}
	http.Redirect(w, r, "/token/"+ch.ID.String(), http.StatusSeeOther)
}

func (m *Mux) tokenPage(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	logger = logger.WithGroup("tokenPage")
	data := map[string]any{
		"Site":          m.site(),
		"TokenValidity": m.cfg.TokenValidity.Duration,
		"RateLimit":     m.cfg.RateLimit.Duration,
	}
	id, err := uuid.Parse(r.PathValue("id"))
	ch, ok := m.ctrl.Challenge(ctx, logger, id, m.now())
	if err != nil || !ok {
		m.render(logger, w, http.StatusNotFound, "token", data)
		return
	}
	data["Challenge"] = ch
	data["Commands"] = m.tokenCommands(ch)
	m.render(logger, w, http.StatusOK, "token", data)
}

// every phrases an interval: "every minute", "every 5 minutes".
func every(d time.Duration) string {
	h := human(d)
	if rest, ok := strings.CutPrefix(h, "1 "); ok {
		return "every " + rest
	}
	return "every " + h
}

func human(d time.Duration) string {
	for _, unit := range []struct {
		size time.Duration
		name string
	}{{24 * time.Hour, "day"}, {time.Hour, "hour"}, {time.Minute, "minute"}, {time.Second, "second"}} {
		if d >= unit.size && d%unit.size == 0 {
			n := int(d / unit.size)
			if n == 1 {
				return "1 " + unit.name
			}
			return fmt.Sprintf("%d %ss", n, unit.name)
		}
	}
	return d.String()
}
