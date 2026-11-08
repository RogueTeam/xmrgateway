package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Config struct {
	Schema string `json:"$schema,omitempty"`

	Listen         string   `json:"listen"`
	RequestTimeout Duration `json:"request_timeout"`
	LogLevel       string   `json:"log_level"`
	StaticDir      string   `json:"static_dir"`
	PublicURL      string   `json:"public_url"`
	OnionURL       string   `json:"onion_url,omitempty"`
	// ClearnetHosts get an Onion-Location header pointing at OnionURL.
	ClearnetHosts   []string `json:"clearnet_hosts,omitempty"`
	DonationAddress string   `json:"donation_address,omitempty"`

	Secret            string   `json:"secret"`
	TokenValidity     Duration `json:"token_validity"`
	ChallengeLifetime Duration `json:"challenge_lifetime"`
	ChallengeMinWait  Duration `json:"challenge_min_wait"`
	PowDifficulty     int      `json:"pow_difficulty"`
	MaxChallenges     int      `json:"max_challenges"`
	RateLimit         Duration `json:"rate_limit"`

	PriceRefresh    Duration `json:"price_refresh"`
	PriceMaxAge     Duration `json:"price_max_age"`
	MaxDeviationBps uint64   `json:"max_deviation_bps"`
	FetchTimeout    Duration `json:"fetch_timeout"`
	UpstreamProxy   string   `json:"upstream_proxy,omitempty"`
}

const MaxChallenges = 1_000_000

func setDefault[T comparable](field *T, value T) {
	var zero T
	if *field == zero {
		*field = value
	}
}

// Default fills every unset field. Load calls it right after unmarshalling.
func (c *Config) Default() {
	setDefault(&c.Listen, "127.0.0.1:8080")
	setDefault(&c.RequestTimeout, Duration{30 * time.Second})
	setDefault(&c.LogLevel, "info")
	setDefault(&c.StaticDir, "static")
	setDefault(&c.PublicURL, "https://xmrgateway.com")

	setDefault(&c.TokenValidity, Duration{90 * 24 * time.Hour})
	setDefault(&c.ChallengeLifetime, Duration{time.Hour})
	setDefault(&c.ChallengeMinWait, Duration{30 * time.Second})
	setDefault(&c.PowDifficulty, 24)
	setDefault(&c.MaxChallenges, MaxChallenges)
	setDefault(&c.RateLimit, Duration{30 * time.Second})

	setDefault(&c.PriceRefresh, Duration{10 * time.Minute})
	setDefault(&c.PriceMaxAge, Duration{30 * time.Minute})
	setDefault(&c.MaxDeviationBps, 500)
	setDefault(&c.FetchTimeout, Duration{30 * time.Second})
}

// Load reads a JSON config and fills in the defaults. Unknown fields are an
// error. Validate is left to the caller.
func Load(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	cfg.Default()
	return cfg, nil
}
