package install

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strconv"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/logsafe"
)

const (
	LogSourceService    = "service"
	LogSourceOperations = "operations"
	DefaultLogTail      = 200
	MaxLogTail          = 10_000
	MaxLogSince         = 7 * 24 * time.Hour
	maxLogLineBytes     = 64 << 10
)

// LogOptions is already parsed and bounded CLI input. Component is empty for
// all records or an exact logsafe component. Since is passed to the platform
// service as canonical UTC RFC3339, never as free-form input.
type LogOptions struct {
	Source    string
	Tail      int
	Since     time.Time
	Follow    bool
	Component string
}

// StreamLogs selects the deployment-owned service log source from validated
// install state. Docker and native mode stay host-side; no shell is involved.
func StreamLogs(ctx context.Context, h Host, state *State, options LogOptions, stdout, stderr io.Writer) error {
	if options.Source == "" {
		options.Source = LogSourceService
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
	switch options.Source {
	case LogSourceService:
	case LogSourceOperations:
		if options.Follow {
			return fmt.Errorf("logs: follow is available only for the service source")
		}
		if options.Component != "" {
			return fmt.Errorf("logs: component filtering is available only for the service source")
		}
	default:
		return fmt.Errorf("logs: unknown source %q", options.Source)
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if options.Source == LogSourceOperations {
		return streamOperationLogs(ctx, h, options, stdout)
	}
	if state == nil {
		return fmt.Errorf("logs: WG-Guard is not installed")
	}
	if err := validateState(state); err != nil {
		return fmt.Errorf("logs: install state: %w", err)
	}

	argv, err := logArgv(state.Mode, options)
	if err != nil {
		return err
	}
	logOutput := stdout
	if options.Component != "" {
		logOutput = &componentLineFilter{out: stdout, component: options.Component}
	}
	// Docker preserves the container's stdout/stderr split. WG-Guard's
	// structured logger writes to stderr, so route both container streams into
	// the command's single documented stdout stream. Native journalctl already
	// emits journal records on stdout and keeps diagnostics on stderr.
	sourceStderr := stderr
	if state.Mode == ModeDocker {
		sourceStderr = logOutput
	}
	if err := h.Stream(ctx, argv, logOutput, sourceStderr); err != nil {
		return fmt.Errorf("logs: %s source: %w", state.Mode, err)
	}
	return nil
}

func streamOperationLogs(ctx context.Context, host Host, options LogOptions, stdout io.Writer) error {
	entries, err := host.ReadDir(OperationLogDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("logs: operation source: %w", err)
	}
	names := make([]string, 0, len(entries))
	firstDay := time.Date(options.Since.UTC().Year(), options.Since.UTC().Month(), options.Since.UTC().Day(), 0, 0, 0, 0, time.UTC)
	for _, entry := range entries {
		date, ok := parseOperationLogName(entry.Name())
		if ok && !entry.IsDir() && !date.Before(firstDay) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	lines := make([][]byte, 0, min(options.Tail, 256))
	selectedBytes := 0
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		file, err := host.Open(OperationLogDir + "/" + name)
		if err != nil {
			return fmt.Errorf("logs: operation source: %w", err)
		}
		err = eachBoundedLogLine(file, func(line []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			record, at, ok := parseOperationRecord(line)
			if !ok || at.Before(options.Since) {
				return nil
			}
			canonical, err := json.Marshal(record)
			if err != nil {
				return nil
			}
			canonical = append(canonical, '\n')
			for len(lines) > 0 && (len(lines) >= options.Tail || selectedBytes+len(canonical) > OperationLogMaxBytes) {
				selectedBytes -= len(lines[0])
				lines = lines[1:]
			}
			if len(canonical) <= OperationLogMaxBytes {
				lines = append(lines, canonical)
				selectedBytes += len(canonical)
			}
			return nil
		})
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			return fmt.Errorf("logs: operation source: %w", errors.Join(err, closeErr))
		}
	}
	for _, line := range lines {
		if _, err := stdout.Write(line); err != nil {
			return fmt.Errorf("logs: operation output: %w", err)
		}
	}
	return nil
}

func eachBoundedLogLine(reader io.Reader, visit func([]byte) error) error {
	buffered := bufio.NewReaderSize(reader, maxLogLineBytes)
	for {
		line, err := buffered.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = buffered.ReadSlice('\n')
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			continue
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := visit(line); err != nil {
			return err
		}
	}
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
