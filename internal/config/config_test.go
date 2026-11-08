package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

const secret = "0123456789abcdef0123456789abcdef"

var (
	onion    = "http://" + strings.Repeat("a", 56) + ".onion"
	donation = "4" + strings.Repeat("A", 94)
)

// defaults returns a defaulted config after applying mod.
func defaults(mod func(*Config)) Config {
	var c Config
	c.Default()
	if mod != nil {
		mod(&c)
	}
	return c
}

func valid(mod func(*Config)) Config {
	return defaults(func(c *Config) {
		c.Secret = secret
		if mod != nil {
			mod(c)
		}
	})
}

func writeFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func d(v time.Duration) Duration { return Duration{v} }

func TestDefault(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Config Config }
			Expect struct{ Config Config }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		set := Config{
			Listen: "0.0.0.0:80", RequestTimeout: d(time.Minute), LogLevel: "debug", StaticDir: "/var/www",
			PublicURL: "https://example.com", TokenValidity: d(time.Hour), ChallengeLifetime: d(2 * time.Hour),
			ChallengeMinWait: d(time.Second), PowDifficulty: 30, MaxChallenges: 10, RateLimit: d(time.Minute),
			PriceRefresh: d(time.Minute), PriceMaxAge: d(time.Hour), MaxDeviationBps: 100, FetchTimeout: d(5 * time.Second),
		}
		tests := []Test{
			{
				Name:  "empty",
				Input: Input{Config{}},
				Expect: Expect{Config{
					Listen: "127.0.0.1:8080", RequestTimeout: d(30 * time.Second), LogLevel: "info", StaticDir: "static",
					PublicURL: "https://xmrgateway.com", TokenValidity: d(2160 * time.Hour), ChallengeLifetime: d(time.Hour),
					ChallengeMinWait: d(30 * time.Second), PowDifficulty: 24, MaxChallenges: 1_000_000, RateLimit: d(30 * time.Second),
					PriceRefresh: d(10 * time.Minute), PriceMaxAge: d(30 * time.Minute), MaxDeviationBps: 500, FetchTimeout: d(30 * time.Second),
				}},
			},
			{Name: "set fields are kept", Input: Input{set}, Expect: Expect{set}},
			{
				Name:  "optional fields stay as given",
				Input: Input{Config{Secret: "s", OnionURL: onion, ClearnetHosts: []string{"a"}, UpstreamProxy: "socks5h://127.0.0.1:9050"}},
				Expect: Expect{defaults(func(c *Config) {
					c.Secret, c.OnionURL, c.ClearnetHosts, c.UpstreamProxy = "s", onion, []string{"a"}, "socks5h://127.0.0.1:9050"
				})},
			},
			{
				Name:   "negative values are left for Validate",
				Input:  Input{Config{ChallengeMinWait: d(-time.Second), PowDifficulty: -1}},
				Expect: Expect{defaults(func(c *Config) { c.ChallengeMinWait, c.PowDifficulty = d(-time.Second), -1 })},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				got := test.Input.Config
				got.Default()
				if !reflect.DeepEqual(got, test.Expect.Config) {
					t.Fatalf("got  %+v\nwant %+v", got, test.Expect.Config)
				}
				again := got
				again.Default()
				if !reflect.DeepEqual(again, got) {
					t.Fatal("Default is not idempotent")
				}
			})
		}
	})
}

