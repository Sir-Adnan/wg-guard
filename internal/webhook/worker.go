package webhook

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
)

// Worker delivers due webhook deliveries. One pass selects due rows (indexed
// by status,next_attempt_at), delivers them with capped concurrency, and
// records attempts. One broken endpoint can never block the process: every
// request has a timeout, concurrency is capped, and failures only touch the
// delivery row (webhooks.md). The pass runs as a scheduler job (every few
// seconds); a restart simply re-delivers — receivers deduplicate by event id.
type Worker struct {
	db   *database.DB
	ring *secrets.KeyRing
	reg  *settings.Registry
	log  *slog.Logger
	now  func() time.Time

	// HTTP client shared across deliveries: bounded connection pool, one
	// hard timeout per request.
	client *http.Client
}

const (
	// maxBatch bounds one pass. At the default scheduler cadence (5 s) this
	// sustains ~12 deliveries/s without queuing pressure on SQLite.
	maxBatch = 64
	// maxConcurrency caps parallel in-flight POSTs per pass.
	maxConcurrency = 4
	// requestTimeout bounds a single delivery attempt.
	requestTimeout = 10 * time.Second
	// maxErrorLen keeps last_error compact (status lines only — response
	// bodies are never stored: a receiver's echo could contain anything).
	maxErrorLen = 200
	// backoffBase/backoffCap shape the exponential retry curve:
	// 30s, 1m, 2m, 4m, 8m, 16m, 32m, 1h, 2h, 4h, 6h(capped)…
	backoffBase = 30 * time.Second
	backoffCap  = 6 * time.Hour
)

// Report summarizes one delivery pass.
type Report struct {
	Attempted int
	Delivered int
	Failed    int
	Dead      int
}

// NewWorker wires the delivery worker (max attempts read live from
// `webhooks.max_attempts`, node identity from `node.id`).
func NewWorker(db *database.DB, ring *secrets.KeyRing, reg *settings.Registry, log *slog.Logger) *Worker {
	if log == nil {
		log = slog.Default()
	}
	return &Worker{
		db:   db,
		ring: ring,
		reg:  reg,
		log:  log,
		now:  time.Now,
		client: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        maxConcurrency * 2,
				MaxIdleConnsPerHost: maxConcurrency,
				IdleConnTimeout:     60 * time.Second,
			},
		},
	}
}

// due is one delivery row selected for this pass.
type due struct {
	id           string
	endpointID   string
	endpointURL  string
	eventID      string
	eventType    string
	payload      string
	eventCreated time.Time
	attempts     int
}

