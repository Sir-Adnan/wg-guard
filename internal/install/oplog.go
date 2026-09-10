package install

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/logsafe"
)

const (
	OperationLogDir      = DataDir + "/operations"
	OperationLogMaxBytes = 8 << 20
)

type operationAction string
type operationOutcome string

const (
	operationInstall   operationAction = "install"
	operationUpdate    operationAction = "update"
	operationRollback  operationAction = "rollback"
	operationUninstall operationAction = "uninstall"

	operationStarted   operationOutcome = "started"
	operationSucceeded operationOutcome = "succeeded"
	operationFailed    operationOutcome = "failed"
)

type operationJournal struct {
	host     Host
	now      func() time.Time
	maxBytes int64
}

type operationRecord struct {
	Time      string `json:"time"`
	Level     string `json:"level"`
	Message   string `json:"msg"`
	Source    string `json:"source"`
	Operation string `json:"operation"`
	Outcome   string `json:"outcome"`
	Mode      string `json:"mode"`
}

func newOperationJournal(host Host) operationJournal {
	return operationJournal{host: host, now: time.Now, maxBytes: OperationLogMaxBytes}
}

func ensureOperationRetention(ctx context.Context, host Host, state *State, persist bool) error {
	if state == nil {
		return fmt.Errorf("operation retention: install state is required")
	}
	owned := false
	for _, path := range state.ExtraFiles {
		owned = owned || path == OperationRetentionPath
	}
	previous, readErr := host.ReadFile(OperationRetentionPath)
	existed := readErr == nil
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		return readErr
	}
	if existed && !owned {
		return fmt.Errorf("operation retention: refusing unowned %s", OperationRetentionPath)
	}
	previousMode := fs.FileMode(0o644)
	if existed {
		if info, err := host.Stat(OperationRetentionPath); err != nil {
			return err
		} else {
			previousMode = info.Mode().Perm()
		}
	}
	restore := func() {
		if existed {
			_ = atomicWrite(host, OperationRetentionPath, previous, previousMode)
		} else {
			_ = host.Remove(OperationRetentionPath)
		}
	}
	if err := host.MkdirAll(OperationRetentionDir, 0o755); err != nil {
		return err
	}
	if err := atomicWrite(host, OperationRetentionPath, []byte(RenderOperationRetention()), 0o644); err != nil {
		return err
	}
	if err := runQuiet(ctx, host, []string{"systemd-tmpfiles", "--create", OperationRetentionPath}, 30*time.Second); err != nil {
		restore()
		return fmt.Errorf("operation retention: apply tmpfiles policy: %w", err)
	}
	next := *state
	next.ExtraFiles = addUnique(append([]string(nil), state.ExtraFiles...), OperationRetentionPath)
	if persist && !owned {
		if err := saveState(host, &next); err != nil {
			restore()
			return err
		}
	}
	*state = next
	return nil
}

func (j operationJournal) record(action operationAction, outcome operationOutcome, mode Mode) error {
	if !validOperationAction(action) || !validOperationOutcome(outcome) || !mode.Valid() {
		return fmt.Errorf("operation log: invalid fixed metadata")
	}
	if j.host == nil {
		return fmt.Errorf("operation log: host is required")
	}
	if j.now == nil {
		j.now = time.Now
	}
	if j.maxBytes <= 0 {
		j.maxBytes = OperationLogMaxBytes
	}
	now := j.now().UTC()
	var encoded bytes.Buffer
	handler := logsafe.New(slog.NewJSONHandler(&encoded, nil))
	record := slog.NewRecord(now, slog.LevelInfo, "lifecycle", 0)
	record.AddAttrs(
		slog.String("source", "operations"),
		slog.String("operation", string(action)),
		slog.String("outcome", string(outcome)),
		slog.String("mode", string(mode)),
	)
	if err := handler.Handle(context.Background(), record); err != nil {
		return err
	}
	if int64(encoded.Len()) > j.maxBytes {
		return fmt.Errorf("operation log: record exceeds storage cap")
	}
	if err := j.host.MkdirAll(OperationLogDir, 0o700); err != nil {
		return err
	}
	path := operationLogPath(now)
	if err := j.prune(now, int64(encoded.Len()+1)); err != nil {
		return err
	}
	payload := encoded.Bytes()
	if current, err := j.host.ReadFile(path); err == nil && len(current) > 0 && current[len(current)-1] != '\n' {
		payload = append([]byte{'\n'}, payload...)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return j.host.AppendFile(path, payload, 0o600)
}

func validOperationAction(action operationAction) bool {
	switch action {
	case operationInstall, operationUpdate, operationRollback, operationUninstall:
		return true
	default:
		return false
	}
}

func validOperationOutcome(outcome operationOutcome) bool {
	switch outcome {
	case operationStarted, operationSucceeded, operationFailed:
		return true
	default:
		return false
	}
}

func operationLogPath(at time.Time) string {
	return OperationLogDir + "/operations-" + at.UTC().Format(time.DateOnly) + ".jsonl"
}

type operationLogFile struct {
	name string
	date time.Time
	size int64
}

func parseOperationLogName(name string) (time.Time, bool) {
	const prefix, suffix = "operations-", ".jsonl"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return time.Time{}, false
	}
	dateText := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	date, err := time.Parse(time.DateOnly, dateText)
	return date, err == nil && date.Format(time.DateOnly) == dateText
}

func (j operationJournal) prune(now time.Time, incoming int64) error {
	entries, err := j.host.ReadDir(OperationLogDir)
	if err != nil {
		return err
	}
	day := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	cutoff := day.AddDate(0, 0, -6)
	files := make([]operationLogFile, 0, len(entries))
	for _, entry := range entries {
		date, ok := parseOperationLogName(entry.Name())
		if !ok || entry.IsDir() {
			continue
		}
		path := OperationLogDir + "/" + entry.Name()
		if date.Before(cutoff) {
			if err := j.host.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		files = append(files, operationLogFile{name: entry.Name(), date: date, size: info.Size()})
	}
	sort.Slice(files, func(a, b int) bool {
		if files[a].date.Equal(files[b].date) {
			return files[a].name < files[b].name
		}
		return files[a].date.Before(files[b].date)
	})
	var total int64
	for _, file := range files {
		total += file.size
	}
	for _, file := range files {
		if total+incoming <= j.maxBytes {
			break
		}
		if err := j.host.Remove(OperationLogDir + "/" + file.name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		total -= file.size
	}
	if total+incoming > j.maxBytes {
		return fmt.Errorf("operation log: storage cap cannot be enforced")
	}
	return nil
}

func parseOperationRecord(line []byte) (operationRecord, time.Time, bool) {
	var record operationRecord
	if len(line) > 4<<10 || json.Unmarshal(bytes.TrimSpace(line), &record) != nil {
		return record, time.Time{}, false
	}
	if record.Level != "INFO" || record.Message != "lifecycle" || record.Source != "operations" ||
		!validOperationAction(operationAction(record.Operation)) ||
		!validOperationOutcome(operationOutcome(record.Outcome)) || !Mode(record.Mode).Valid() {
		return record, time.Time{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, record.Time)
	return record, at, err == nil
}
