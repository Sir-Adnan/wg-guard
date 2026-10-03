package updatequeue

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const MaxHistory = 100

type OperationError struct {
	Code  Failure
	Cause error
}

func (e *OperationError) Error() string { return string(e.Code) }
func (e *OperationError) Unwrap() error { return e.Cause }

func failureCode(err error) Failure {
	var operation *OperationError
	if errors.As(err, &operation) && validFailure(operation.Code) {
		return operation.Code
	}
	return FailureOperation
}
func validFailure(code Failure) bool {
	switch code {
	case "", FailureOperation, FailureInterrupted, "missed_window", "acquisition_failed", "preflight_blocked", "backup_failed", "health_failed", "core_failed", "reboot_required", "recovery_failed", "deployment_failed", "identity_changed", "authorization_revoked":
		return true
	}
	return false
}

// Step is a closed, secret-free event. Output and subprocess errors never enter
// this record; the web catalog supplies localized descriptions for these IDs.
type Step struct {
	Component  string    `json:"component"`
	Stage      string    `json:"stage"`
	State      State     `json:"state"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
}

var components = map[string]bool{"host": true, "manager": true, "panel": true, "core": true}
var stages = map[string]bool{
	"inspect": true, "preflight": true, "acquire": true, "verify": true,
	"image": true, "backup": true, "deploy": true, "health": true,
	"manager": true, "core": true, "recovery": true, "complete": true,
}

func validActor(actor string) bool {
	if len(actor) > 128 {
		return false
	}
	return !strings.ContainsFunc(actor, unicode.IsControl)
}

func validStatus(s Status) bool {
	if s.Schema != Schema || !safeRequestID.MatchString(s.ID) || !validInput(s.Input) || !validActor(s.Actor) || !validActor(s.ActorID) || len(s.Steps) > 48 {
		return false
	}
	switch s.State {
	case StateQueued, StateRunning, StateSucceeded, StateFailed, StateScheduled, StateCanceled:
	default:
		return false
	}
	for _, step := range s.Steps {
		if !components[step.Component] || !stages[step.Stage] || step.State != StateRunning && step.State != StateSucceeded && step.State != StateFailed {
			return false
		}
	}
	if s.Outcome != nil {
		for _, value := range []string{s.Outcome.PanelVersion, s.Outcome.ManagerVersion, s.Outcome.Bundle, s.Outcome.Recovery} {
			if !validActor(value) {
				return false
			}
		}
		if b := s.Outcome.Backup; b != nil && (!safeRequestID.MatchString(b.OperationID) || !safeCatalogID.MatchString(b.Name) || len(b.SHA256) != 64) {
			return false
		}
	}
	return validFailure(s.Failure)
}

func (q *Queue) RecordOutcome(req Request, outcome Outcome) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	s, err := q.Status()
	if err != nil || s.ID != req.ID || s.State != StateRunning {
		return ErrInvalid
	}
	s.Outcome = &outcome
	s.Revision++
	if !validStatus(s) {
		return ErrInvalid
	}
	return writeJSONAtomic(q.Paths().Status, s, true)
}

func (q *Queue) Record(req Request, component, stage string, state State) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !components[component] || !stages[stage] || state != StateRunning && state != StateSucceeded && state != StateFailed {
		return ErrInvalid
	}
	s, err := q.Status()
	if err != nil || s.ID != req.ID || s.State != StateRunning {
		return ErrInvalid
	}
	now := q.now()
	index := -1
	for i := len(s.Steps) - 1; i >= 0; i-- {
		if s.Steps[i].Component == component && s.Steps[i].Stage == stage {
			index = i
			break
		}
	}
	if index < 0 {
		if len(s.Steps) >= 48 {
			return ErrInvalid
		}
		s.Steps = append(s.Steps, Step{Component: component, Stage: stage, StartedAt: now})
		index = len(s.Steps) - 1
	}
	s.Steps[index].State = state
	if state != StateRunning {
		s.Steps[index].FinishedAt = now
	}
	s.Revision++
	return writeJSONAtomic(q.Paths().Status, s, true)
}

func (q *Queue) historyPath() string  { return filepath.Join(q.Dir, "update-history.json") }
func (q *Queue) schedulePath() string { return filepath.Join(q.Dir, "update-scheduled.json") }

func (q *Queue) History() ([]Status, error) {
	raw, err := readBoundedRegular(q.historyPath(), 1<<20)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var history []Status
	if json.Unmarshal(raw, &history) != nil || len(history) > MaxHistory {
		return nil, ErrInvalid
	}
	for _, s := range history {
		if !validStatus(s) || s.State == StateQueued || s.State == StateRunning || s.State == StateScheduled {
			return nil, ErrInvalid
		}
	}
	return history, nil
}

func (q *Queue) archive(s Status) error {
	history, err := q.History()
	if err != nil {
		return err
	}
	for i, old := range history {
		if old.ID == s.ID {
			history = append(history[:i], history[i+1:]...)
			break
		}
	}
	history = append([]Status{s}, history...)
	if len(history) > MaxHistory {
		history = history[:MaxHistory]
	}
	for len(history) > 1 {
		raw, err := json.Marshal(history)
		if err != nil {
			return err
		}
		if len(raw) <= 1<<20 {
			break
		}
		history = history[:len(history)-1]
	}
	return writeJSONAtomic(q.historyPath(), history, true)
}

func (q *Queue) Schedule(ctx context.Context, input Input, actor string, at time.Time) (Status, error) {
	return q.ScheduleFor(ctx, input, actor, "", at)
}

func (q *Queue) ScheduleFor(ctx context.Context, input Input, actor, actorID string, at time.Time) (Status, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if ctx.Err() != nil {
		return Status{}, ctx.Err()
	}
	if !q.Enhanced() {
		return Status{}, ErrUnavailable
	}
	now := q.now()
	at = at.UTC()
	if !legacyOperation(input.Operation) || !validInput(input) || !validActor(actor) || !validActor(actorID) || at.Before(now.Add(time.Minute)) || at.After(now.Add(30*24*time.Hour)) {
		return Status{}, ErrInvalid
	}
	if err := q.clearExpiredActive(q.Paths()); err != nil {
		return Status{}, err
	}
	req := Request{Schema: Schema, ID: nonce(), CreatedAt: now, Actor: actor, ActorID: actorID, ExecuteAt: at, Input: input}
	s := statusFrom(req, StateScheduled)
	// The schedule is published last. It is separate from the systemd path's
	// immediate-request file, so a future operation cannot cause a busy loop.
	if err := writeJSONAtomic(q.Paths().Status, s, true); err != nil {
		return Status{}, err
	}
	if err := writeJSONAtomic(q.schedulePath(), req, false); err != nil {
		return Status{}, err
	}
	return s, nil
}

// PromoteDue is called only by the fixed host timer. Missed windows are closed
// instead of unexpectedly running maintenance after a long host outage.
func (q *Queue) PromoteDue() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	// Recover only our fixed promotion record; no elapsed-time lock stealing.
	claim := q.schedulePath() + ".claimed"
	if pathOccupied(claim) {
		if pathOccupied(q.Paths().Request) || pathOccupied(q.Paths().Running) {
			if err := os.Remove(claim); err != nil {
				return err
			}
		} else {
			if err := os.Rename(claim, q.schedulePath()); err != nil {
				return err
			}
		}
	}
	raw, err := readBoundedRegular(q.schedulePath(), 16<<10)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var req Request
	if json.Unmarshal(raw, &req) != nil || !validRequest(req) || req.ExecuteAt.IsZero() {
		return ErrInvalid
	}
	if q.now().Before(req.ExecuteAt) {
		return nil
	}
	if pathOccupied(q.Paths().Request) || pathOccupied(q.Paths().Running) {
		return ErrBusy
	}
	// Rename arbitrates with cancellation across the web/host processes.
	claim = q.schedulePath() + ".claimed"
	if err := os.Rename(q.schedulePath(), claim); err != nil {
		return err
	}
	if q.now().After(req.ExecuteAt.Add(30 * time.Minute)) {
		s := statusFrom(req, StateCanceled)
		s.Failure = "missed_window"
		s.FinishedAt = q.now()
		if err := writeJSONAtomic(q.Paths().Status, s, true); err != nil {
			return err
		}
		if err := q.archive(s); err != nil {
			return err
		}
		return os.Remove(claim)
	}
	s := statusFrom(req, StateQueued)
	if err := writeJSONAtomic(q.Paths().Status, s, true); err != nil {
		return err
	}
	if err := writeJSONAtomic(q.Paths().Request, req, false); err != nil {
		_ = os.Rename(claim, q.schedulePath())
		return err
	}
	return os.Remove(claim)
}

// Cancel wins only if the host has not claimed the exact request. Running
// maintenance is never terminated by a web action.
func (q *Queue) Cancel(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !safeRequestID.MatchString(id) {
		return ErrInvalid
	}
	for _, file := range []string{q.schedulePath(), q.Paths().Request} {
		raw, err := readBoundedRegular(file, 16<<10)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		var req Request
		if json.Unmarshal(raw, &req) != nil || !validRequest(req) || req.ID != id {
			return ErrInvalid
		}
		claim := file + ".cancel-" + nonce()
		if err := os.Rename(file, claim); err != nil {
			return ErrBusy
		}
		s := statusFrom(req, StateCanceled)
		s.FinishedAt = q.now()
		if err := writeJSONAtomic(q.Paths().Status, s, true); err != nil {
			return err
		}
		if err := q.archive(s); err != nil {
			return err
		}
		return os.Remove(claim)
	}
	return ErrBusy
}

// ReconcileInterrupted is host-only: systemd serializes runner invocations.
// It records the prior interrupted job but leaves the lifecycle journal intact;
// subsequent execution still has to pass that journal's recovery gate.
func (q *Queue) ReconcileInterrupted() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	raw, err := readBoundedRegular(q.Paths().Running, 16<<10)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var req Request
	if json.Unmarshal(raw, &req) != nil || !validRequest(req) {
		return ErrInvalid
	}
	s, err := q.Status()
	if err != nil || s.ID != req.ID {
		return ErrInvalid
	}
	if s.State == StateRunning || s.State == StateQueued {
		s.State = StateFailed
		s.Failure = FailureInterrupted
		s.FinishedAt = q.now()
		s.Revision++
	}
	if err := writeJSONAtomic(q.Paths().Status, s, true); err != nil {
		return err
	}
	if err := q.archive(s); err != nil {
		return err
	}
	return os.Remove(q.Paths().Running)
}
