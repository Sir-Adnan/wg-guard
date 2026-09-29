-- Phase 14 ownership carriers. NULL means the existing node-wide operator
-- namespace; reseller identities are not exposed until every route is gated.
CREATE TABLE resellers (
    id           TEXT PRIMARY KEY,
    slug         TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL DEFAULT '',
    permissions  TEXT NOT NULL DEFAULT '[]',
    enabled      INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

ALTER TABLE admins ADD COLUMN reseller_id TEXT REFERENCES resellers(id) ON DELETE RESTRICT;
ALTER TABLE users ADD COLUMN reseller_id TEXT REFERENCES resellers(id) ON DELETE RESTRICT;
ALTER TABLE api_tokens ADD COLUMN reseller_id TEXT REFERENCES resellers(id) ON DELETE RESTRICT;
ALTER TABLE api_tokens ADD COLUMN issued_by_admin_id TEXT REFERENCES admins(id) ON DELETE SET NULL;
ALTER TABLE webhook_endpoints ADD COLUMN reseller_id TEXT REFERENCES resellers(id) ON DELETE RESTRICT;
ALTER TABLE webhook_events ADD COLUMN reseller_id TEXT REFERENCES resellers(id) ON DELETE RESTRICT;

CREATE INDEX idx_admins_reseller ON admins(reseller_id);
CREATE INDEX idx_users_reseller_created ON users(reseller_id, created_at, id);
CREATE INDEX idx_api_tokens_reseller ON api_tokens(reseller_id);
CREATE INDEX idx_webhook_endpoints_reseller ON webhook_endpoints(reseller_id);
CREATE INDEX idx_webhook_events_reseller ON webhook_events(reseller_id, created_at);
