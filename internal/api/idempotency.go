package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// Idempotency (api.md): `Idempotency-Key` on mutating integration endpoints
// (create user, bulk create, renew, traffic mutations) — retries from bots
// and panels never duplicate effects. The row is inserted in its own
// transaction before the handler runs; on success the response snapshot is
// stored, on failure the row is released so the client can retry.
//
// Semantics:
//   - same key + same request hash + stored response → replay it
//     (Idempotency-Replayed: true);
//   - same key, different request → 409 IDEMPOTENCY_KEY_REUSED;
//   - same key while the first request is still in flight → 409 (rare: the
//     window is the handler duration);
//   - no key header → the handler runs normally.
type idempotencyStore struct {
	db *database.DB
}

const idempotencyTTL = 24 * time.Hour

type idemResponse struct {
	Status  int               `json:"status"`
	Body    []byte            `json:"body"`
	Headers map[string]string `json:"headers,omitempty"`
}

// wrap executes h with idempotency guarantees for the request.
func (st *idempotencyStore) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}
		if len(key) > 128 || !isPrintableASCII(key) {
			writeErr(w, r, http.StatusBadRequest, domain.CodeInvalidRequest,
				"Idempotency-Key must be 1-128 printable characters")
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeServiceErr(w, r, err)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		hash := requestHash(r.Method, r.URL.Path, body)

		// Claim the key. The PRIMARY KEY is the arbiter: a concurrent claim
		// fails the insert and resolves below.
		claimed, err := st.claim(r.Context(), key, hash)
		if err != nil {
			writeServiceErr(w, r, err)
			return
		}
		if !claimed {
			// Key exists: replay, in-flight conflict, or request mismatch.
			snapshot, hashMatch, err := st.lookup(r.Context(), key, hash)
			if err != nil {
				writeServiceErr(w, r, err)
				return
			}
			switch {
			case !hashMatch:
				writeErr(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED",
					"Idempotency-Key was already used with a different request")
			case snapshot == nil:
				writeErr(w, r, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED",
					"a request with this Idempotency-Key is still in progress")
			default:
				w.Header().Set("Idempotency-Replayed", "true")
				for k, v := range snapshot.Headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(snapshot.Status)
				_, _ = w.Write(snapshot.Body)
			}
			return
		}

		rec := &statusRecorder{ResponseWriter: w, status: 200, buf: &bytes.Buffer{}}
		next.ServeHTTP(rec, r)

		if rec.status >= 200 && rec.status < 300 {
			_ = st.store(r.Context(), key, idemResponse{Status: rec.status, Body: rec.buf.Bytes()})
		} else {
			_ = st.release(r.Context(), key) // failures are retryable
		}
	})
}

func (st *idempotencyStore) claim(ctx context.Context, key, hash string) (bool, error) {
	_, err := st.db.ExecContext(ctx, `INSERT INTO idempotency_keys
		(key, request_hash, response_snapshot, expires_at) VALUES (?, ?, '', ?)`,
		key, hash, time.Now().UTC().Add(idempotencyTTL).Format(time.RFC3339Nano))
	if err == nil {
		return true, nil
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return false, nil
	}
	return false, err
}

func (st *idempotencyStore) lookup(ctx context.Context, key, hash string) (*idemResponse, bool, error) {
	var (
		storedHash string
		snapshot   string
	)
	err := st.db.QueryRowContext(ctx,
		`SELECT request_hash, response_snapshot FROM idempotency_keys WHERE key = ? AND expires_at > ?`,
		key, time.Now().UTC().Format(time.RFC3339Nano)).Scan(&storedHash, &snapshot)
	if err == sql.ErrNoRows {
		// Expired between claim-failure and lookup: treat as claimable next
		// time; for this request report in-flight conflict.
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if storedHash != hash {
		return nil, false, nil
	}
	if snapshot == "" {
		return nil, true, nil // in flight
	}
	var resp idemResponse
	if err := json.Unmarshal([]byte(snapshot), &resp); err != nil {
		return nil, true, nil // corrupt snapshot: treat as in flight, never crash
	}
	return &resp, true, nil
}

func (st *idempotencyStore) store(ctx context.Context, key string, resp idemResponse) error {
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = st.db.ExecContext(ctx,
		`UPDATE idempotency_keys SET response_snapshot = ? WHERE key = ?`, string(b), key)
	return err
}

func (st *idempotencyStore) release(ctx context.Context, key string) error {
	_, err := st.db.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE key = ?`, key)
	return err
}

// Prune deletes expired idempotency rows (scheduler housekeeping).
func (st *idempotencyStore) Prune(ctx context.Context, now time.Time) (int64, error) {
	res, err := st.db.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE expires_at < ?`,
		now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func requestHash(method, path string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte{0})
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}
