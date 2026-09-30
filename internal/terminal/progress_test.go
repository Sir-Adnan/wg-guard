package terminal

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTaskProgressIsContextualAndBoundedWhenRedirected(t *testing.T) {
	var out bytes.Buffer
	task := New(nil, &out, Options{Width: 48}).BeginTask("Building verified Docker runtime")
	task.Tick(15 * time.Second)
	task.Tick(61 * time.Second)
	task.Tick(75 * time.Second)
	task.Done(nil)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "[RUN] Building verified Docker runtime") || !strings.Contains(lines[1], "1m1s") || !strings.Contains(lines[2], "[OK]") {
		t.Fatalf("unexpected redirected progress: %q", out.String())
	}
	if strings.Contains(out.String(), "\x1b[") || strings.Contains(out.String(), "Still working") {
		t.Fatalf("redirected progress contains control codes or generic heartbeat: %q", out.String())
	}
}

func TestTaskProgressUsesOneNarrowTTYLineAndReportsFailure(t *testing.T) {
	t.Setenv("TERM", "xterm")
	var out bytes.Buffer
	task := New(nil, &out, Options{Width: 40, TTY: true}).BeginTask("A very long operational task with details")
	task.Tick(15 * time.Second)
	task.Done(errors.New("failed"))
	if strings.Count(out.String(), "\n") != 1 || !strings.Contains(out.String(), "\r\x1b[2K") || !strings.Contains(out.String(), "[FAIL]") {
		t.Fatalf("unexpected TTY progress: %q", out.String())
	}
	for _, draw := range strings.Split(out.String(), "\r\x1b[2K") {
		if n := len([]rune(strings.TrimSpace(draw))); n > 40 {
			t.Fatalf("TTY progress overflowed 40 columns (%d): %q", n, draw)
		}
	}
}
