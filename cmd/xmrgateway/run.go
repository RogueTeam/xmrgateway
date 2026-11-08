package main

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	"uuid"

	"github.com/RogueTeam/xmrgateway/controller"
	"github.com/RogueTeam/xmrgateway/httpmux"
	"github.com/RogueTeam/xmrgateway/internal/config"
	"github.com/RogueTeam/xmrgateway/internal/sandbox"
	"github.com/RogueTeam/xmrgateway/ratelimit"
	"github.com/RogueTeam/xmrgateway/sources"
)

func serverCmd(args []string) error { return serve("server", args, true) }

// daemonCmd is what the rc.d script runs. syslog stamps every line, so the
// logger leaves the time out.
func daemonCmd(args []string) error { return serve("daemon", args, false) }

func serve(name string, args []string, timestamps bool) error {
	fs, home := newFlags(name)
	configPath := fs.String("config", "config.json", "config file")
	if err := parse(fs, home, args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("%s:\n%w", *configPath, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return run(ctx, newLogger(cfg.LogLevel, timestamps).WithGroup(name), cfg)
}

func newLogger(level string, timestamps bool) *slog.Logger {
	var lvl slog.Level
	lvl.UnmarshalText([]byte(level))
	opts := &slog.HandlerOptions{Level: lvl}
	if !timestamps {
		opts.ReplaceAttr = func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		}
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

// run serves until ctx is cancelled or the listener fails, then stops the
// HTTP server and every worker before returning.
func run(ctx context.Context, logger *slog.Logger, cfg config.Config) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	upstream, err := httpClient(cfg.UpstreamProxy, cfg.FetchTimeout.Duration)
	if err != nil {
		return err
	}
	ctrl := controller.New(controller.Options{
		Secret:            []byte(cfg.Secret),
		TokenValidity:     cfg.TokenValidity.Duration,
		ChallengeLifetime: cfg.ChallengeLifetime.Duration,
		ChallengeMinWait:  cfg.ChallengeMinWait.Duration,
		Difficulty:        cfg.PowDifficulty,
		MaxChallenges:     cfg.MaxChallenges,
		PriceRefresh:      cfg.PriceRefresh.Duration,
		PriceMaxAge:       cfg.PriceMaxAge.Duration,
		FetchTimeout:      cfg.FetchTimeout.Duration,
		Aggregate:         sources.Options{MaxAge: cfg.PriceMaxAge.Duration, MaxDeviationBps: cfg.MaxDeviationBps},
	}, sources.All(upstream))
	limiter := ratelimit.New[uuid.UUID](cfg.RateLimit.Duration)
	server, err := httpmux.New(ctx, logger, ctrl, limiter, httpmux.Options{Config: cfg, Version: currentVersion()})
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	if err := lockdown(cfg.StaticDir); err != nil {
		ln.Close()
		return err
	}

	var workers sync.WaitGroup
	workers.Go(func() { ctrl.PriceWorker(ctx, logger) })
	workers.Go(func() { ctrl.ChallengeJanitor(ctx, logger) })
	workers.Go(func() { limiter.Run(ctx, cfg.RateLimit.Duration) })

	served := make(chan error, 1)
	go func() { served <- server.Serve(ln) }()
	logger.Info("listening", "addr", ln.Addr().String(), "version", currentVersion(), "sandboxed", sandbox.Supported)

	select {
	case err = <-served:
	case <-ctx.Done():
		logger.Info("shutting down")
	}
	cancel()
	shutdown, done := context.WithTimeout(context.Background(), cfg.RequestTimeout.Duration)
	defer done()
	if serr := server.Shutdown(shutdown); serr != nil {
		logger.Warn("shutdown", "error", serr)
	}
	workers.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// lockdown hides everything but the static files and the resolver config,
// then keeps only networking and reading those files.
func lockdown(staticDir string) error {
	// Both are otherwise loaded lazily, after the filesystem is gone.
	if _, err := x509.SystemCertPool(); err != nil {
		return err
	}
	time.Now().Local().Zone()

	for _, path := range []string{staticDir, "/etc/resolv.conf", "/etc/hosts"} {
		if err := sandbox.Unveil(path, "r"); err != nil && !errors.Is(err, syscall.ENOENT) {
			return fmt.Errorf("unveil %s: %w", path, err)
		}
	}
	if err := sandbox.Lock(); err != nil {
		return fmt.Errorf("unveil: %w", err)
	}
	if err := sandbox.Pledge("stdio rpath inet dns"); err != nil {
		return fmt.Errorf("pledge: %w", err)
	}
	return nil
}
