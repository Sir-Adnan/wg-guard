// Package domainqueue is the bounded certificate-specific host mailbox. It
// transports approved domain intent and private staged imports, never commands.
package domainqueue

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/domaintls"
)

const Schema = 1

var ErrUnavailable = errors.New("domain host bridge is unavailable")
var ErrBusy = errors.New("domain operation is active")
var ErrInvalid = errors.New("domain request is invalid")

type Input struct {
	Operation        string           `json:"operation"` // configure, renew, remove, inspect, recover
	Role             domaintls.Role   `json:"role,omitempty"`
	Origin           string           `json:"origin,omitempty"`
	Method           domaintls.Method `json:"method,omitempty"`
	ExpectedRevision string           `json:"expected_revision,omitempty"`
	StageID          string           `json:"stage_id,omitempty"`
	CertSource       string           `json:"cert_source,omitempty"`
	KeySource        string           `json:"key_source,omitempty"`
	Email            string           `json:"email,omitempty"`
	Challenge        string           `json:"challenge,omitempty"`
}

type Request struct {
	Schema    int       `json:"schema"`
	ID        string    `json:"id"`
	ActorID   string    `json:"actor_id"`
	CreatedAt time.Time `json:"created_at"`
	Input
}

type Status struct {
	Schema    int            `json:"schema"`
	ID        string         `json:"id"`
	State     string         `json:"state"`
	Stage     string         `json:"stage"`
	Failure   string         `json:"failure,omitempty"`
	UpdatedAt time.Time      `json:"updated_at"`
	Role      domaintls.Role `json:"role,omitempty"`
	Origin    string         `json:"origin,omitempty"`
}

type Queue struct {
	Dir string
	mu  sync.Mutex
}

func New(dataDir string) *Queue          { return &Queue{Dir: filepath.Join(dataDir, "domain-operations")} }
func (q *Queue) File(name string) string { return filepath.Join(q.Dir, name) }
func (q *Queue) Ready() bool {
	var marker struct {
		Schema int `json:"schema"`
	}
	return readJSON(q.File("broker.json"), &marker) == nil && marker.Schema == Schema
}

func validInput(i Input) bool {
	if i.Operation == "inspect" || i.Operation == "recover" {
		return i == Input{Operation: i.Operation}
	}
	if !i.Role.Valid() || i.ExpectedRevision != "legacy" && !domaintls.IDPattern.MatchString(i.ExpectedRevision) {
		return false
	}
	if i.Operation == "remove" {
		return i.Role == domaintls.Subscription && i == Input{Operation: "remove", Role: i.Role, ExpectedRevision: i.ExpectedRevision}
	}
	if i.Operation == "renew" {
		return i == Input{Operation: "renew", Role: i.Role, ExpectedRevision: i.ExpectedRevision}
	}
	if i.Operation != "configure" || i.Method != domaintls.Manual && i.Method != domaintls.Automatic && i.Method != domaintls.External {
		return false
	}
	o, e := domaintls.ParseOrigin(i.Origin)
	if e != nil || o.URL != i.Origin || len(i.Email) > 254 {
		return false
	}
	if i.Method == domaintls.Manual {
		return i.Email == "" && i.Challenge == "" && (i.StageID != "" && domaintls.IDPattern.MatchString(i.StageID) && i.CertSource == "" && i.KeySource == "" || i.StageID == "" && filepath.IsAbs(i.CertSource) && filepath.IsAbs(i.KeySource) && len(i.CertSource) < 512 && len(i.KeySource) < 512)
	}
	if i.StageID != "" || i.CertSource != "" || i.KeySource != "" {
		return false
	}
	return i.Method == domaintls.External && i.Challenge == "" && i.Email == "" || i.Method == domaintls.Automatic && (i.Challenge == "http" || i.Challenge == "cloudflare")
}

func validRequest(r Request) bool {
	return r.Schema == Schema && domaintls.IDPattern.MatchString(r.ID) && len(r.ActorID) > 0 && len(r.ActorID) <= 128 && !r.CreatedAt.IsZero() && validInput(r.Input)
}

func readJSON(file string, value any) error {
	info, e := os.Lstat(file)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 16<<10 {
		return ErrInvalid
	}
	f, e := os.Open(file)
	if e != nil {
		return e
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, (16<<10)+1))
	if e != nil || len(raw) > 16<<10 {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	return nil
}

