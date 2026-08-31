package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/serve"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/fake"
)

// gracefulTimeout bounds in-flight request draining on shutdown.
const gracefulTimeout = 10 * time.Second

// runServe composes the node: boot config (file + env), the full bring-up
// sequence, the REST API over HTTP(S), and the central scheduler. It blocks
// until SIGINT/SIGTERM, then drains gracefully.
//
// -backend fake substitutes the in-memory tunnel backend (dev/benchmarks:
// no root, no AWG tooling, no host networking). It never touches tunnels and
// says so loudly in the log.
func runServe(args []string) error {
	configPath := "/etc/wg-guard/wg-guard.toml"
	var backendOpts []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-config", "--config":
			if i+1 >= len(args) {
				return fmt.Errorf("-config requires a path")
			}
			i++
			configPath = args[i]
		case "-backend", "--backend":
			if i+1 >= len(args) {
				return fmt.Errorf("-backend requires a value (fake)")
			}
			i++
			backendOpts = append(backendOpts, args[i])
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	var devBackend bool
	for _, b := range backendOpts {
		if b != "fake" {
			return fmt.Errorf("unknown backend %q (only \"fake\" is available)", b)
		}
		devBackend = true
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	log, err := buildLogger(cfg)
	if err != nil {
		return err
	}

	opts := serve.Options{Config: cfg, ConfigPath: configPath, Log: log}
	if devBackend {
		opts.Backend = fake.New()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	node, err := serve.Start(ctx, opts)
	if err != nil {
		return err
	}
	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), gracefulTimeout)
	defer cancel()
	if err := node.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("stopped")
	return nil
}

func buildLogger(cfg *config.Config) (*slog.Logger, error) {
	level := slog.LevelInfo
	switch cfg.Log.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if cfg.Log.Format == "json" {
		h = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		h = slog.NewTextHandler(os.Stderr, opts)
	}
	return slog.New(h), nil
}
