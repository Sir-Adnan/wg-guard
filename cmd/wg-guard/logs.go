package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/logsafe"
)

func runLogs(args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	err := runLogsWith(ctx, args, install.NewRealHost(), os.Stdout, os.Stderr, time.Now())
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return nil
	}
	return err
}

func runLogsWith(ctx context.Context, args []string, host install.Host, stdout, stderr io.Writer, now time.Time) error {
	options, err := parseLogsOptions(args, now)
	if err != nil {
		return err
	}
	state, err := install.LoadState(host)
	if err != nil {
		return fmt.Errorf("logs: load install state: %w", err)
	}
	return install.StreamLogs(ctx, host, state, options, stdout, stderr)
}

func parseLogsOptions(args []string, now time.Time) (install.LogOptions, error) {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	tail := fs.Int("tail", install.DefaultLogTail, "maximum service records")
	since := fs.String("since", "24h", "bounded duration or RFC3339 instant")
	follow := fs.Bool("follow", false, "follow new service records")
	component := fs.String("component", "", "structured component filter")
	if err := fs.Parse(args); err != nil {
		return install.LogOptions{}, fmt.Errorf("logs: %w", err)
	}
	if fs.NArg() != 0 {
		return install.LogOptions{}, fmt.Errorf("logs: unexpected arguments")
	}
	if *tail < 1 || *tail > install.MaxLogTail {
		return install.LogOptions{}, fmt.Errorf("logs: --tail must be between 1 and %d", install.MaxLogTail)
	}
	instant, err := parseLogSince(*since, now)
	if err != nil {
		return install.LogOptions{}, err
	}
	if *component != "" {
		if _, ok := logsafe.ParseComponent(*component); !ok {
			return install.LogOptions{}, fmt.Errorf("logs: unknown --component %q", *component)
		}
	}
	return install.LogOptions{
		Tail: *tail, Since: instant, Follow: *follow, Component: *component,
	}, nil
}

func parseLogSince(value string, now time.Time) (time.Time, error) {
	now = now.UTC().Truncate(time.Second)
	if duration, err := time.ParseDuration(value); err == nil {
		if duration <= 0 || duration > install.MaxLogSince {
			return time.Time{}, fmt.Errorf("logs: --since duration must be greater than zero and at most 168h")
		}
		return now.Add(-duration), nil
	}
	instant, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("logs: --since must be a duration up to 168h or an RFC3339 instant")
	}
	instant = instant.UTC().Truncate(time.Second)
	if instant.After(now) || instant.Before(now.Add(-install.MaxLogSince)) {
		return time.Time{}, fmt.Errorf("logs: --since instant must be within the previous 168h")
	}
	return instant, nil
}
