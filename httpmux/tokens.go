package httpmux

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/RogueTeam/xmrgateway/api"
	"github.com/RogueTeam/xmrgateway/controller"
)

// redeem godoc
//
//	@Summary     Redeem a stamp for a token
//	@Description A stamp that fails the proof of work burns its challenge.
//	@Tags        tokens
//	@Accept      json
//	@Produce     json
//	@Param       body body     api.RedeemRequest true "solved stamp"
//	@Success     201  {object} api.Token
//	@Failure     400  {object} api.Error "malformed stamp"
//	@Failure     403  {object} api.Error "not enough work, challenge burned"
//	@Failure     404  {object} api.Error "unknown or already used challenge"
//	@Failure     410  {object} api.Error "challenge expired"
//	@Failure     425  {object} api.Error "too early, see retry_at"
//	@Header      425  {integer} Retry-After "seconds until the stamp can be redeemed"
//	@Router      /tokens [post]
func (m *Mux) redeem(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	logger = logger.WithGroup("redeem")
	var req api.RedeemRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "body must be {\"stamp\": \"...\"}", nil)
		return
	}
	now := m.now()
	token, err := m.ctrl.ResolveChallenge(ctx, logger, req.Stamp, now)
	var early *controller.TooEarlyError
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, api.Token{Token: token.Value, ExpiresAt: token.ExpiresAt})
	case errors.As(err, &early):
		setRetryAfter(w, now, early.RetryAt)
		writeError(w, http.StatusTooEarly, err.Error(), &early.RetryAt)
	case errors.Is(err, controller.ErrInvalidStamp):
		writeError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, controller.ErrInvalidProof):
		writeError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, controller.ErrChallengeExpired):
		writeError(w, http.StatusGone, err.Error(), nil)
	case errors.Is(err, controller.ErrUnknownChallenge):
		writeError(w, http.StatusNotFound, err.Error(), nil)
	case ctx.Err() != nil:
		writeError(w, http.StatusServiceUnavailable, "request timed out", nil)
	default:
		logger.Error("unexpected error", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error", nil)
	}
}