func writeJSON(file string, value any, replace bool) error {
	raw, e := json.Marshal(value)
	if e != nil || len(raw) > 16<<10 {
		return ErrInvalid
	}
	f, e := os.CreateTemp(filepath.Dir(file), ".domain-record-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(raw)
	e = errors.Join(e, f.Sync(), f.Close())
	if e != nil {
		return e
	}
	if replace {
		return os.Rename(f.Name(), file)
	}
	// Publish complete bytes without replacing an existing request. PathExists
	// can wake the host runner immediately; it must never see a partial write.
	if e := os.Link(f.Name(), file); os.IsExist(e) {
		return ErrBusy
	} else {
		return e
	}
}

func (q *Queue) Enqueue(input Input, actorID string) (Status, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.Ready() {
		return Status{}, ErrUnavailable
	}
	if !validInput(input) {
		return Status{}, ErrInvalid
	}
	if _, e := os.Stat(q.File("running.json")); e == nil {
		return Status{}, ErrBusy
	}
	var nonce [16]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		return Status{}, e
	}
	r := Request{Schema: Schema, ID: hex.EncodeToString(nonce[:]), ActorID: actorID, CreatedAt: time.Now().UTC(), Input: input}
	if !validRequest(r) {
		return Status{}, ErrInvalid
	}
	if e := writeJSON(q.File("request.json"), r, false); e != nil {
		return Status{}, e
	}
	s := Status{Schema: Schema, ID: r.ID, State: "queued", Stage: "requested", UpdatedAt: r.CreatedAt, Role: r.Role, Origin: r.Origin}
	return s, nil
}

func (q *Queue) Claim() (Request, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var r Request
	if _, e := os.Stat(q.File("running.json")); e == nil {
		return r, ErrBusy
	}
	if e := readJSON(q.File("request.json"), &r); e != nil {
		return r, e
	}
	if !validRequest(r) || time.Since(r.CreatedAt) > time.Hour || time.Until(r.CreatedAt) > time.Minute {
		return r, ErrInvalid
	}
	if e := os.Link(q.File("request.json"), q.File("running.json")); os.IsExist(e) {
		return r, ErrBusy
	} else if e != nil {
		return r, e
	}
	if e := os.Remove(q.File("request.json")); e != nil {
		return r, e
	}
	return r, q.Record(r, "running", "requested", "")
}

func validStatus(s Status) bool {
	if s.Role != "" && !s.Role.Valid() {
		return false
	}
	if s.Origin != "" {
		o, e := domaintls.ParseOrigin(s.Origin)
		if e != nil || o.URL != s.Origin || !s.Role.Valid() {
			return false
		}
	}
	return s.Schema == Schema && domaintls.IDPattern.MatchString(s.ID) && !s.UpdatedAt.IsZero() &&
		(s.State == "queued" || s.State == "running" || s.State == "succeeded" || s.State == "failed") &&
		(s.Stage == "requested" || s.Stage == "validating" || s.Stage == "issuing" || s.Stage == "issued" || s.Stage == "activating" || s.Stage == "verified" || s.Stage == "recovery") &&
		(s.Failure == "" || s.Failure == "authorization_revoked" || s.Failure == "invalid_request" || s.Failure == "operation_failed" || s.Failure == "interrupted")
}

func (q *Queue) Record(r Request, state, stage, failure string) error {
	s := Status{Schema: Schema, ID: r.ID, State: state, Stage: stage, Failure: failure, UpdatedAt: time.Now().UTC(), Role: r.Role, Origin: r.Origin}
	if !validRequest(r) || !validStatus(s) || state == "queued" {
		return ErrInvalid
	}
	return writeJSON(q.File("status.json"), s, true)
}

func (q *Queue) Finish(r Request, err error) error {
	state, stage, failure := "succeeded", "verified", ""
	if err != nil {
		state, failure = "failed", "operation_failed"
		var old Status
		if readJSON(q.File("status.json"), &old) == nil && validStatus(old) && old.ID == r.ID {
			stage = old.Stage
			if old.Failure != "" {
				failure = old.Failure
			}
		}
	}
	if e := q.Record(r, state, stage, failure); e != nil {
		return e
	}
	return os.Remove(q.File("running.json"))
}

