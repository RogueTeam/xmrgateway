package httpmux

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/RogueTeam/xmrgateway/internal/config"
)

func TestRoutes(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Method, Path string
			}
			Expect struct {
				ContentType string
				Contains    []string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name:  "home",
				Input: Input{"GET", "/"},
				Expect: Expect{"text/html; charset=utf-8", []string{
					"Generate token", `action="/token"`, "Monero prices for everyone", "clearnet jump box", "without any guarantee",
					`href="https://github.com/RogueTeam/xmrgateway"`, "once every 30 seconds", "valid for 90 days", "5%",
					"<kbd>BTC</kbd> <kbd>EUR</kbd> <kbd>USD</kbd>", onion,
				}},
			},
			{Name: "home head", Input: Input{"HEAD", "/"}, Expect: Expect{"text/html", nil}},
			{Name: "docs", Input: Input{"GET", "/docs"}, Expect: Expect{"text/html", []string{"/api/v1/price", "/api/v1/tokens", "/api/v1/challenges", "(8 bits)", "X-API-Token"}}},
			{Name: "swagger json", Input: Input{"GET", "/docs/swagger.json"}, Expect: Expect{"application/json", []string{`"swagger": "2.0"`, `"/price"`}}},
			{Name: "swagger yaml", Input: Input{"GET", "/docs/swagger.yaml"}, Expect: Expect{"application/yaml", []string{"swagger: \"2.0\"", "/price:"}}},
			{Name: "stylesheet", Input: Input{"GET", "/static/pico.orange.min.css"}, Expect: Expect{"text/css", []string{"Pico CSS"}}},
			{Name: "logo", Input: Input{"GET", "/static/logo.svg"}, Expect: Expect{"image/svg+xml", []string{"<svg"}}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				r := newEnv(t, nil).do(t, t.Context(), test.Input.Method, test.Input.Path, nil, "")
				if r.Status != http.StatusOK {
					t.Fatalf("status %d", r.Status)
				}
				if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, test.Expect.ContentType) {
					t.Errorf("Content-Type %q, want %q", ct, test.Expect.ContentType)
				}
				for _, want := range test.Expect.Contains {
					if !strings.Contains(r.Body, want) {
						t.Errorf("body is missing %q", want)
					}
				}
				if strings.Contains(r.Body, "<script") {
					t.Error("page has a script")
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Method, Path string
			}
			Expect struct {
				Status      int
				ContentType string
				Contains    []string
				Location    string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		notFound := Expect{Status: 404, ContentType: "text/plain"}
		notAllowed := Expect{Status: 405, ContentType: "text/plain"}
		tests := []Test{
			{Name: "unknown page", Input: Input{"GET", "/nope"}, Expect: notFound},
			{Name: "token page with a bad id", Input: Input{"GET", "/token/nope"}, Expect: Expect{Status: 404, ContentType: "text/html", Contains: []string{"Challenge not found", "never existed"}}},
			{Name: "token page with an unknown id", Input: Input{"GET", "/token/" + uuid.NewV4().String()}, Expect: Expect{Status: 404, ContentType: "text/html", Contains: []string{"Challenge not found"}}},
			{Name: "unknown spec file", Input: Input{"GET", "/docs/other"}, Expect: notFound},
			{Name: "spec with the wrong extension", Input: Input{"GET", "/docs/swagger.yml"}, Expect: notFound},
			{Name: "static listing", Input: Input{"GET", "/static/"}, Expect: notFound},
			{Name: "missing static file", Input: Input{"GET", "/static/missing.css"}, Expect: notFound},
			{Name: "static subdirectory", Input: Input{"GET", "/static/sub/"}, Expect: notFound},
			{Name: "api root", Input: Input{"GET", "/api/v1/"}, Expect: notFound},
			{Name: "path traversal", Input: Input{"GET", "/static/../go.mod"}, Expect: Expect{Status: http.StatusTemporaryRedirect, Location: "/go.mod"}},
			{Name: "post home", Input: Input{"POST", "/"}, Expect: notAllowed},
			{Name: "get the token form", Input: Input{"GET", "/token"}, Expect: notAllowed},
			{Name: "post the price", Input: Input{"POST", "/api/v1/price"}, Expect: notAllowed},
			{Name: "delete a challenge", Input: Input{"DELETE", "/api/v1/challenges/" + uuid.NewV4().String()}, Expect: notAllowed},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				r := newEnv(t, nil).do(t, t.Context(), test.Input.Method, test.Input.Path, nil, "")
				if r.Status != test.Expect.Status {
					t.Fatalf("status %d, want %d", r.Status, test.Expect.Status)
				}
				if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, test.Expect.ContentType) {
					t.Errorf("Content-Type %q, want %q", ct, test.Expect.ContentType)
				}
				for _, want := range test.Expect.Contains {
					if !strings.Contains(r.Body, want) {
						t.Errorf("body is missing %q", want)
					}
				}
				if loc := r.Header.Get("Location"); loc != test.Expect.Location {
					t.Errorf("Location %q", loc)
				}
			})
		}
	})
}

