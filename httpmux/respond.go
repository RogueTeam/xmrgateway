package httpmux

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/RogueTeam/xmrgateway/api"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string, retryAt *time.Time) {
	body := api.Error{Message: msg}
	if retryAt != nil {
		t := retryAt.UTC()
		body.RetryAt = &t
	}
	writeJSON(w, status, body)
}

func setRetryAfter(w http.ResponseWriter, now, at time.Time) {
	seconds := (at.Sub(now) + time.Second - 1) / time.Second
	w.Header().Set("Retry-After", strconv.Itoa(max(int(seconds), 1)))
}
