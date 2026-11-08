package httpmux

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/RogueTeam/xmrgateway/api"
	"github.com/RogueTeam/xmrgateway/controller"
)

// price godoc
//
//	@Summary     Price of 1 XMR
//	@Description One request per token per rate limit window. next_request_at says when the token may ask again.
//	@Tags        price
//	@Produce     json
//	@Security    APIToken
//	@Param       currency query    string false "ISO 4217 code, or BTC" default(USD)
//	@Success     200      {object} api.Price
//	@Failure     401      {object} api.Error "invalid or expired token"
//	@Failure     404      {object} api.Error "unknown currency"
//	@Failure     429      {object} api.Error "rate limited, or no token"
//	@Header      429      {integer} Retry-After "seconds until the next allowed request"
//	@Failure     503      {object} api.Error "no recent price"
//	@Router      /price [get]
func (m *Mux) price(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	logger = logger.WithGroup("price")
	currency := r.URL.Query().Get("currency")
	if currency == "" {
		currency = "USD"
	}
	q, err := m.ctrl.Price(ctx, logger, currency, m.now())
	switch {
	case errors.Is(err, controller.ErrUnknownCurrency):
		writeError(w, http.StatusNotFound, err.Error(), nil)
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, err.Error(), nil)
	default:
		writeJSON(w, http.StatusOK, api.Price{
			Currency:      q.Currency,
			Price:         q.Price,
			UpdatedAt:     q.UpdatedAt.UTC(),
			NextRequestAt: nextRequest(ctx).UTC(),
		})
	}
}
