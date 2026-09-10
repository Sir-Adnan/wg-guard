package main

import (
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/install"
)

func TestParseLogsOptions(t *testing.T) {
	now := time.Date(2026, 9, 10, 8, 30, 45, 0, time.UTC)
	tests := []struct {
		name          string
		args          []string
		wantTail      int
		wantSince     time.Time
		wantFollow    bool
		wantComponent string
	}{
		{
			name: "recommended defaults", wantTail: 200,
			wantSince: now.Add(-24 * time.Hour),
		},
		{
			name:     "duration follow and component",
			args:     []string{"--tail", "750", "--since", "90m", "--follow", "--component", "http"},
			wantTail: 750, wantSince: now.Add(-90 * time.Minute), wantFollow: true, wantComponent: "http",
		},
		{
			name:     "bounded instant",
			args:     []string{"--since", "2026-09-05T04:03:02+03:30", "--component", "awg"},
			wantTail: 200, wantSince: time.Date(2026, 9, 5, 0, 33, 2, 0, time.UTC), wantComponent: "awg",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLogsOptions(tc.args, now)
			if err != nil {
				t.Fatal(err)
			}
			if got.Tail != tc.wantTail || !got.Since.Equal(tc.wantSince) || got.Follow != tc.wantFollow || got.Component != tc.wantComponent {
				t.Fatalf("options = %+v", got)
			}
		})
	}
}

func TestParseLogsOptionsRejectsUnsafeOrUnboundedInput(t *testing.T) {
	now := time.Date(2026, 9, 10, 8, 30, 45, 0, time.UTC)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"zero tail", []string{"--tail", "0"}},
		{"negative tail", []string{"--tail", "-1"}},
		{"oversized tail", []string{"--tail", "10001"}},
		{"zero since", []string{"--since", "0s"}},
		{"negative since", []string{"--since", "-1h"}},
		{"oversized since", []string{"--since", "169h"}},
		{"old instant", []string{"--since", "2026-09-01T00:00:00Z"}},
		{"future instant", []string{"--since", "2026-09-11T00:00:00Z"}},
		{"free form since", []string{"--since", "yesterday"}},
		{"unknown component", []string{"--component", "database"}},
		{"component injection", []string{"--component", "http --follow"}},
		{"extra arg", []string{"unexpected"}},
		{"unknown flag", []string{"--source", "docker"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseLogsOptions(tc.args, now); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestLogsConstantsMatchPublicBounds(t *testing.T) {
	if install.DefaultLogTail != 200 || install.MaxLogTail != 10_000 || install.MaxLogSince != 7*24*time.Hour {
		t.Fatal("logs CLI and install engine bounds diverged")
	}
}
