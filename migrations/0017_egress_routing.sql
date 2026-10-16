-- Egress routing (docs/architecture/egress-routing.md): alternate exits for
-- client traffic, optional outbound groups and ordered first-match rules.
-- Kernel state (links, policy routes, nftables marks) is rendered from these
-- rows; nothing here is consulted on the per-packet path.

-- An outbound is one alternate exit. slot is the stable small integer that
-- derives its link name (wgo<slot>), routing table and packet mark.
CREATE TABLE egress_outbounds (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL UNIQUE,
    kind             TEXT NOT NULL CHECK (kind IN ('warp', 'wireguard', 'source_ip')),
    slot             INTEGER NOT NULL UNIQUE CHECK (slot BETWEEN 1 AND 63),
    enabled          INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    -- Non-secret settings (address, peer, endpoint, MTU, obfuscation, WARP
    -- account metadata or source address) as one versioned JSON object.
    config_json      TEXT NOT NULL DEFAULT '{}',
    -- Private key, preshared key and WARP credentials, sealed by the master key.
    secret_encrypted BLOB,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

-- A balancer groups outbounds. Rules may target it instead of one outbound.
CREATE TABLE egress_balancers (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    strategy     TEXT NOT NULL CHECK (strategy IN ('sticky', 'round_robin', 'random', 'least_conn', 'failover')),
    fallback     TEXT NOT NULL DEFAULT 'direct' CHECK (fallback IN ('direct', 'block')),
    members_json TEXT NOT NULL DEFAULT '[]',
    enabled      INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

-- Ordered rules: the first enabled rule whose matchers match decides the exit.
CREATE TABLE egress_rules (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    position     INTEGER NOT NULL,
    enabled      INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    target_kind  TEXT NOT NULL CHECK (target_kind IN ('direct', 'block', 'outbound', 'balancer')),
    target_id    TEXT NOT NULL DEFAULT '',
    presets_json TEXT NOT NULL DEFAULT '[]',
    matchers     TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);
CREATE INDEX idx_egress_rules_position ON egress_rules(position);

-- Installation-wide routing options (one row).
CREATE TABLE egress_settings (
    id                  INTEGER PRIMARY KEY CHECK (id = 1),
    default_target_kind TEXT NOT NULL DEFAULT 'direct'
        CHECK (default_target_kind IN ('direct', 'outbound', 'balancer')),
    default_target_id   TEXT NOT NULL DEFAULT '',
    block_encrypted_dns INTEGER NOT NULL DEFAULT 1 CHECK (block_encrypted_dns IN (0, 1)),
    geosite_url         TEXT NOT NULL DEFAULT '',
    geoip_url           TEXT NOT NULL DEFAULT '',
    updated_at          TEXT NOT NULL DEFAULT ''
);
INSERT INTO egress_settings (id) VALUES (1);