func TestLoad(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ JSON string }
			Expect struct{ Config Config }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "empty object", Input: Input{`{}`}, Expect: Expect{defaults(nil)}},
			{Name: "only the secret", Input: Input{`{"secret":"` + secret + `"}`}, Expect: Expect{valid(nil)}},
			{
				Name:  "values override defaults",
				Input: Input{`{"rate_limit":"45s","pow_difficulty":20,"token_validity":"720h","log_level":"debug"}`},
				Expect: Expect{defaults(func(c *Config) {
					c.RateLimit, c.PowDifficulty, c.TokenValidity, c.LogLevel = d(45*time.Second), 20, d(720*time.Hour), "debug"
				})},
			},
			{
				Name:   "zero durations count as unset",
				Input:  Input{`{"rate_limit":"0s","price_refresh":"0m"}`},
				Expect: Expect{defaults(nil)},
			},
			{
				Name:   "fractional durations",
				Input:  Input{`{"challenge_lifetime":"1.5h","request_timeout":"2500ms"}`},
				Expect: Expect{defaults(func(c *Config) { c.ChallengeLifetime, c.RequestTimeout = d(90*time.Minute), d(2500*time.Millisecond) })},
			},
			{
				Name:  "schema key and lists",
				Input: Input{`{"$schema":"./config.schema.json","onion_url":"` + onion + `","clearnet_hosts":["a.com","b.com"]}`},
				Expect: Expect{defaults(func(c *Config) {
					c.Schema, c.OnionURL, c.ClearnetHosts = "./config.schema.json", onion, []string{"a.com", "b.com"}
				})},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				got, err := Load(writeFile(t, test.Input.JSON))
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, test.Expect.Config) {
					t.Fatalf("got  %+v\nwant %+v", got, test.Expect.Config)
				}
			})
		}
	})

	t.Run("Succeed with the shipped example", func(t *testing.T) {
		type (
			Input  struct{ Path string }
			Expect struct{ Valid bool }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "config.example.json", Input: Input{"../../config.example.json"}, Expect: Expect{true}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				cfg, err := Load(test.Input.Path)
				if err != nil {
					t.Fatal(err)
				}
				if err := cfg.Validate(); (err == nil) != test.Expect.Valid {
					t.Fatal(err)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				JSON    string
				Missing bool
			}
			Expect struct{ ErrContains string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "missing file", Input: Input{Missing: true}, Expect: Expect{"no such file"}},
			{Name: "unknown field", Input: Input{JSON: `{"secret":"x","sekret":"typo"}`}, Expect: Expect{`unknown field "sekret"`}},
			{Name: "duration without a unit", Input: Input{JSON: `{"rate_limit":"30"}`}, Expect: Expect{"missing unit"}},
			{Name: "duration in words", Input: Input{JSON: `{"rate_limit":"30 seconds"}`}, Expect: Expect{"unknown unit"}},
			{Name: "duration as a number", Input: Input{JSON: `{"rate_limit":30}`}, Expect: Expect{"cannot unmarshal number"}},
			{Name: "null duration", Input: Input{JSON: `{"rate_limit":null}`}, Expect: Expect{"invalid duration"}},
			{Name: "number as a string", Input: Input{JSON: `{"pow_difficulty":"24"}`}, Expect: Expect{"cannot unmarshal string"}},
			{Name: "negative unsigned", Input: Input{JSON: `{"max_deviation_bps":-1}`}, Expect: Expect{"cannot unmarshal number -1"}},
			{Name: "hosts as a string", Input: Input{JSON: `{"clearnet_hosts":"a.com"}`}, Expect: Expect{"cannot unmarshal string"}},
			{Name: "not json", Input: Input{JSON: `secret = "abc"`}, Expect: Expect{"invalid character"}},
			{Name: "empty file", Input: Input{JSON: ``}, Expect: Expect{"EOF"}},
			{Name: "array", Input: Input{JSON: `[]`}, Expect: Expect{"cannot unmarshal array"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "absent.json")
				if !test.Input.Missing {
					path = writeFile(t, test.Input.JSON)
				}
				_, err := Load(path)
				if err == nil || !strings.Contains(err.Error(), test.Expect.ErrContains) {
					t.Fatalf("got %v, want %q", err, test.Expect.ErrContains)
				}
				if !strings.Contains(err.Error(), filepath.Base(path)) {
					t.Errorf("error does not name the file: %v", err)
				}
			})
		}
	})
}

