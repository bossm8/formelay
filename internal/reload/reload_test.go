package reload

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// waitFor polls cond until it's true or the timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("condition not met within %v", timeout)
	}
}

func TestWatchFiles(t *testing.T) {
	t.Run("a file change triggers reload after the debounce window", func(t *testing.T) {
		dir := t.TempDir()
		globalPath := filepath.Join(dir, "config.yaml")
		formsDir := filepath.Join(dir, "forms.d")
		writeFile(t, globalPath, "x: 1\n")
		if err := os.MkdirAll(formsDir, 0o755); err != nil {
			t.Fatal(err)
		}

		var count int32
		reload := func() error {
			atomic.AddInt32(&count, 1)
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		if err := WatchFiles(ctx, log, globalPath, formsDir, 50*time.Millisecond, reload); err != nil {
			t.Fatalf("WatchFiles: %v", err)
		}

		writeFile(t, globalPath, "x: 2\n")
		waitFor(t, 2*time.Second, func() bool { return atomic.LoadInt32(&count) >= 1 })
	})

	t.Run("multiple rapid writes within the debounce window trigger only one reload", func(t *testing.T) {
		dir := t.TempDir()
		globalPath := filepath.Join(dir, "config.yaml")
		formsDir := filepath.Join(dir, "forms.d")
		writeFile(t, globalPath, "x: 1\n")
		if err := os.MkdirAll(formsDir, 0o755); err != nil {
			t.Fatal(err)
		}

		var mu sync.Mutex
		var count int
		reload := func() error {
			mu.Lock()
			count++
			mu.Unlock()
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		debounce := 150 * time.Millisecond
		if err := WatchFiles(ctx, log, globalPath, formsDir, debounce, reload); err != nil {
			t.Fatalf("WatchFiles: %v", err)
		}

		for i := 0; i < 5; i++ {
			writeFile(t, globalPath, "x: rapid\n")
			time.Sleep(10 * time.Millisecond)
		}

		// Wait well past the debounce window from the last write, then check
		// exactly once more after settling to make sure no extra reload fires.
		time.Sleep(debounce + 300*time.Millisecond)
		mu.Lock()
		got := count
		mu.Unlock()
		if got != 1 {
			t.Fatalf("reload was called %d times for a burst of rapid writes within the debounce window, want exactly 1", got)
		}
	})

	t.Run("a reload error is logged, not swallowed silently", func(t *testing.T) {
		dir := t.TempDir()
		globalPath := filepath.Join(dir, "config.yaml")
		formsDir := filepath.Join(dir, "forms.d")
		writeFile(t, globalPath, "x: 1\n")
		if err := os.MkdirAll(formsDir, 0o755); err != nil {
			t.Fatal(err)
		}

		reload := func() error { return errBoom }

		var buf bytes.Buffer
		var mu sync.Mutex
		log := slog.New(slog.NewTextHandler(&syncWriter{buf: &buf, mu: &mu}, nil))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := WatchFiles(ctx, log, globalPath, formsDir, 20*time.Millisecond, reload); err != nil {
			t.Fatalf("WatchFiles: %v", err)
		}

		writeFile(t, globalPath, "x: 2\n")
		waitFor(t, 2*time.Second, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return buf.Len() > 0
		})
		mu.Lock()
		out := buf.String()
		mu.Unlock()
		if !bytes.Contains([]byte(out), []byte("config reload failed")) {
			t.Fatalf("expected the reload error to be logged, got: %s", out)
		}
	})
}

type syncWriter struct {
	buf *bytes.Buffer
	mu  *sync.Mutex
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

var errBoom = errors.New("boom")

func TestHandleSIGHUP(t *testing.T) {
	var count int32
	reload := func() error {
		atomic.AddInt32(&count, 1)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	HandleSIGHUP(ctx, log, reload)

	// Give the signal.Notify goroutine a moment to register before sending.
	time.Sleep(50 * time.Millisecond)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Skipf("cannot send SIGHUP to self in this environment: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool { return atomic.LoadInt32(&count) >= 1 })
}
