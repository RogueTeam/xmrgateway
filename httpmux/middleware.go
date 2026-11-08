package httpmux

import (
	"net"
	"net/http"
	"slices"
	"strings"
)

const csp = "default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// onionLocation advertises the onion service to Tor Browser users visiting
// one of the clearnet host names.
func (m *Mux) onionLocation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.cfg.OnionURL != "" && slices.Contains(m.cfg.ClearnetHosts, hostname(r.Host)) {
			w.Header().Set("Onion-Location", strings.TrimRight(m.cfg.OnionURL, "/")+r.URL.RequestURI())
		}
		next.ServeHTTP(w, r)
	})
}

func hostname(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.ToLower(host)
	}
	return strings.ToLower(hostport)
}
