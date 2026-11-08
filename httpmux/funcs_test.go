package httpmux

import (
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/RogueTeam/xmrgateway/internal/config"
)

func TestHuman(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ D time.Duration }
			Expect struct{ Human, Every string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "one second", Input: Input{time.Second}, Expect: Expect{"1 second", "every second"}},
			{Name: "seconds", Input: Input{30 * time.Second}, Expect: Expect{"30 seconds", "every 30 seconds"}},
			{Name: "seconds that are not whole minutes", Input: Input{90 * time.Second}, Expect: Expect{"90 seconds", "every 90 seconds"}},
			{Name: "one minute", Input: Input{time.Minute}, Expect: Expect{"1 minute", "every minute"}},
			{Name: "minutes", Input: Input{10 * time.Minute}, Expect: Expect{"10 minutes", "every 10 minutes"}},
			{Name: "one hour", Input: Input{time.Hour}, Expect: Expect{"1 hour", "every hour"}},
			{Name: "hours that are not whole days", Input: Input{25 * time.Hour}, Expect: Expect{"25 hours", "every 25 hours"}},
			{Name: "one day", Input: Input{24 * time.Hour}, Expect: Expect{"1 day", "every day"}},
			{Name: "three months", Input: Input{2160 * time.Hour}, Expect: Expect{"90 days", "every 90 days"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := human(test.Input.D); got != test.Expect.Human {
					t.Errorf("human = %q, want %q", got, test.Expect.Human)
				}
				if got := every(test.Input.D); got != test.Expect.Every {
					t.Errorf("every = %q, want %q", got, test.Expect.Every)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ D time.Duration }
			Expect struct{ Fallback string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		// Not a whole number of any unit: both fall back to Go's own format.
		tests := []Test{
			{Name: "fraction of a second", Input: Input{1500 * time.Millisecond}, Expect: Expect{"1.5s"}},
			{Name: "below a second", Input: Input{250 * time.Millisecond}, Expect: Expect{"250ms"}},
			{Name: "zero", Input: Input{0}, Expect: Expect{"0s"}},
			{Name: "negative", Input: Input{-time.Minute}, Expect: Expect{"-1m0s"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := human(test.Input.D); got != test.Expect.Fallback {
					t.Errorf("human = %q, want %q", got, test.Expect.Fallback)
				}
				if got := every(test.Input.D); got != "every "+test.Expect.Fallback {
					t.Errorf("every = %q", got)
				}
			})
		}
	})
}

func TestTemplateFuncs(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Call func() string }
			Expect struct{ Out string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		bps := funcs["bps"].(func(uint64) string)
		utc := funcs["utc"].(func(time.Time) string)
		tests := []Test{
			{Name: "bps whole percent", Input: Input{func() string { return bps(500) }}, Expect: Expect{"5%"}},
			{Name: "bps fraction", Input: Input{func() string { return bps(250) }}, Expect: Expect{"2.50%"}},
			{Name: "bps smallest", Input: Input{func() string { return bps(1) }}, Expect: Expect{"0.01%"}},
			{Name: "bps largest", Input: Input{func() string { return bps(5000) }}, Expect: Expect{"50%"}},
			{Name: "bps odd", Input: Input{func() string { return bps(1234) }}, Expect: Expect{"12.34%"}},
			{Name: "utc", Input: Input{func() string { return utc(t0) }}, Expect: Expect{"2026-11-07 12:00:00 UTC"}},
			{Name: "utc converts zones", Input: Input{func() string { return utc(t0.In(time.FixedZone("IST", 19800))) }}, Expect: Expect{"2026-11-07 12:00:00 UTC"}},
			{Name: "tor proxy for http", Input: Input{func() string { return torProxy(onion) }}, Expect: Expect{"http_proxy=socks5h://127.0.0.1:9050 \\\n"}},
			{Name: "tor proxy for https", Input: Input{func() string { return torProxy("https://x.onion") }}, Expect: Expect{"https_proxy=socks5h://127.0.0.1:9050 \\\n"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := test.Input.Call(); got != test.Expect.Out {
					t.Fatalf("got %q, want %q", got, test.Expect.Out)
				}
			})
		}
	})
}

func TestHostname(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ HostPort string }
			Expect struct{ Host string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "bare", Input: Input{"xmrgateway.com"}, Expect: Expect{"xmrgateway.com"}},
			{Name: "with port", Input: Input{"xmrgateway.com:443"}, Expect: Expect{"xmrgateway.com"}},
			{Name: "upper case", Input: Input{"XMRGateway.COM"}, Expect: Expect{"xmrgateway.com"}},
			{Name: "ipv6", Input: Input{"[::1]:8080"}, Expect: Expect{"::1"}},
			{Name: "empty", Input: Input{""}, Expect: Expect{""}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := hostname(test.Input.HostPort); got != test.Expect.Host {
					t.Fatalf("got %q", got)
				}
			})
		}
	})
}

func TestSetRetryAfter(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Wait time.Duration }
			Expect struct{ Header string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "whole seconds", Input: Input{30 * time.Second}, Expect: Expect{"30"}},
			{Name: "rounds up", Input: Input{29*time.Second + time.Millisecond}, Expect: Expect{"30"}},
			{Name: "one nanosecond", Input: Input{1}, Expect: Expect{"1"}},
			{Name: "already passed", Input: Input{-time.Minute}, Expect: Expect{"1"}},
			{Name: "now", Input: Input{0}, Expect: Expect{"1"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				w := httptest.NewRecorder()
				setRetryAfter(w, t0, t0.Add(test.Input.Wait))
				if got := w.Header().Get("Retry-After"); got != test.Expect.Header {
					t.Fatalf("got %q", got)
				}
			})
		}
	})
}