func (q *Queue) Status() (Status, error) {
	var s Status
	var r Request
	if readJSON(q.File("request.json"), &r) == nil && validRequest(r) {
		return Status{Schema: Schema, ID: r.ID, State: "queued", Stage: "requested", UpdatedAt: r.CreatedAt, Role: r.Role, Origin: r.Origin}, nil
	}
	if e := readJSON(q.File("status.json"), &s); e != nil {
		return s, e
	}
	if !validStatus(s) {
		return Status{}, ErrInvalid
	}
	return s, nil
}

func (q *Queue) Stage(cert, key []byte) (string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(cert) == 0 || len(key) == 0 || len(cert) > domaintls.MaxMaterialBytes || len(key) > domaintls.MaxMaterialBytes {
		return "", ErrInvalid
	}
	base := q.File("imports")
	if e := os.MkdirAll(base, 0700); e != nil {
		return "", e
	}
	if info, e := os.Lstat(base); e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrInvalid
	}
	directory, e := os.Open(base)
	if e != nil {
		return "", e
	}
	entries, e := directory.ReadDir(5)
	_ = directory.Close()
	if e != nil && !errors.Is(e, io.EOF) {
		return "", e
	}
	if len(entries) > 4 {
		return "", ErrBusy
	}
	// Interrupted uploads are bounded and expire without touching queued/running
	// imports. Normal completion removes its own private staging immediately.
	kept := 0
	for _, entry := range entries {
		if !domaintls.IDPattern.MatchString(entry.Name()) || !entry.IsDir() {
			return "", ErrInvalid
		}
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		if time.Since(info.ModTime()) > time.Hour {
			protected := false
			for _, name := range []string{"request.json", "running.json"} {
				var r Request
				if readJSON(q.File(name), &r) == nil && r.StageID == entry.Name() {
					protected = true
				}
			}
			if !protected {
				if err := q.RemoveImport(entry.Name()); err != nil {
					return "", err
				}
				continue
			}
		}
		kept++
	}
	if kept >= 4 {
		return "", ErrBusy
	}
	var nonce [16]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		return "", e
	}
	id := hex.EncodeToString(nonce[:])
	dir := filepath.Join(base, id)
	if e := os.Mkdir(dir, 0700); e != nil {
		return "", e
	}
	if e := os.WriteFile(filepath.Join(dir, "fullchain.pem"), cert, 0600); e != nil {
		_ = os.RemoveAll(dir)
		return "", e
	}
	if e := os.WriteFile(filepath.Join(dir, "privkey.pem"), key, 0600); e != nil {
		_ = os.RemoveAll(dir)
		return "", e
	}
	return id, nil
}

func (q *Queue) ImportFiles(id string) (string, string, error) {
	if !domaintls.IDPattern.MatchString(id) {
		return "", "", ErrInvalid
	}
	dir := filepath.Join(q.File("imports"), id)
	return filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem"), nil
}
func (q *Queue) RemoveImport(id string) error {
	cert, _, e := q.ImportFiles(id)
	if e != nil {
		return e
	}
	return os.RemoveAll(filepath.Dir(cert))
}

// Reports are fixed host view models; private staged material never enters them.
func (q *Queue) ReadInventory(value any) error { return readJSON(q.File("inventory.json"), value) }
func (q *Queue) HasRequest() bool              { _, e := os.Stat(q.File("request.json")); return e == nil }
func (q *Queue) WriteInventory(value any) error {
	return writeJSON(q.File("inventory.json"), value, true)
}

// RecoverInterrupted is called only after the host runner's cross-process lock.
// An interrupted queue never implies retrying destructive work automatically.
func (q *Queue) RecoverInterrupted() error {
	var r Request
	if _, e := os.Stat(q.File("running.json")); os.IsNotExist(e) {
		return nil
	}
	if e := readJSON(q.File("running.json"), &r); e != nil || !validRequest(r) {
		return ErrInvalid
	}
	if e := q.Record(r, "failed", "recovery", "interrupted"); e != nil {
		return e
	}
	return os.Remove(q.File("running.json"))
}
