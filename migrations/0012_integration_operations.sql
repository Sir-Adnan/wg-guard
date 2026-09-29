-- Recoverable automation outcomes. Only a hash of the caller's key and a
-- non-secret result are stored; the capability link and device keys are not.
CREATE TABLE integration_operations (
    id           TEXT PRIMARY KEY,
    scope_key    TEXT NOT NULL,
    key_hash     TEXT NOT NULL UNIQUE,
    kind         TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    result_json  TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL
);
CREATE INDEX idx_integration_operations_expiry ON integration_operations(expires_at);
