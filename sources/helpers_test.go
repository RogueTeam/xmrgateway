package sources

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type reply struct {
	Status int
	Body   string
}

func ok(body string) reply { return reply{Status: http.StatusOK, Body: body} }

// serve answers the listed request URIs and fails the test on anything else.
func serve(t *testing.T, routes map[string]reply) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Errorf("request without User-Agent")
		}
		rep, found := routes[r.URL.RequestURI()]
		if !found {
			t.Errorf("unexpected request %s", r.URL.RequestURI())
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rep.Status)
		fmt.Fprint(w, rep.Body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// pairs renders rates as "BASE/QUOTE=price" for easy comparison.
func pairs(rates []Rate) []string {
	out := make([]string, len(rates))
	for i, r := range rates {
		out[i] = fmt.Sprintf("%s/%s=%s", r.Base, r.Quote, FormatDecimal(r.Price, Scale(r.Quote)))
	}
	return out
}
