-- Per-user subscription links (Phase 5 refinement). One row per user; the
-- token is stored twice: AES-GCM encrypted (panel re-display, same envelope
-- as device keys) and SHA-256 hashed (unique lookup index — the plaintext is
-- never stored in the clear and never logged). Lifecycle: created on user
-- creation, regenerated on demand (the old token dies because the hash is
-- replaced), revoked/restored via revoked_at.
CREATE TABLE sub_links (
    user_id         TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    token_encrypted BLOB NOT NULL,
    token_hash      TEXT NOT NULL UNIQUE,
    created_at      TEXT NOT NULL,
    rotated_at      TEXT,
    revoked_at      TEXT
);