// Pass performs one delivery pass and returns what it did.
func (w *Worker) Pass(ctx context.Context) (Report, error) {
	rep := Report{}
	now := w.now().UTC()

	maxAttempts := 12
	var nodeID string
	if w.reg != nil {
		if n, err := w.reg.GetInt(ctx, "webhooks.max_attempts"); err == nil && n > 0 {
			maxAttempts = n
		}
		nodeID, _ = w.reg.GetString(ctx, "node.id")
	}

	var dues []due
	err := w.db.WithTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT d.id, d.endpoint_id, e.url, d.event_id, d.event_type,
				d.payload, d.attempts, ev.created_at
			FROM webhook_deliveries d
			JOIN webhook_endpoints e ON e.id = d.endpoint_id
			JOIN webhook_events ev ON ev.id = d.event_id
			WHERE d.status = 'pending' AND e.enabled = 1
			  AND (d.next_attempt_at IS NULL OR d.next_attempt_at <= ?)
			ORDER BY d.next_attempt_at LIMIT ?`, now.Format(time.RFC3339Nano), maxBatch)
		if err != nil {
			return fmt.Errorf("webhook: select due: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				d       due
				created string
			)
			if err := rows.Scan(&d.id, &d.endpointID, &d.endpointURL, &d.eventID, &d.eventType,
				&d.payload, &d.attempts, &created); err != nil {
				return fmt.Errorf("webhook: scan due: %w", err)
			}
			d.eventCreated, _ = time.Parse(time.RFC3339Nano, created)
			dues = append(dues, d)
		}
		return rows.Err()
	})
	if err != nil {
		return rep, err
	}
	if len(dues) == 0 {
		return rep, nil
	}

	// Secrets are decrypted per pass for the endpoints involved (small,
	// bounded; secrets never enter logs or the delivery rows).
	secretsByEndpoint := map[string]string{}
	for _, d := range dues {
		if _, ok := secretsByEndpoint[d.endpointID]; ok {
			continue
		}
		var enc []byte
		err := w.db.QueryRowContext(ctx, `SELECT secret_encrypted FROM webhook_endpoints WHERE id = ?`, d.endpointID).Scan(&enc)
		if err != nil {
			continue // endpoint vanished mid-pass: its rows are skipped below
		}
		sec, err := w.ring.DecryptString(string(enc))
		if err != nil {
			w.log.Error("webhook: decrypt secret failed", "endpoint", d.endpointID)
			continue
		}
		secretsByEndpoint[d.endpointID] = sec
	}

	// Deliver with capped concurrency.
	type outcome struct {
		d      due
		ok     bool
		errMsg string
	}
	outcomes := make([]outcome, len(dues))
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	for i, d := range dues {
		sec, ok := secretsByEndpoint[d.endpointID]
		if !ok {
			continue // no secret: left pending, retried next pass
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, d due, sec string) {
			defer wg.Done()
			defer func() { <-sem }()
			ok, msg := w.deliver(ctx, d, sec, nodeID)
			outcomes[i] = outcome{d: d, ok: ok, errMsg: msg}
		}(i, d, sec)
	}
	wg.Wait()

	// Record attempts in one transaction: a crash here re-delivers the
	// already-delivered subset (at-least-once; receivers dedupe by event id).
	err = w.db.WithTx(ctx, func(tx *sql.Tx) error {
		for _, o := range outcomes {
			if o.d.id == "" {
				continue
			}
			rep.Attempted++
			next := now.Add(backoff(o.d.attempts + 1)).Format(time.RFC3339Nano)
			status := "pending"
			attempts := o.d.attempts + 1
			if o.ok {
				rep.Delivered++
				status, next = "delivered", now.Format(time.RFC3339Nano)
			} else if attempts >= maxAttempts {
				rep.Dead++
				status = "dead"
				w.log.Warn("webhook: delivery dead-lettered",
					"endpoint", o.d.endpointID, "event", o.d.eventType, "attempts", attempts)
			} else {
				rep.Failed++
			}
			if _, err := tx.ExecContext(ctx, `UPDATE webhook_deliveries
				SET status = ?, attempts = ?, next_attempt_at = ?, last_error = ?, updated_at = ?
				WHERE id = ?`,
				status, attempts, next, truncate(o.errMsg, maxErrorLen), now.Format(time.RFC3339Nano), o.d.id); err != nil {
				return fmt.Errorf("webhook: record attempt: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return rep, err
	}
	return rep, nil
}

// deliver performs one POST with the signed envelope.
func (w *Worker) deliver(ctx context.Context, d due, secret, nodeID string) (bool, string) {
	body, err := json.Marshal(map[string]any{
		"id":        d.eventID,
		"type":      d.eventType,
		"timestamp": d.eventCreated.UTC().Format(time.RFC3339Nano),
		"node_id":   nodeID,
		"data":      json.RawMessage(d.payload),
	})
	if err != nil {
		return false, "marshal envelope: " + err.Error()
	}
	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, d.endpointURL, bytes.NewReader(body))
	if err != nil {
		return false, "build request: " + err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-WG-Event", d.eventType)
	req.Header.Set("X-WG-Delivery", d.id)
	req.Header.Set("X-WG-Signature", Sign(secret, w.now().Unix(), body))
	req.Header.Set("User-Agent", "wg-guard-webhook/1.0")

	resp, err := w.client.Do(req)
	if err != nil {
		return false, err.Error() // network error text; never the body
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, ""
	}
	return false, "receiver returned HTTP " + strconv.Itoa(resp.StatusCode)
}

// backoff is the exponential retry delay for the Nth attempt (1-based).
func backoff(n int) time.Duration {
	d := backoffBase
	for i := 1; i < n; i++ {
		d *= 2
		if d >= backoffCap {
			return backoffCap
		}
	}
	return d
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
