// Command formelay is a lightweight, Dockerized backend that accepts form
// submissions from allowlisted websites and relays them to notification
// channels (email, Discord, generic webhooks, ...) via per-form templates.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/bossm8/formelay/internal/api"
	"github.com/bossm8/formelay/internal/app"
	"github.com/bossm8/formelay/internal/audit"
	"github.com/bossm8/formelay/internal/captcha"
	"github.com/bossm8/formelay/internal/config"
	"github.com/bossm8/formelay/internal/metrics"
	"github.com/bossm8/formelay/internal/notify"
	"github.com/bossm8/formelay/internal/notify/discord"
	"github.com/bossm8/formelay/internal/notify/email"
	"github.com/bossm8/formelay/internal/notify/webhook"
	"github.com/bossm8/formelay/internal/ratelimit"
	"github.com/bossm8/formelay/internal/ratelimit/memory"
	"github.com/bossm8/formelay/internal/ratelimit/valkey"
	"github.com/bossm8/formelay/internal/reload"
	"github.com/bossm8/formelay/internal/spamfilter"
	"github.com/bossm8/formelay/internal/spamfilter/ai"
	"github.com/bossm8/formelay/internal/version"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "keygen":
			runKeygen()
			return
		case "healthcheck":
			runHealthcheck(os.Args[2:])
			return
		}
	}
	runServe(os.Args[1:])
}

func runKeygen() {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		fmt.Fprintln(os.Stderr, "keygen:", err)
		os.Exit(1)
	}
	fmt.Println(base64.RawURLEncoding.EncodeToString(buf))
}

func runHealthcheck(args []string) {
	fs := flag.NewFlagSet("healthcheck", flag.ExitOnError)
	addr := fs.String("addr", "http://127.0.0.1:9090/healthz", "healthz URL to check")
	_ = fs.Parse(args)
	resp, err := http.Get(*addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		os.Exit(1)
	}
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "/etc/formelay/config.yaml", "path to config.yaml")
	_ = fs.Parse(args)

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	registries := app.Registries{
		Notify:     notify.NewRegistry(),
		Captcha:    captcha.NewRegistry(),
		SpamFilter: spamfilter.NewRegistry(),
	}
	registries.Notify.Register(email.Type, email.New)
	registries.Notify.Register(discord.Type, discord.New)
	registries.Notify.Register(webhook.Type, webhook.New)
	registries.Captcha.Register("turnstile", captcha.NewFactory("turnstile"))
	registries.Captcha.Register("hcaptcha", captcha.NewFactory("hcaptcha"))
	registries.Captcha.Register("recaptcha_v2", captcha.NewFactory("recaptcha_v2"))
	registries.Captcha.Register("recaptcha_v3", captcha.NewFactory("recaptcha_v3"))
	registries.Captcha.Register("generic", captcha.NewFactory(""))
	registries.SpamFilter.Register(ai.Type, ai.New)

	a := app.New(*configPath, registries)
	if err := a.Reload(); err != nil {
		log.Error("initial config load failed", "error", err)
		os.Exit(1)
	}
	rt := a.Current()
	global := rt.Config.Global

	m := metrics.New(version.Version, version.Commit, runtime.Version())
	auditLogger := audit.New(log)

	rl, closeRL, err := buildRateLimiter(global.RateLimit, m)
	if err != nil {
		log.Error("failed to build rate limiter", "error", err)
		os.Exit(1)
	}
	if closeRL != nil {
		defer closeRL()
	}

	trustedProxies := api.ParseTrustedProxies(global.Server.TrustedProxies)

	server := &api.Server{
		App:            a,
		RateLimiter:    rl,
		Audit:          auditLogger,
		Metrics:        m,
		IDGen:          func() string { return ulid.Make().String() },
		TrustedProxies: trustedProxies,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if global.Reload.WatchFiles {
		if err := reload.WatchFiles(ctx, log, *configPath, global.FormsDir, 200*time.Millisecond, a.Reload); err != nil {
			log.Error("failed to start config watcher", "error", err)
		}
	}
	if global.Reload.HandleSIGHUP {
		reload.HandleSIGHUP(ctx, log, a.Reload)
	}

	mux := server.NewMux()
	httpServer := &http.Server{
		Addr:              global.Server.ListenAddr,
		Handler:           mux,
		ReadTimeout:       global.Server.ReadTimeout.Std(),
		WriteTimeout:      global.Server.WriteTimeout.Std(),
		IdleTimeout:       global.Server.IdleTimeout.Std(),
		ReadHeaderTimeout: nonZero(global.Server.ReadHeaderTimeout.Std(), 5*time.Second),
	}

	// Liveness/readiness always live on the internal listener; /metrics is
	// added to the same mux when enabled.
	internalMux := server.NewInternalMux(global.Health.LivenessPath, global.Health.ReadinessPath)
	if global.Metrics.Enabled {
		internalMux.Handle("GET "+global.Metrics.Path, promHandler(m))
	}
	internalServer := &http.Server{Addr: global.Metrics.ListenAddr, Handler: internalMux}
	go func() {
		log.Info("internal (health/metrics) listening", "addr", global.Metrics.ListenAddr)
		if err := internalServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("internal server error", "error", err)
		}
	}()

	go func() {
		log.Info("formelay listening", "addr", global.Server.ListenAddr, "version", version.Version)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), nonZero(global.Server.ShutdownGracePeriod.Std(), 15*time.Second))
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	_ = internalServer.Shutdown(shutdownCtx)
}

func nonZero(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}

func buildRateLimiter(cfg config.RateLimitConfig, m *metrics.Metrics) (ratelimit.Store, func(), error) {
	switch cfg.Backend {
	case "valkey":
		st, err := valkey.New(valkey.Config{
			Addresses:   cfg.Valkey.Addresses,
			Password:    os.Getenv(cfg.Valkey.PasswordEnv),
			DB:          cfg.Valkey.DB,
			DialTimeout: cfg.Valkey.DialTimeout.Std(),
			KeyPrefix:   cfg.Valkey.KeyPrefix,
			OnError:     valkey.OnError(cfg.Valkey.OnError),
		})
		if err != nil {
			return nil, nil, err
		}
		return st, st.Close, nil
	default:
		idleTTL := nonZero(cfg.BucketIdleTTL.Std(), 10*time.Minute)
		cleanup := nonZero(cfg.CleanupInterval.Std(), 5*time.Minute)
		st := memory.New(idleTTL)
		ctx, cancel := context.WithCancel(context.Background())
		st.StartJanitor(ctx, cleanup)
		return st, cancel, nil
	}
}

func promHandler(m *metrics.Metrics) http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}
