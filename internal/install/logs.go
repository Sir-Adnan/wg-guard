package install

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/logsafe"
)

const (
	DefaultLogTail  = 200
	MaxLogTail      = 10_000
	MaxLogSince     = 7 * 24 * time.Hour
	maxLogLineBytes = 64 << 10
)

// LogOptions is already parsed and bounded CLI input. Component is empty for
// all records or an exact logsafe component. Since is passed to the platform
// service as canonical UTC RFC3339, never as free-form input.
type LogOptions struct {
	Tail      int
	Since     time.Time
	Follow    bool
	Component string
}

// StreamLogs selects the deployment-owned service log source from validated
// install state. Docker and native mode stay host-side; no shell is involved.
func StreamLogs(ctx context.Context, h Host, state *State, options LogOptions, stdout, stderr io.Writer) error {
	if state == nil {
		return fmt.Errorf("logs: WG-Guard is not installed")
	}
	if err := validateState(state); err != nil {
		return fmt.Errorf("logs: install state: %w", err)
	}
	if options.Tail < 1 || options.Tail > MaxLogTail {
		return fmt.Errorf("logs: tail must be between 1 and %d", MaxLogTail)
	}
	if options.Since.IsZero() {
		return fmt.Errorf("logs: since is required")
	}
	if options.Component != "" {
		if _, ok := logsafe.ParseComponent(options.Component); !ok {
			return fmt.Errorf("logs: unknown component %q", options.Component)
		}
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	argv, err := logArgv(state.Mode, options)
	if err != nil {
		return err
	}
	if options.Component != "" {
		stdout = &componentLineFilter{out: stdout, component: options.Component}
	}
	if err := h.Stream(ctx, argv, stdout, stderr); err != nil {
		return fmt.Errorf("logs: %s source: %w", state.Mode, err)
	}
	return nil
}

func logArgv(mode Mode, options LogOptions) ([]string, error) {
	since := options.Since.UTC().Format(time.RFC3339)
	tail := strconv.Itoa(options.Tail)
	switch mode {
	case ModeDocker:
		argv := []string{"docker", "logs", "--tail", tail, "--since", since}
		if options.Follow {
			argv = append(argv, "--follow")
		}
		return append(argv, Container), nil
	case ModeNative:
		argv := []string{
			"journalctl", "--namespace=wg-guard", "--unit=wg-guard.service",
			"--no-pager", "--output=cat", "--lines", tail, "--since", since,
		}
		if options.Follow {
			argv = append(argv, "--follow")
		}
		return argv, nil
	default:
		return nil, fmt.Errorf("logs: unsupported install mode %q", mode)
	}
}

type componentLineFilter struct {
	out       io.Writer
	component string
	line      []byte
	dropping  bool
}

func (f *componentLineFilter) Write(data []byte) (int, error) {
	written := 0
	for len(data) > 0 {
		newline := bytes.IndexByte(data, '\n')
		partLen := len(data)
		complete := false
		if newline >= 0 {
			partLen = newline + 1
			complete = true
		}
		part := data[:partLen]
		data = data[partLen:]
		written += partLen

		if !f.dropping {
			if len(f.line)+len(part) > maxLogLineBytes {
				clear(f.line)
				f.line = f.line[:0]
				f.dropping = true
			} else {
				f.line = append(f.line, part...)
			}
		}
		if !complete {
			continue
		}
		if !f.dropping && lineHasComponent(f.line, f.component) {
			if _, err := f.out.Write(f.line); err != nil {
				return written, err
			}
		}
		clear(f.line)
		f.line = f.line[:0]
		f.dropping = false
	}
	return written, nil
}

func lineHasComponent(line []byte, component string) bool {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var record struct {
			Component string `json:"component"`
		}
		if json.Unmarshal(trimmed, &record) == nil {
			return record.Component == component
		}
	}
	target := []byte("component=" + component)
	start := 0
	inQuote := false
	escaped := false
	for index, char := range line {
		if inQuote {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
			} else if char == '"' {
				inQuote = false
			}
			continue
		}
		switch char {
		case '"':
			inQuote = true
		case ' ', '\t', '\r', '\n':
			if bytes.Equal(line[start:index], target) {
				return true
			}
			start = index + 1
		}
	}
	return bytes.Equal(bytes.TrimSpace(line[start:]), target)
}
