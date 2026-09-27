-- A revoke replaces client keys in the same transaction as the subscription
-- capability. Preserve former public identities until a successful runtime
-- reconciliation confirms they are absent, including across process crashes.
CREATE TABLE retired_peer_keys (
    interface_id TEXT NOT NULL REFERENCES tunnel_interfaces(id) ON DELETE CASCADE,
    public_key   TEXT NOT NULL,
    PRIMARY KEY (interface_id, public_key)
);
