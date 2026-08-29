-- 0001_init: full schema per docs/architecture/database.md (the contract).
-- All timestamps are UTC, stored as RFC3339 TEXT. IDs are UUIDv7 TEXT.
-- Nullable obfuscation params mean "not set / omit from config" (plain WG).

CREATE TABLE tunnel_interfaces (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL UNIQUE,          -- awgN
    listen_port       INTEGER NOT NULL UNIQUE,
    ipv4_subnet       TEXT NOT NULL,                 -- CIDR
    mtu               INTEGER NOT NULL,
    public_key        TEXT NOT NULL,                 -- base64 (server key for client configs)
    private_key_encrypted BLOB NOT NULL,             -- AES-GCM under the master key
    jc                INTEGER,
    jmin              INTEGER,
    jmax              INTEGER,
    s1                INTEGER,
    s2                INTEGER,
    h1                INTEGER,
    h2                INTEGER,
    h3                INTEGER,
    h4                INTEGER,
    i1                TEXT,
    i2                TEXT,
    i3                TEXT,
    i4                TEXT,
    i5                TEXT,
    preset_name       TEXT NOT NULL DEFAULT '',
    enabled           INTEGER NOT NULL DEFAULT 1,
    backend_mode      TEXT NOT NULL DEFAULT 'kernel',
    endpoint_override TEXT,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    CHECK (backend_mode IN ('kernel', 'userspace'))
);

CREATE TABLE plans (
    id                  TEXT PRIMARY KEY,
    name                TEXT NOT NULL UNIQUE,
    traffic_limit_bytes INTEGER,
    duration_seconds    INTEGER,
    start_policy        TEXT NOT NULL DEFAULT 'immediate',
    device_limit        INTEGER,
    speed_limit_kbps    INTEGER,
    interface_id        TEXT REFERENCES tunnel_interfaces(id) ON DELETE SET NULL,
    enabled             INTEGER NOT NULL DEFAULT 1,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    CHECK (start_policy IN ('immediate', 'first_connection'))
);

CREATE TABLE users (
    id                  TEXT PRIMARY KEY,
    username            TEXT NOT NULL UNIQUE,
    display_name        TEXT NOT NULL DEFAULT '',
    note                TEXT NOT NULL DEFAULT '',
    tags                TEXT NOT NULL DEFAULT '[]',
    status              TEXT NOT NULL DEFAULT 'active',
    disable_reason      TEXT,
    traffic_limit_bytes INTEGER,
    traffic_used_rx     INTEGER NOT NULL DEFAULT 0,
    traffic_used_tx     INTEGER NOT NULL DEFAULT 0,
    speed_limit_kbps    INTEGER,
    device_limit        INTEGER,
    plan_id             TEXT REFERENCES plans(id) ON DELETE SET NULL,
    interface_id        TEXT REFERENCES tunnel_interfaces(id) ON DELETE SET NULL,
    start_policy        TEXT NOT NULL DEFAULT 'immediate',
    duration_seconds    INTEGER,
    activated_at        TEXT,
    expires_at          TEXT,
    last_activity_at    TEXT,
    enabled             INTEGER NOT NULL DEFAULT 1,
    metadata            TEXT NOT NULL DEFAULT '{}',
    deleted_at          TEXT,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    CHECK (start_policy IN ('immediate', 'first_connection'))
);

CREATE INDEX idx_users_status ON users(status);
CREATE INDEX idx_users_plan ON users(plan_id);

CREATE TABLE devices (
    id                     TEXT PRIMARY KEY,
    user_id                TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    interface_id           TEXT NOT NULL REFERENCES tunnel_interfaces(id),
    name                   TEXT NOT NULL,
    ipv4_address           TEXT NOT NULL,
    public_key             TEXT NOT NULL UNIQUE,
    private_key_encrypted  BLOB NOT NULL,
    preshared_key_encrypted BLOB,
    enabled                INTEGER NOT NULL DEFAULT 1,
    last_handshake_at      TEXT,
    last_endpoint          TEXT,
    rx_bytes               INTEGER NOT NULL DEFAULT 0,
    tx_bytes               INTEGER NOT NULL DEFAULT 0,
    last_rx                INTEGER NOT NULL DEFAULT 0,
    last_tx                INTEGER NOT NULL DEFAULT 0,
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL,
    UNIQUE (interface_id, ipv4_address),
    UNIQUE (user_id, name)
);

