// Package reload wires fsnotify directory watching and SIGHUP handling to a
// reload callback, debounced so editors/ConfigMap syncers that emit several
// filesystem events per logical change don't trigger repeated reloads.
package reload

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatchFiles watches globalPath's parent directory and formsDir
// (directory-level, so atomic renames are caught) and calls reload after a
// debounce window following any change.
func WatchFiles(ctx context.Context, log *slog.Logger, globalPath, formsDir string, debounce time.Duration, reload func() error) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := watcher.Add(filepath.Dir(globalPath)); err != nil {
		watcher.Close()
		return err
	}
	if err := watcher.Add(formsDir); err != nil {
		watcher.Close()
		return err
	}

	go func() {
		defer watcher.Close()
		var timer *time.Timer
		fire := func() {
			if err := reload(); err != nil {
				log.Error("config reload failed", "error", err)
			} else {
				log.Info("config reloaded")
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-watcher.Events:
				if !ok {
					return
				}
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(debounce, fire)
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Error("config watcher error", "error", err)
			}
		}
	}()
	return nil
}

// HandleSIGHUP calls reload every time the process receives SIGHUP.
func HandleSIGHUP(ctx context.Context, log *slog.Logger, reload func() error) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		defer signal.Stop(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				if err := reload(); err != nil {
					log.Error("config reload failed (SIGHUP)", "error", err)
				} else {
					log.Info("config reloaded (SIGHUP)")
				}
			}
		}
	}()
}
