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
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"sync"
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
	addr := fs.String("addr", "http://127.0.0.1:9696/healthz", "healthz URL to check")
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

	// Built before the initial config load (it has no config dependency),
	// so reloadFn below can record that first load too, not just later ones.
	m := metrics.New(version.Version, version.Commit, runtime.Version())

	a := app.New(*configPath, registries)
	// Wraps every reload (this initial one, and every later one triggered
	// by the file watcher or SIGHUP below) so formelay_config_reload_total
	// and formelay_config_last_reload_timestamp_seconds reflect all of them,
	// not just a subset.
	reloadFn := func() error {
		err := a.Reload()
		status := "success"
		if err != nil {
			status = "failure"
		} else {
			m.ConfigLastReloadTimestamp.Set(float64(time.Now().Unix()))
			if ids := formsWithOriginCheckDisabled(a.Current().Forms); len(ids) > 0 {
				log.Warn("origin/CORS checking is fully disabled (allowed_origins: DANGEROUS_DISABLED) — development only, never leave this on in production", "forms", ids)
			}
		}
		m.ConfigReloadTotal.WithLabelValues(status).Inc()
		return err
	}
	if err := reloadFn(); err != nil {
		log.Error("initial config load failed", "error", err)
		os.Exit(1)
	}
	rt := a.Current()
	global := rt.Config.Global

	// Rebuild the logger per logging.level/logging.format now that config
	// is loaded (the bootstrap logger above exists only so config-load
	// errors themselves have somewhere to go). Not hot-reloaded: a later
	// config change to logging.* takes effect on the next restart, not live.
	log = buildLogger(global.Logging, os.Stdout)
	slog.SetDefault(log)

	// The audit log is always JSON, independent of logging.format, so it
	// gets its own logger rather than reusing the (possibly text-formatted)
	// application logger above.
	auditLog := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	auditLogger := audit.New(auditLog)

	rl, closeRL, err := buildRateLimiter(global.RateLimit, m)
	if err != nil {
		log.Error("failed to build rate limiter", "error", err)
		os.Exit(1)
	}
	if closeRL != nil {
		defer closeRL()
	}

	trustedProxies := api.ParseTrustedProxies(global.Server.TrustedProxies)

	// Backs response_mode: async — deliberately not derived from the
	// shutdown-signal ctx below (that one is cancelled the instant a
	// signal arrives), so an in-flight background dispatch gets the full
	// shutdown grace period to finish rather than being killed instantly.
	// See the shutdown sequence at the bottom of this function.
	bgCtx, cancelBackground := context.WithCancel(context.Background())
	defer cancelBackground()
	var bgWG sync.WaitGroup

	server := &api.Server{
		App:            a,
		RateLimiter:    rl,
		Audit:          auditLogger,
		Metrics:        m,
		IDGen:          func() string { return ulid.Make().String() },
		TrustedProxies: trustedProxies,
		BackgroundCtx:  bgCtx,
		BackgroundWG:   &bgWG,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if global.Reload.WatchFiles {
		if err := reload.WatchFiles(ctx, log, *configPath, global.FormsDir, 200*time.Millisecond, reloadFn); err != nil {
			log.Error("failed to start config watcher", "error", err)
		}
	}
	if global.Reload.HandleSIGHUP {
		reload.HandleSIGHUP(ctx, log, reloadFn)
	}

	// formelay_ratelimit_buckets_active only applies to the memory backend
	// (valkey's bucket state lives server-side, not in this process).
	if memStore, ok := rl.(*memory.Store); ok {
		go sampleActiveBuckets(ctx, memStore, m)
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
		var err error
		if global.Server.TLS.Enabled {
			log.Info("formelay listening (TLS)", "addr", global.Server.ListenAddr, "version", version.Version)
			err = httpServer.ListenAndServeTLS(global.Server.TLS.CertFile, global.Server.TLS.KeyFile)
		} else {
			log.Info("formelay listening", "addr", global.Server.ListenAddr, "version", version.Version)
			err = httpServer.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
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
	waitForBackgroundDispatches(shutdownCtx, log, &bgWG)
	cancelBackground()
}

// waitForBackgroundDispatches blocks until every response_mode: async
// dispatch spawned via api.Server.BackgroundWG has finished, or shutdownCtx
// expires (the same server.shutdown_grace_period budget that already
// governs httpServer.Shutdown above) — whichever comes first. A no-op,
// returning immediately, if no form ever used response_mode: async.
func waitForBackgroundDispatches(shutdownCtx context.Context, log *slog.Logger, wg *sync.WaitGroup) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		log.Warn("shutdown grace period elapsed with response_mode: async dispatches still in flight; exiting anyway")
	}
}

// buildLogger applies logging.level/logging.format to the general
// application logger (not the audit logger, which is always JSON
// regardless, see the audit.New call site above). w is injected so tests
// can capture output instead of writing to a real stream.
func buildLogger(cfg config.LoggingConfig, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLogLevel(cfg.Level)}
	var handler slog.Handler
	if strings.EqualFold(cfg.Format, "text") {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}
	return slog.New(handler)
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func nonZero(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}

// formsWithOriginCheckDisabled returns the ids of every form whose
// allowed_origins contains api.DangerousDisableOriginCheck, sorted for
// stable log output — used to warn on every reload, not just once at
// startup, so it stays visible for as long as it's actually configured.
func formsWithOriginCheckDisabled(forms map[string]*app.CompiledForm) []string {
	var ids []string
	for id, cf := range forms {
		for _, o := range cf.Config.AllowedOrigins {
			if o == api.DangerousDisableOriginCheck {
				ids = append(ids, id)
				break
			}
		}
	}
	sort.Strings(ids)
	return ids
}

// sampleActiveBuckets periodically snapshots st's per-scope bucket counts
// into m.RatelimitBucketsActive until ctx is cancelled. A ticker, not a
// push on every Allow(), since the memory package deliberately has no
// metrics dependency (see ratelimit.Store's minimal interface).
func sampleActiveBuckets(ctx context.Context, st *memory.Store, m *metrics.Metrics) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for scope, n := range st.ActiveBucketsByScope() {
				m.RatelimitBucketsActive.WithLabelValues(scope).Set(float64(n))
			}
		}
	}
}

func buildRateLimiter(cfg config.RateLimitConfig, m *metrics.Metrics) (ratelimit.Store, func(), error) {
	switch cfg.Backend {
	case "valkey":
		st, err := valkey.New(valkey.Config{
			Addresses:      cfg.Valkey.Addresses,
			Password:       os.Getenv(cfg.Valkey.PasswordEnv),
			DB:             cfg.Valkey.DB,
			DialTimeout:    cfg.Valkey.DialTimeout.Std(),
			KeyPrefix:      cfg.Valkey.KeyPrefix,
			OnError:        valkey.OnError(cfg.Valkey.OnError),
			OnBackendError: func() { m.RatelimitBackendErrorsTotal.WithLabelValues("valkey").Inc() },
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
