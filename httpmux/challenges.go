package httpmux

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"uuid"

	"github.com/RogueTeam/xmrgateway/api"
	"github.com/RogueTeam/xmrgateway/controller"
)

func toAPIChallenge(c controller.Challenge) api.Challenge {
	return api.Challenge{ID: c.ID.String(), Difficulty: c.Difficulty, NotBefore: c.NotBefore.UTC(), ExpiresAt: c.ExpiresAt.UTC()}
}

// createChallenge godoc
//
//	@Summary     Request a challenge
//	@Description Solve it with a hashcash stamp and redeem the stamp at /tokens between not_before and expires_at.
//	@Tags        tokens
//	@Produce     json
//	@Success     201 {object} api.Challenge
//	@Failure     503 {object} api.Error "too many pending challenges"
//	@Router      /challenges [post]
func (m *Mux) createChallenge(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	logger = logger.WithGroup("createChallenge")
	ch, err := m.ctrl.RegisterChallenge(ctx, logger, m.now())
	if errors.Is(err, controller.ErrChallengesFull) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusServiceUnavailable, err.Error(), nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusCreated, toAPIChallenge(ch))
}

// getChallenge godoc
//
//	@Summary Look up a pending challenge
//	@Tags    tokens
//	@Produce json
//	@Param   id  path     string true "challenge id"
//	@Success 200 {object} api.Challenge
//	@Failure 404 {object} api.Error "unknown, used or expired"
//	@Router  /challenges/{id} [get]
func (m *Mux) getChallenge(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	logger = logger.WithGroup("getChallenge")
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, controller.ErrUnknownChallenge.Error(), nil)
		return
	}
	ch, ok := m.ctrl.Challenge(ctx, logger, id, m.now())
	if !ok {
		writeError(w, http.StatusNotFound, controller.ErrUnknownChallenge.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, toAPIChallenge(ch))
}
