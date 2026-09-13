// Package updatequeue bridges the unprivileged web workflow to the existing
// host-owned lifecycle manager. The panel can enqueue only catalog identities;
// it never supplies argv, shell text, credentials or executable paths.
package updatequeue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"time"
)

const (
	Schema      = 1
	activeLease = time.Hour
)

type Operation string
type State string
type Failure string

const (
	OperationPanel Operation = "panel"
	OperationCore  Operation = "core"
	OperationAll   Operation = "all"

	StateQueued    State = "queued"
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"

	FailureOperation   Failure = "operation_failed"
	FailureInterrupted Failure = "broker_interrupted"
)

var (
	ErrUnavailable = errors.New("update queue: host broker unavailable")
	ErrBusy        = errors.New("update queue: operation already active")
	ErrInvalid     = errors.New("update queue: invalid request")
	ErrNoRequest   = errors.New("update queue: no queued request")
	safeCatalogID  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	safeRequestID  = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

type Input struct {
	Operation Operation `json:"operation"`
	Channel   string    `json:"channel,omitempty"`
	Ref       string    `json:"ref,omitempty"`
	Core      string    `json:"core,omitempty"`
}

type Request struct {
	Schema    int       `json:"schema"`
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Input
}

type Status struct {
	Schema     int       `json:"schema"`
	ID         string    `json:"id"`
	State      State     `json:"state"`
	CreatedAt  time.Time `json:"created_at"`
	StartedAt  time.Time `json:"started_at,omitzero"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
	Failure    Failure   `json:"failure,omitempty"`
	Input
}

type Paths struct {
	Marker, Request, Running, Status string
}

type Queue struct {
	Dir string
	Now func() time.Time
	mu  sync.Mutex
}

func New(dataDir string) *Queue { return &Queue{Dir: dataDir} }

func (q *Queue) Paths() Paths {
	return Paths{
		Marker:  filepath.Join(q.Dir, "update-broker.json"),
		Request: filepath.Join(q.Dir, "update-request.json"),
		Running: filepath.Join(q.Dir, "update-running.json"),
		Status:  filepath.Join(q.Dir, "update-status.json"),
	}
}

func ReadyMarker() []byte { return []byte("{\"schema\":1}\n") }

func (q *Queue) Available() bool {
	raw, err := readBoundedRegular(q.Paths().Marker, 128)
	if err != nil {
		return false
	}
	var marker struct {
		Schema int `json:"schema"`
	}
	return json.Unmarshal(raw, &marker) == nil && marker.Schema == Schema
}

func (q *Queue) Enqueue(ctx context.Context, input Input) (Status, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	if !q.Available() {
		return Status{}, ErrUnavailable
	}
	if !validInput(input) {
		return Status{}, ErrInvalid
	}
	paths := q.Paths()
	if err := q.clearExpiredActive(paths); err != nil {
		return Status{}, err
	}
	now := q.now()
	req := Request{Schema: Schema, ID: nonce(), CreatedAt: now, Input: input}
	status := statusFrom(req, StateQueued)
	if err := writeJSONAtomic(paths.Status, status, true); err != nil {
		return Status{}, err
	}
	// Request is published last: the host watcher never sees a request whose
	// initial status has not already been durably written.
	if err := writeJSONAtomic(paths.Request, req, false); err != nil {
		return Status{}, err
	}
	return status, nil
}

func (q *Queue) Claim(ctx context.Context) (Request, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Request{}, err
	}
	paths := q.Paths()
	if pathOccupied(paths.Running) {
		return Request{}, ErrBusy
	}
	if err := os.Rename(paths.Request, paths.Running); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Request{}, ErrNoRequest
		}
		return Request{}, err
	}
	raw, err := readBoundedRegular(paths.Running, 16<<10)
	if err != nil {
		_ = os.Remove(paths.Running)
		return Request{}, err
	}
	var req Request
	if json.Unmarshal(raw, &req) != nil || !validRequest(req) {
		_ = os.Remove(paths.Running)
		return Request{}, ErrInvalid
	}
	status := statusFrom(req, StateRunning)
	status.StartedAt = q.now()
	if err := writeJSONAtomic(paths.Status, status, true); err != nil {
		_ = os.Rename(paths.Running, paths.Request)
		return Request{}, err
	}
	return req, nil
}

func (q *Queue) Finish(ctx context.Context, req Request, operationErr error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	paths := q.Paths()
	raw, err := readBoundedRegular(paths.Running, 16<<10)
	if err != nil {
		return err
	}
	var active Request
	if json.Unmarshal(raw, &active) != nil || !validRequest(active) || active.ID != req.ID {
		return ErrInvalid
	}
	state := StateSucceeded
	failure := Failure("")
	if operationErr != nil {
		state = StateFailed
		failure = FailureOperation
	}
	status := statusFrom(active, state)
	status.StartedAt = q.statusStartedAt(active.ID)
	status.FinishedAt = q.now()
	status.Failure = failure
	if err := writeJSONAtomic(paths.Status, status, true); err != nil {
		return err
	}
	if err := os.Remove(paths.Running); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return syncDirectory(q.Dir)
}

func (q *Queue) Status() (Status, error) {
	raw, err := readBoundedRegular(q.Paths().Status, 16<<10)
	if errors.Is(err, fs.ErrNotExist) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	var status Status
	if json.Unmarshal(raw, &status) != nil || status.Schema != Schema || status.ID == "" || !validInput(status.Input) ||
		(status.State != StateQueued && status.State != StateRunning && status.State != StateSucceeded && status.State != StateFailed) {
		return Status{}, ErrInvalid
	}
	if (status.State == StateQueued || status.State == StateRunning) && q.now().Sub(status.CreatedAt) > activeLease {
		status.State = StateFailed
		status.FinishedAt = q.now()
		status.Failure = FailureInterrupted
	}
	return status, nil
}

func (q *Queue) clearExpiredActive(paths Paths) error {
	removed := false
	for _, path := range []string{paths.Request, paths.Running} {
		raw, err := readBoundedRegular(path, 16<<10)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return ErrBusy
		}
		var request Request
		if json.Unmarshal(raw, &request) != nil || !validRequest(request) || q.now().Sub(request.CreatedAt) <= activeLease {
			return ErrBusy
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		removed = true
	}
	if removed {
		return syncDirectory(q.Dir)
	}
	return nil
}

func statusFrom(req Request, state State) Status {
	return Status{Schema: Schema, ID: req.ID, State: state, CreatedAt: req.CreatedAt, Input: req.Input}
}

func (q *Queue) statusStartedAt(id string) time.Time {
	status, err := q.Status()
	if err == nil && status.ID == id {
		return status.StartedAt
	}
	return time.Time{}
}

func validRequest(req Request) bool {
	return req.Schema == Schema && safeRequestID.MatchString(req.ID) && !req.CreatedAt.IsZero() && validInput(req.Input)
}

func validInput(input Input) bool {
	panel := input.Channel == "release" && safeCatalogID.MatchString(input.Ref)
	core := safeCatalogID.MatchString(input.Core)
	switch input.Operation {
	case OperationPanel:
		return panel && input.Core == ""
	case OperationCore:
		return core && input.Channel == "" && input.Ref == ""
	case OperationAll:
		return panel && core
	default:
		return false
	}
}

func ValidateInput(input Input) error {
	if !validInput(input) {
		return ErrInvalid
	}
	return nil
}

func (q *Queue) now() time.Time {
	if q.Now != nil {
		return q.Now().UTC()
	}
	return time.Now().UTC()
}

func nonce() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic("update queue: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(value[:])
}

func pathOccupied(path string) bool {
	_, err := os.Lstat(path)
	return err == nil || !errors.Is(err, fs.ErrNotExist)
}

func readBoundedRegular(path string, max int64) ([]byte, error) {
	stat, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() || stat.Size() > max {
		return nil, ErrInvalid
	}
	return os.ReadFile(path)
}

func writeJSONAtomic(path string, value any, replace bool) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp := path + ".tmp-" + nonce()
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(raw)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		return errors.Join(writeErr, closeErr)
	}
	if !replace {
		if err := os.Link(tmp, path); err != nil {
			_ = os.Remove(tmp)
			if errors.Is(err, fs.ErrExist) {
				return ErrBusy
			}
			return err
		}
		_ = os.Remove(tmp)
		return syncDirectory(dir)
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return syncDirectory(dir)
}

func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("update queue: sync directory: %w", err)
	}
	return nil
}