CREATE INDEX idx_devices_user ON devices(user_id);

CREATE TABLE admins (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'admin',
    permissions   TEXT NOT NULL DEFAULT '[]',        -- JSON; owner implicitly all
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    CHECK (role IN ('owner', 'admin'))
);

CREATE TABLE admin_sessions (
    id           TEXT PRIMARY KEY,
    admin_id     TEXT NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    created_at   TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    source_ip    TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_admin_sessions_admin ON admin_sessions(admin_id);

CREATE TABLE api_tokens (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    prefix         TEXT NOT NULL UNIQUE,
    token_hash     TEXT NOT NULL UNIQUE,
    scopes         TEXT NOT NULL DEFAULT '[]',
    expires_at     TEXT,
    enabled        INTEGER NOT NULL DEFAULT 1,
    cidr_allowlist TEXT NOT NULL DEFAULT '',
    last_used_at   TEXT,
    created_at     TEXT NOT NULL
);

CREATE TABLE webhook_endpoints (
    id               TEXT PRIMARY KEY,
    url              TEXT NOT NULL,
    secret_encrypted BLOB NOT NULL,
    enabled          INTEGER NOT NULL DEFAULT 1,
    events           TEXT NOT NULL DEFAULT '[]',
    created_at       TEXT NOT NULL
);

CREATE TABLE webhook_events (
    id         TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    payload    TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE webhook_deliveries (
    id             TEXT PRIMARY KEY,
    endpoint_id    TEXT NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    event_id       TEXT NOT NULL REFERENCES webhook_events(id) ON DELETE CASCADE,
    event_type     TEXT NOT NULL,
    payload        TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'pending',
    attempts       INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT,
    last_error     TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    CHECK (status IN ('pending', 'delivered', 'dead'))
);

CREATE INDEX idx_webhook_deliveries_due ON webhook_deliveries(status, next_attempt_at);

CREATE TABLE audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    ts         TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id   TEXT NOT NULL DEFAULT '',
    action     TEXT NOT NULL,
    target     TEXT NOT NULL DEFAULT '',
    source_ip  TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '',
    metadata   TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_audit_ts ON audit_log(ts);

CREATE TABLE idempotency_keys (
    key               TEXT PRIMARY KEY,
    request_hash      TEXT NOT NULL,
    response_snapshot TEXT NOT NULL,
    expires_at        TEXT NOT NULL
);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE backup_schedules (
    id             TEXT PRIMARY KEY,
    mode           TEXT NOT NULL,                    -- daily | interval | weekly
    time_of_day    TEXT NOT NULL DEFAULT '03:00',
    interval_hours INTEGER NOT NULL DEFAULT 24,
    weekday        INTEGER NOT NULL DEFAULT 0,
    retention_count INTEGER NOT NULL DEFAULT 14,
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);

CREATE TABLE traffic_samples (
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    ts        TEXT NOT NULL,
    rx_delta  INTEGER NOT NULL,
    tx_delta  INTEGER NOT NULL,
    PRIMARY KEY (device_id, ts)
);

CREATE INDEX idx_traffic_samples_ts ON traffic_samples(ts);

CREATE TABLE traffic_rollups (
    device_id    TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    bucket_start TEXT NOT NULL,
    granularity  TEXT NOT NULL,                      -- hourly | daily
    rx           INTEGER NOT NULL,
    tx           INTEGER NOT NULL,
    PRIMARY KEY (device_id, granularity, bucket_start)
);