func TestValidate(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Mod func(*Config) }
			Expect struct{}
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "defaults with a secret", Input: Input{nil}},
			{Name: "onion with clearnet hosts", Input: Input{func(c *Config) { c.OnionURL, c.ClearnetHosts = onion, []string{"xmrgateway.com", "www.xmrgateway.com"} }}},
			{Name: "https onion with trailing slash", Input: Input{func(c *Config) { c.OnionURL = "https://" + strings.Repeat("z", 56) + ".onion/" }}},
			{Name: "primary address", Input: Input{func(c *Config) { c.DonationAddress = donation }}},
			{Name: "subaddress", Input: Input{func(c *Config) { c.DonationAddress = "8" + strings.Repeat("z", 94) }}},
			{Name: "integrated address", Input: Input{func(c *Config) { c.DonationAddress = donation + strings.Repeat("1", 11) }}},
			{Name: "socks5h proxy", Input: Input{func(c *Config) { c.UpstreamProxy = "socks5h://127.0.0.1:9050" }}},
			{Name: "socks5 proxy", Input: Input{func(c *Config) { c.UpstreamProxy = "socks5://127.0.0.1:9050" }}},
			{Name: "http proxy", Input: Input{func(c *Config) { c.UpstreamProxy = "http://proxy:3128" }}},
			{Name: "https proxy", Input: Input{func(c *Config) { c.UpstreamProxy = "https://proxy" }}},
			{Name: "http public url", Input: Input{func(c *Config) { c.PublicURL = "http://192.168.1.10:8080" }}},
			{Name: "warn log level", Input: Input{func(c *Config) { c.LogLevel = "warn" }}},
			{Name: "lowest limits", Input: Input{func(c *Config) {
				c.RequestTimeout, c.PowDifficulty, c.MaxChallenges, c.RateLimit = d(time.Second), 8, 1, d(time.Second)
				c.TokenValidity, c.ChallengeMinWait, c.ChallengeLifetime = d(time.Second), d(0), d(time.Nanosecond)
				c.PriceRefresh, c.PriceMaxAge, c.FetchTimeout, c.MaxDeviationBps = d(10*time.Second), d(10*time.Second), d(time.Nanosecond), 1
			}}},
			{Name: "highest limits", Input: Input{func(c *Config) {
				c.RequestTimeout, c.PowDifficulty, c.MaxChallenges, c.MaxDeviationBps = d(5*time.Minute), 40, MaxChallenges, 5000
				c.FetchTimeout = c.PriceRefresh
			}}},
			{Name: "long secret", Input: Input{func(c *Config) { c.Secret = strings.Repeat("s", 256) }}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if err := valid(test.Input.Mod).Validate(); err != nil {
					t.Fatal(err)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ Mod func(*Config) }
			Expect struct{ Errors []string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "no listen", Input: Input{func(c *Config) { c.Listen = "" }}, Expect: Expect{[]string{"listen is required"}}},
			{Name: "request timeout too short", Input: Input{func(c *Config) { c.RequestTimeout = d(999 * time.Millisecond) }}, Expect: Expect{[]string{"request_timeout"}}},
			{Name: "request timeout too long", Input: Input{func(c *Config) { c.RequestTimeout = d(5*time.Minute + 1) }}, Expect: Expect{[]string{"request_timeout"}}},
			{Name: "unknown log level", Input: Input{func(c *Config) { c.LogLevel = "loud" }}, Expect: Expect{[]string{"log_level"}}},
			{Name: "log level is case sensitive", Input: Input{func(c *Config) { c.LogLevel = "INFO" }}, Expect: Expect{[]string{"log_level"}}},
			{Name: "no static dir", Input: Input{func(c *Config) { c.StaticDir = "" }}, Expect: Expect{[]string{"static_dir"}}},
			{Name: "public url without scheme", Input: Input{func(c *Config) { c.PublicURL = "xmrgateway.com" }}, Expect: Expect{[]string{"public_url"}}},
			{Name: "public url with another scheme", Input: Input{func(c *Config) { c.PublicURL = "ftp://xmrgateway.com" }}, Expect: Expect{[]string{"public_url"}}},
			{Name: "public url without host", Input: Input{func(c *Config) { c.PublicURL = "https://" }}, Expect: Expect{[]string{"public_url"}}},
			{Name: "onion url on the clearnet", Input: Input{func(c *Config) { c.OnionURL = "http://example.com" }}, Expect: Expect{[]string{"onion_url"}}},
			{Name: "short onion", Input: Input{func(c *Config) { c.OnionURL = "http://abc.onion" }}, Expect: Expect{[]string{"onion_url"}}},
			{Name: "uppercase onion", Input: Input{func(c *Config) { c.OnionURL = strings.ToUpper(onion) }}, Expect: Expect{[]string{"onion_url"}}},
			{Name: "onion with socks scheme", Input: Input{func(c *Config) { c.OnionURL = strings.Replace(onion, "http", "socks5h", 1) }}, Expect: Expect{[]string{"onion_url"}}},
			{Name: "clearnet host with scheme", Input: Input{func(c *Config) { c.OnionURL, c.ClearnetHosts = onion, []string{"https://a.com"} }}, Expect: Expect{[]string{`"https://a.com" must be a bare host`}}},
			{Name: "clearnet host with port", Input: Input{func(c *Config) { c.OnionURL, c.ClearnetHosts = onion, []string{"a.com:443"} }}, Expect: Expect{[]string{"bare host"}}},
			{Name: "empty clearnet host", Input: Input{func(c *Config) { c.OnionURL, c.ClearnetHosts = onion, []string{""} }}, Expect: Expect{[]string{"bare host"}}},
			{Name: "clearnet hosts without onion", Input: Input{func(c *Config) { c.ClearnetHosts = []string{"a.com"} }}, Expect: Expect{[]string{"needs onion_url"}}},
			{Name: "donation not an address", Input: Input{func(c *Config) { c.DonationAddress = "send me xmr" }}, Expect: Expect{[]string{"donation_address"}}},
			{Name: "donation with bitcoin prefix", Input: Input{func(c *Config) { c.DonationAddress = "1" + donation[1:] }}, Expect: Expect{[]string{"donation_address"}}},
			{Name: "donation with a zero", Input: Input{func(c *Config) { c.DonationAddress = donation[:94] + "0" }}, Expect: Expect{[]string{"donation_address"}}},
			{Name: "donation one short", Input: Input{func(c *Config) { c.DonationAddress = donation[:94] }}, Expect: Expect{[]string{"donation_address"}}},
			{Name: "secret one short", Input: Input{func(c *Config) { c.Secret = secret[:31] }}, Expect: Expect{[]string{"secret"}}},
			{Name: "no secret", Input: Input{func(c *Config) { c.Secret = "" }}, Expect: Expect{[]string{"secret"}}},
			{Name: "token shorter than rate limit", Input: Input{func(c *Config) { c.TokenValidity = d(10 * time.Second) }}, Expect: Expect{[]string{"token_validity"}}},
			{
				Name:   "no challenge lifetime",
				Input:  Input{func(c *Config) { c.ChallengeLifetime = d(0) }},
				Expect: Expect{[]string{"challenge_lifetime must be positive", "challenge_min_wait must be shorter"}},
			},
			{Name: "negative min wait", Input: Input{func(c *Config) { c.ChallengeMinWait = d(-time.Second) }}, Expect: Expect{[]string{"cannot be negative"}}},
			{Name: "min wait equals lifetime", Input: Input{func(c *Config) { c.ChallengeMinWait = c.ChallengeLifetime }}, Expect: Expect{[]string{"challenge_min_wait must be shorter"}}},
			{Name: "pow too easy", Input: Input{func(c *Config) { c.PowDifficulty = 7 }}, Expect: Expect{[]string{"pow_difficulty"}}},
			{Name: "pow too hard", Input: Input{func(c *Config) { c.PowDifficulty = 41 }}, Expect: Expect{[]string{"pow_difficulty"}}},
			{Name: "no challenges", Input: Input{func(c *Config) { c.MaxChallenges = 0 }}, Expect: Expect{[]string{"max_challenges"}}},
			{Name: "too many challenges", Input: Input{func(c *Config) { c.MaxChallenges = MaxChallenges + 1 }}, Expect: Expect{[]string{"max_challenges must be between 1 and 1000000"}}},
			{Name: "rate limit under a second", Input: Input{func(c *Config) { c.RateLimit = d(999 * time.Millisecond) }}, Expect: Expect{[]string{"rate_limit"}}},
			{
				Name:   "price refresh too fast",
				Input:  Input{func(c *Config) { c.PriceRefresh = d(9 * time.Second) }},
				Expect: Expect{[]string{"price_refresh", "fetch_timeout"}},
			},
			{Name: "max age below refresh", Input: Input{func(c *Config) { c.PriceMaxAge = d(5 * time.Minute) }}, Expect: Expect{[]string{"price_max_age"}}},
			{Name: "zero deviation", Input: Input{func(c *Config) { c.MaxDeviationBps = 0 }}, Expect: Expect{[]string{"max_deviation_bps"}}},
			{Name: "deviation over half", Input: Input{func(c *Config) { c.MaxDeviationBps = 5001 }}, Expect: Expect{[]string{"max_deviation_bps"}}},
			{Name: "no fetch timeout", Input: Input{func(c *Config) { c.FetchTimeout = d(0) }}, Expect: Expect{[]string{"fetch_timeout"}}},
			{Name: "fetch timeout over refresh", Input: Input{func(c *Config) { c.FetchTimeout = d(11 * time.Minute) }}, Expect: Expect{[]string{"fetch_timeout"}}},
			{Name: "ftp proxy", Input: Input{func(c *Config) { c.UpstreamProxy = "ftp://127.0.0.1" }}, Expect: Expect{[]string{"upstream_proxy"}}},
			{Name: "proxy without host", Input: Input{func(c *Config) { c.UpstreamProxy = "socks5h://" }}, Expect: Expect{[]string{"upstream_proxy"}}},
			{Name: "proxy without scheme", Input: Input{func(c *Config) { c.UpstreamProxy = "127.0.0.1:9050" }}, Expect: Expect{[]string{"upstream_proxy"}}},
			{
				Name: "reports everything at once",
				Input: Input{func(c *Config) {
					c.Secret, c.ChallengeMinWait, c.PowDifficulty, c.MaxChallenges = "short", d(2*time.Hour), 99, MaxChallenges+1
					c.OnionURL, c.UpstreamProxy, c.LogLevel = "http://example.com", "ftp://127.0.0.1", "loud"
				}},
				Expect: Expect{[]string{"log_level", "onion_url", "secret", "challenge_min_wait", "pow_difficulty", "max_challenges", "upstream_proxy"}},
			},
			{
				Name:   "empty config",
				Input:  Input{func(c *Config) { *c = Config{} }},
				Expect: Expect{[]string{"listen", "request_timeout", "log_level", "static_dir", "public_url", "secret", "challenge_lifetime", "challenge_min_wait", "pow_difficulty", "max_challenges", "rate_limit", "price_refresh", "max_deviation_bps", "fetch_timeout"}},
			},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				err := valid(test.Input.Mod).Validate()
				if err == nil {
					t.Fatal("accepted")
				}
				var joined interface{ Unwrap() []error }
				if !errors.As(err, &joined) || len(joined.Unwrap()) != len(test.Expect.Errors) {
					t.Fatalf("want %d errors, got:\n%v", len(test.Expect.Errors), err)
				}
				for i, want := range test.Expect.Errors {
					if got := joined.Unwrap()[i].Error(); !strings.Contains(got, want) {
						t.Errorf("error %d is %q, want %q", i, got, want)
					}
				}
			})
		}
	})
}

