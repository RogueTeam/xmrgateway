package httpmux

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/RogueTeam/xmrgateway/api"
)

type nextRequestKey struct{}

// rateLimited lets a request through only with a valid token that has not
// been used within the rate limit. The next allowed time travels in the
// request context.
func (m *Mux) rateLimited(next handlerFunc) handlerFunc {
	return func(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
		logger = logger.WithGroup("rateLimited")
		now := m.now()
		token := r.Header.Get(api.TokenHeader)
		if token == "" {
			writeError(w, http.StatusTooManyRequests, "missing "+api.TokenHeader+" header, get a token at /token", nil)
			return
		}
		id, _, err := m.ctrl.VerifyToken(ctx, logger, token, now)
		if err != nil {
			writeError(w, http.StatusUnauthorized, err.Error(), nil)
			return
		}
		nextAt, ok := m.limiter.Allow(id, now)
		if !ok {
			logger.Debug("limited", "token", id, "retry_at", nextAt)
			setRetryAfter(w, now, nextAt)
			writeError(w, http.StatusTooManyRequests, "rate limited", &nextAt)
			return
		}
		ctx = context.WithValue(ctx, nextRequestKey{}, nextAt)
		next(ctx, logger, w, r.WithContext(ctx))
	}
}

func nextRequest(ctx context.Context) time.Time {
	t, _ := ctx.Value(nextRequestKey{}).(time.Time)
	return t
}