func TestTokenForm(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Mod func(*config.Config) }
			Expect struct{ Contains, Absent []string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name: "clearnet and tor",
				Expect: Expect{Contains: []string{
					"<code>{id}</code>", "8 bits", "2026-11-07 12:00:30 UTC", "2026-11-07 13:00:00 UTC",
					goRun + " \\\n  pow {id} 8",
					"curl -X POST \\\n  https://xmrgateway.com/api/v1/tokens \\\n  -H 'Content-Type: application/json' \\\n  -d '{\"stamp\": \"STAMP\"}'",
					"http_proxy=socks5h://127.0.0.1:9050 \\\ncurl -X POST \\\n  " + onion + "/api/v1/tokens",
					`-d "{\"stamp\": \"$(` + goRun + " \\\n    pow -until 2026-11-07T12:00:30Z {id} 8)\\\"}\"",
					"curl -H 'X-API-Token: TOKEN' \\\n  'https://xmrgateway.com/api/v1/price?currency=USD'",
					"http_proxy=socks5h://127.0.0.1:9050 \\\ncurl -H 'X-API-Token: TOKEN' \\\n  '" + onion + "/api/v1/price?currency=USD'",
					"<strong>Clearnet</strong>", "<strong>Tor</strong>", "valid for 90 days", "once every 30 seconds",
				}},
			},
			{
				Name:   "clearnet only",
				Input:  Input{func(c *config.Config) { c.OnionURL, c.ClearnetHosts = "", nil }},
				Expect: Expect{Contains: []string{"<strong>Clearnet</strong>"}, Absent: []string{"http_proxy", "<strong>Tor</strong>", ".onion"}},
			},
			{
				Name:   "https onion",
				Input:  Input{func(c *config.Config) { c.OnionURL = strings.Replace(onion, "http://", "https://", 1) }},
				Expect: Expect{Contains: []string{"https_proxy=socks5h://127.0.0.1:9050 \\\ncurl"}, Absent: []string{"\nhttp_proxy="}},
			},
			{
				Name:   "trailing slashes are trimmed",
				Input:  Input{func(c *config.Config) { c.PublicURL, c.OnionURL = "https://example.org/", onion+"/" }},
				Expect: Expect{Contains: []string{"  https://example.org/api/v1/tokens", "  " + onion + "/api/v1/tokens"}, Absent: []string{"//api"}},
			},
			{
				Name:   "values from the config",
				Input:  Input{func(c *config.Config) { c.PowDifficulty, c.RateLimit = 20, config.Duration{Duration: time.Minute} }},
				Expect: Expect{Contains: []string{"20 bits", "pow {id} 20", "once every minute"}},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				e := newEnv(t, test.Input.Mod)
				r := e.do(t, t.Context(), http.MethodPost, "/token", nil, "")
				loc := r.Header.Get("Location")
				if r.Status != http.StatusSeeOther || !regexp.MustCompile(`^/token/[0-9a-f-]{36}$`).MatchString(loc) {
					t.Fatalf("POST /token: %d %q", r.Status, loc)
				}
				id := strings.TrimPrefix(loc, "/token/")
				page := e.get(t, loc)
				if page.Status != http.StatusOK {
					t.Fatalf("token page: %d", page.Status)
				}
				body := text(page.Body)
				for _, want := range test.Expect.Contains {
					if want = strings.ReplaceAll(want, "{id}", id); !strings.Contains(body, want) {
						t.Errorf("page is missing %q", want)
					}
				}
				for _, absent := range test.Expect.Absent {
					if strings.Contains(body, absent) {
						t.Errorf("page has %q", absent)
					}
				}
				if reload := e.get(t, loc); reload.Body != page.Body || e.ctrl.PendingChallenges() != 1 {
					t.Error("reloading the page changed it or made a new challenge")
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Existing int
				Canceled bool
				Expired  bool
			}
			Expect struct {
				Status   int
				Contains []string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "too many pending", Input: Input{Existing: 2}, Expect: Expect{503, []string{"Too many pending challenges", "Try again in a minute.", "Generate a new one"}}},
			{Name: "canceled request", Input: Input{Canceled: true}, Expect: Expect{503, []string{"Could not create a challenge"}}},
			{Name: "page of an expired challenge", Input: Input{Expired: true}, Expect: Expect{404, []string{"Challenge not found", "It expired"}}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				e := newEnv(t, func(c *config.Config) { c.MaxChallenges = 2 })
				for range test.Input.Existing {
					e.register(t)
				}
				var r reply
				if test.Input.Expired {
					ch := e.register(t)
					e.clock.Set(time.Hour)
					r = e.get(t, "/token/"+ch.ID.String())
				} else {
					ctx := t.Context()
					if test.Input.Canceled {
						ctx = canceled(t)
					}
					r = e.do(t, ctx, http.MethodPost, "/token", nil, "")
				}
				if r.Status != test.Expect.Status || r.Header.Get("Location") != "" {
					t.Fatalf("status %d, location %q", r.Status, r.Header.Get("Location"))
				}
				for _, want := range test.Expect.Contains {
					if !strings.Contains(r.Body, want) {
						t.Errorf("page is missing %q", want)
					}
				}
			})
		}
	})
}