func TestDuration(t *testing.T) {
	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ JSON string }
			Expect struct {
				Duration time.Duration
				JSON     string
			}
			Test struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "seconds", Input: Input{`"30s"`}, Expect: Expect{30 * time.Second, `"30s"`}},
			{Name: "three months", Input: Input{`"2160h"`}, Expect: Expect{2160 * time.Hour, `"2160h0m0s"`}},
			{Name: "compound", Input: Input{`"1h30m"`}, Expect: Expect{90 * time.Minute, `"1h30m0s"`}},
			{Name: "fraction", Input: Input{`"1.5h"`}, Expect: Expect{90 * time.Minute, `"1h30m0s"`}},
			{Name: "milliseconds", Input: Input{`"250ms"`}, Expect: Expect{250 * time.Millisecond, `"250ms"`}},
			{Name: "zero", Input: Input{`"0"`}, Expect: Expect{0, `"0s"`}},
			{Name: "negative", Input: Input{`"-5m"`}, Expect: Expect{-5 * time.Minute, `"-5m0s"`}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				var got Duration
				if err := json.Unmarshal([]byte(test.Input.JSON), &got); err != nil {
					t.Fatal(err)
				}
				if got.Duration != test.Expect.Duration {
					t.Fatalf("got %v", got.Duration)
				}
				out, err := json.Marshal(got)
				if err != nil || string(out) != test.Expect.JSON {
					t.Fatalf("marshal %s, %v", out, err)
				}
				var back Duration
				if err := json.Unmarshal(out, &back); err != nil || back != got {
					t.Fatalf("round trip %v, %v", back, err)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input  struct{ JSON string }
			Expect struct{ ErrContains string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "number", Input: Input{`30`}, Expect: Expect{"cannot unmarshal number"}},
			{Name: "no unit", Input: Input{`"30"`}, Expect: Expect{"missing unit"}},
			{Name: "words", Input: Input{`"thirty seconds"`}, Expect: Expect{"invalid duration"}},
			{Name: "days are not a unit", Input: Input{`"90d"`}, Expect: Expect{"unknown unit"}},
			{Name: "empty", Input: Input{`""`}, Expect: Expect{"invalid duration"}},
			{Name: "null", Input: Input{`null`}, Expect: Expect{"invalid duration"}},
			{Name: "object", Input: Input{`{"seconds":30}`}, Expect: Expect{"cannot unmarshal object"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				var got Duration
				err := json.Unmarshal([]byte(test.Input.JSON), &got)
				if err == nil || !strings.Contains(err.Error(), test.Expect.ErrContains) {
					t.Fatalf("got %v, want %q", err, test.Expect.ErrContains)
				}
			})
		}
	})
}