func TestExamples(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Mod func(*config.Config) }
			Expect struct {
				RetryAfter int
				Fields     map[string]string
				Price      []string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{
				Name: "defaults",
				Expect: Expect{
					RetryAfter: 20,
					Fields: map[string]string{
						"Challenge.difficulty": "8", "Challenge.not_before": "2026-11-07T12:00:30Z", "Challenge.expires_at": "2026-11-07T13:00:00Z",
						"Token.expires_at": "2027-02-05T12:00:30Z", "Price.updated_at": "2026-11-07T11:55:30Z", "Price.next_request_at": "2026-11-07T12:01:00Z",
						"Error.retry_at": "2026-11-07T12:01:00Z",
					},
					Price: []string{`"price": "312.45"`, `"next_request_at": "2026-11-07T12:01:00Z"`},
				},
			},
			{
				Name: "rate limit not divisible by three",
				Input: Input{func(c *config.Config) {
					c.RateLimit, c.ChallengeMinWait = config.Duration{Duration: 10 * time.Second}, config.Duration{Duration: 0}
				}},
				Expect: Expect{
					RetryAfter: 7,
					Fields:     map[string]string{"Challenge.not_before": "2026-11-07T12:00:00Z", "Error.retry_at": "2026-11-07T12:00:10Z"},
				},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				cfg := testConfig()
				if test.Input.Mod != nil {
					test.Input.Mod(&cfg)
				}
				ex := newExamples(cfg)
				if ex.RetryAfter != test.Expect.RetryAfter {
					t.Errorf("RetryAfter %d, want %d", ex.RetryAfter, test.Expect.RetryAfter)
				}
				for key, want := range test.Expect.Fields {
					model, field, _ := strings.Cut(key, ".")
					if got := ex.fields[model][field]; got != want {
						t.Errorf("%s = %q, want %q", key, got, want)
					}
				}
				for _, want := range test.Expect.Price {
					if !strings.Contains(ex.Price, want) {
						t.Errorf("price example is missing %q:\n%s", want, ex.Price)
					}
				}
				if !strings.HasPrefix(ex.fields["RedeemRequest"]["stamp"], "1:"+ex.fields["Challenge"]["difficulty"]+":") {
					t.Errorf("stamp example %q", ex.fields["RedeemRequest"]["stamp"])
				}
			})
		}
	})
}

func TestLoadDocs(t *testing.T) {
	page, err := loadDocs(newExamples(testConfig()).fields)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Method, Path string }
			Expect struct {
				Secured   bool
				Responses []string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "create challenge", Input: Input{"POST", "/api/v1/challenges"}, Expect: Expect{false, []string{"201", "503"}}},
			{Name: "get challenge", Input: Input{"GET", "/api/v1/challenges/{id}"}, Expect: Expect{false, []string{"200", "404"}}},
			{Name: "redeem", Input: Input{"POST", "/api/v1/tokens"}, Expect: Expect{false, []string{"201", "400", "403", "404", "410", "425"}}},
			{Name: "price", Input: Input{"GET", "/api/v1/price"}, Expect: Expect{true, []string{"200", "401", "404", "429", "503"}}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				i := slices.IndexFunc(page.Endpoints, func(e endpoint) bool { return e.Method == test.Input.Method && e.Path == test.Input.Path })
				if i < 0 {
					t.Fatal("endpoint missing from the docs")
				}
				e := page.Endpoints[i]
				var statuses []string
				for _, r := range e.Responses {
					statuses = append(statuses, r.Status)
				}
				if e.Secured != test.Expect.Secured || !slices.Equal(statuses, test.Expect.Responses) || e.Summary == "" || e.Anchor == "" {
					t.Fatalf("got %+v", e)
				}
			})
		}
		if len(page.Endpoints) != len(tests) {
			t.Errorf("%d endpoints documented, %d tested", len(page.Endpoints), len(tests))
		}
	})

	t.Run("Succeed with models", func(t *testing.T) {
		type (
			Input  struct{ Model, Field string }
			Expect struct{ Example string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "override from config", Input: Input{"Challenge", "not_before"}, Expect: Expect{"2026-11-07T12:00:30Z"}},
			{Name: "difficulty from config", Input: Input{"Challenge", "difficulty"}, Expect: Expect{"8"}},
			{Name: "swag example kept", Input: Input{"Price", "price"}, Expect: Expect{"312.45"}},
			{Name: "currency example", Input: Input{"Price", "currency"}, Expect: Expect{"USD"}},
			{Name: "error example", Input: Input{"Error", "error"}, Expect: Expect{"rate limited"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				i := slices.IndexFunc(page.Models, func(m model) bool { return m.Name == test.Input.Model })
				if i < 0 {
					t.Fatal("model missing")
				}
				j := slices.IndexFunc(page.Models[i].Fields, func(f field) bool { return f.Name == test.Input.Field })
				if j < 0 || page.Models[i].Fields[j].Example != test.Expect.Example {
					t.Fatalf("fields %+v", page.Models[i].Fields)
				}
			})
		}
	})
}

func TestModelName(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Ref *ref }
			Expect struct{ Name string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "api model", Input: Input{&ref{"#/definitions/api.Price"}}, Expect: Expect{"Price"}},
			{Name: "other package", Input: Input{&ref{"#/definitions/other.Thing"}}, Expect: Expect{"other.Thing"}},
			{Name: "no schema", Input: Input{nil}, Expect: Expect{""}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if got := modelName(test.Input.Ref); got != test.Expect.Name {
					t.Fatalf("got %q", got)
				}
			})
		}
	})
}
