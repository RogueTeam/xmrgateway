package config

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

var (
	onionHost = regexp.MustCompile(`^[a-z2-7]{56}\.onion$`)
	xmrAddr   = regexp.MustCompile(`^[48][1-9A-HJ-NP-Za-km-z]{94}([1-9A-HJ-NP-Za-km-z]{11})?$`)
)

// Validate reports every problem in the config at once.
func (c Config) Validate() error {
	var errs []error
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}

	check(c.Listen != "", "listen is required")
	check(c.RequestTimeout.Duration >= time.Second && c.RequestTimeout.Duration <= 5*time.Minute,
		"request_timeout must be between 1s and 5m")
	check(slices.Contains([]string{"debug", "info", "warn", "error"}, c.LogLevel),
		"log_level must be debug, info, warn or error")
	check(c.StaticDir != "", "static_dir is required")
	check(isWebURL(c.PublicURL), "public_url must be an absolute http(s) URL")
	if c.OnionURL != "" {
		u, err := url.Parse(c.OnionURL)
		check(err == nil && (u.Scheme == "http" || u.Scheme == "https") && onionHost.MatchString(u.Host),
			"onion_url must be http(s)://<56 characters>.onion")
	}
	for _, h := range c.ClearnetHosts {
		check(h != "" && !strings.ContainsAny(h, "/: "), "clearnet_hosts: %q must be a bare host name", h)
	}
	check(len(c.ClearnetHosts) == 0 || c.OnionURL != "", "clearnet_hosts needs onion_url")
	check(c.DonationAddress == "" || xmrAddr.MatchString(c.DonationAddress), "donation_address is not a Monero address")

	check(len(c.Secret) >= 32, "secret must be at least 32 characters")
	check(c.TokenValidity.Duration >= c.RateLimit.Duration, "token_validity must be at least rate_limit")
	check(c.ChallengeLifetime.Duration > 0, "challenge_lifetime must be positive")
	check(c.ChallengeMinWait.Duration >= 0, "challenge_min_wait cannot be negative")
	check(c.ChallengeMinWait.Duration < c.ChallengeLifetime.Duration, "challenge_min_wait must be shorter than challenge_lifetime")
	check(c.PowDifficulty >= 8 && c.PowDifficulty <= 40, "pow_difficulty must be between 8 and 40")
	check(c.MaxChallenges >= 1 && c.MaxChallenges <= MaxChallenges, "max_challenges must be between 1 and %d", MaxChallenges)
	check(c.RateLimit.Duration >= time.Second, "rate_limit must be at least 1s")

	check(c.PriceRefresh.Duration >= 10*time.Second, "price_refresh must be at least 10s")
	check(c.PriceMaxAge.Duration >= c.PriceRefresh.Duration, "price_max_age must be at least price_refresh")
	check(c.MaxDeviationBps >= 1 && c.MaxDeviationBps <= 5000, "max_deviation_bps must be between 1 and 5000")
	check(c.FetchTimeout.Duration > 0 && c.FetchTimeout.Duration <= c.PriceRefresh.Duration,
		"fetch_timeout must be positive and at most price_refresh")
	if c.UpstreamProxy != "" {
		u, err := url.Parse(c.UpstreamProxy)
		check(err == nil && u.Host != "" && slices.Contains([]string{"http", "https", "socks5", "socks5h"}, u.Scheme),
			"upstream_proxy must be an http, https, socks5 or socks5h URL")
	}
	return errors.Join(errs...)
}

func isWebURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