type schemaDoc struct {
	Defs       map[string]schemaProp `json:"$defs"`
	Properties map[string]schemaProp `json:"properties"`
	Required   []string              `json:"required"`
}

type schemaProp struct {
	Ref     string          `json:"$ref"`
	Pattern string          `json:"pattern"`
	Default json.RawMessage `json:"default"`
	Items   *schemaProp     `json:"items"`
}

func loadSchema(t *testing.T) schemaDoc {
	t.Helper()
	var s schemaDoc
	if err := json.Unmarshal(JSONSchema, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// pattern resolves the regexp a property's values must match.
func (s schemaDoc) pattern(t *testing.T, key string) *regexp.Regexp {
	t.Helper()
	p := s.Properties[key]
	if p.Items != nil {
		p = *p.Items
	}
	if p.Ref != "" {
		p = s.Defs[strings.TrimPrefix(p.Ref, "#/$defs/")]
	}
	if p.Pattern == "" {
		t.Fatalf("%s has no pattern", key)
	}
	return regexp.MustCompile(p.Pattern)
}

func TestSchemaFields(t *testing.T) {
	schema := loadSchema(t)
	defaults := defaults(nil)

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input  struct{ Field reflect.StructField }
			Expect struct{ Ref string }
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		var tests []Test
		for f := range reflect.TypeFor[Config]().Fields() {
			test := Test{Name: strings.Split(f.Tag.Get("json"), ",")[0], Input: Input{f}}
			if f.Type == reflect.TypeFor[Duration]() {
				test.Expect.Ref = "#/$defs/duration"
			}
			tests = append(tests, test)
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				prop, found := schema.Properties[test.Name]
				if !found {
					t.Fatal("missing from the schema")
				}
				if test.Expect.Ref != "" && prop.Ref != test.Expect.Ref {
					t.Errorf("$ref %q, want %q", prop.Ref, test.Expect.Ref)
				}
				if prop.Default == nil {
					return
				}
				got := reflect.New(test.Input.Field.Type)
				if err := json.Unmarshal(prop.Default, got.Interface()); err != nil {
					t.Fatal(err)
				}
				want := reflect.ValueOf(defaults).FieldByIndex(test.Input.Field.Index)
				if !reflect.DeepEqual(got.Elem().Interface(), want.Interface()) {
					t.Errorf("schema default %s, Default() sets %v", prop.Default, want.Interface())
				}
			})
		}
		if len(schema.Properties) != len(tests) {
			t.Errorf("schema has %d properties, Config has %d fields", len(schema.Properties), len(tests))
		}
		if len(schema.Required) != 1 || schema.Required[0] != "secret" {
			t.Errorf("required %v", schema.Required)
		}
	})
}