func TestPagesFollowConfig(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Mod func(*config.Config) }
			Expect struct{ Home, Docs, Absent []string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name: "defaults",
				Expect: Expect{
					Home: []string{"8 leading zero bits", "at least 30 seconds", "valid for 90 days", "once every 30 seconds", "every 10 minutes", "5%", "Retry-After: 20"},
					Docs: []string{"(8 bits)", "opens 30 seconds after", "closes after 1 hour", "valid for 90 days", "at most 30 seconds", "2026-11-07T12:00:30Z", "2027-02-05T12:00:30Z", "1:8:"},
				},
			},
			{
				Name: "custom",
				Input: Input{func(c *config.Config) {
					c.PowDifficulty = 20
					c.ChallengeMinWait = config.Duration{Duration: 10 * time.Second}
					c.ChallengeLifetime = config.Duration{Duration: 2 * time.Hour}
					c.TokenValidity = config.Duration{Duration: 7 * 24 * time.Hour}
					c.RateLimit = config.Duration{Duration: 45 * time.Second}
					c.PriceRefresh = config.Duration{Duration: 4 * time.Minute}
					c.MaxDeviationBps = 250
				}},
				Expect: Expect{
					Home: []string{
						"20 leading zero bits", "at least 10 seconds", "valid for 7 days", "once every 45 seconds",
						"every 4 minutes", "2.50%", "Retry-After: 30",
						`&#34;next_request_at&#34;: &#34;2026-11-07T12:00:55Z&#34;`,
						`&#34;updated_at&#34;: &#34;2026-11-07T11:58:10Z&#34;`,
						`&#34;retry_at&#34;: &#34;2026-11-07T12:00:55Z&#34;`,
					},
					Docs:   []string{"(20 bits)", "opens 10 seconds after", "closes after 2 hours", "valid for 7 days", "at most 45 seconds", "2026-11-07T12:00:10Z", "2026-11-07T14:00:00Z", "2026-11-14T12:00:10Z", "1:20:"},
					Absent: []string{"12:01:31", "Retry-After: 42", "2026-11-08", "2027-02-05", "12:05:00", "90 days"},
				},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				e := newEnv(t, test.Input.Mod)
				home, docs := e.get(t, "/").Body, e.get(t, "/docs").Body
				for _, want := range test.Expect.Home {
					if !strings.Contains(home, want) {
						t.Errorf("home is missing %q", want)
					}
				}
				for _, want := range test.Expect.Docs {
					if !strings.Contains(docs, want) {
						t.Errorf("docs are missing %q", want)
					}
				}
				for _, stale := range test.Expect.Absent {
					if strings.Contains(home+docs, stale) {
						t.Errorf("hardcoded %q left on a page", stale)
					}
				}
			})
		}
	})
}

func TestStaticDir(t *testing.T) {
	deployed := func(t *testing.T) string {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "pico.orange.min.css"), []byte("/* deployed */"), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	fetch := func(t *testing.T, staticDir, path string) reply {
		return newEnv(t, func(c *config.Config) { c.StaticDir = staticDir }).get(t, path)
	}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Setup func(t *testing.T) string
				Path  string
			}
			Expect struct{ Contains string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "deployed copy wins", Input: Input{deployed, "/static/pico.orange.min.css"}, Expect: Expect{"/* deployed */"}},
			{
				Name:   "missing directory falls back to embedded",
				Input:  Input{func(t *testing.T) string { return filepath.Join(t.TempDir(), "absent") }, "/static/pico.orange.min.css"},
				Expect: Expect{"Pico CSS"},
			},
			{
				Name: "a file instead of a directory falls back to embedded",
				Input: Input{func(t *testing.T) string {
					f := filepath.Join(t.TempDir(), "file")
					os.WriteFile(f, nil, 0o644)
					return f
				}, "/static/logo.svg"},
				Expect: Expect{"<svg"},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				r := fetch(t, test.Input.Setup(t), test.Input.Path)
				if r.Status != http.StatusOK || !strings.Contains(r.Body, test.Expect.Contains) {
					t.Fatalf("%d %.80q", r.Status, r.Body)
				}
				if cc := r.Header.Get("Cache-Control"); cc != "public, max-age=86400" {
					t.Errorf("Cache-Control %q", cc)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Setup func(t *testing.T) string
				Path  string
			}
			Expect struct{ Status int }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "deployed directory does not mix with embedded", Input: Input{deployed, "/static/logo.svg"}, Expect: Expect{http.StatusNotFound}},
			{Name: "deployed directory listing", Input: Input{deployed, "/static/"}, Expect: Expect{http.StatusNotFound}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if r := fetch(t, test.Input.Setup(t), test.Input.Path); r.Status != test.Expect.Status {
					t.Fatalf("status %d", r.Status)
				}
			})
		}
	})
}