func TestSchemaPatterns(t *testing.T) {
	schema := loadSchema(t)

	t.Run("Succeed", func(t *testing.T) {
		type (
			Input struct {
				Key, Value string
			}
			Expect struct{}
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "duration", Input: Input{"rate_limit", "30s"}},
			{Name: "compound duration", Input: Input{"token_validity", "1h30m"}},
			{Name: "fractional duration", Input: Input{"challenge_lifetime", "1.5h"}},
			{Name: "micro sign", Input: Input{"request_timeout", "500µs"}},
			{Name: "public url", Input: Input{"public_url", "https://xmrgateway.com"}},
			{Name: "onion", Input: Input{"onion_url", onion}},
			{Name: "no onion", Input: Input{"onion_url", ""}},
			{Name: "clearnet host", Input: Input{"clearnet_hosts", "www.xmrgateway.com"}},
			{Name: "donation", Input: Input{"donation_address", donation}},
			{Name: "no donation", Input: Input{"donation_address", ""}},
			{Name: "tor proxy", Input: Input{"upstream_proxy", "socks5h://127.0.0.1:9050"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if !schema.pattern(t, test.Input.Key).MatchString(test.Input.Value) {
					t.Fatalf("%s rejects %q", test.Input.Key, test.Input.Value)
				}
			})
		}
	})

	t.Run("Fail", func(t *testing.T) {
		type (
			Input struct {
				Key, Value string
			}
			Expect struct{}
			Test   struct {
				Name   string
				Input  Input
				Expect Expect
			}
		)
		tests := []Test{
			{Name: "duration without unit", Input: Input{"rate_limit", "30"}},
			{Name: "duration in days", Input: Input{"token_validity", "90d"}},
			{Name: "negative duration", Input: Input{"challenge_min_wait", "-1s"}},
			{Name: "public url without scheme", Input: Input{"public_url", "xmrgateway.com"}},
			{Name: "clearnet onion", Input: Input{"onion_url", "http://example.com"}},
			{Name: "host with port", Input: Input{"clearnet_hosts", "xmrgateway.com:443"}},
			{Name: "host with path", Input: Input{"clearnet_hosts", "xmrgateway.com/"}},
			{Name: "bitcoin address", Input: Input{"donation_address", "1" + donation[1:]}},
			{Name: "ftp proxy", Input: Input{"upstream_proxy", "ftp://127.0.0.1"}},
		}
		for _, test := range tests {
			t.Run(test.Name, func(t *testing.T) {
				if schema.pattern(t, test.Input.Key).MatchString(test.Input.Value) {
					t.Fatalf("%s accepts %q", test.Input.Key, test.Input.Value)
				}
			})
		}
	})
}
