-- Existing node-wide receivers remain global-operator-only on upgrade.
-- The owner may explicitly opt a receiver into reseller customer events.
ALTER TABLE webhook_endpoints ADD COLUMN include_reseller_events INTEGER NOT NULL DEFAULT 0
    CHECK (include_reseller_events IN (0, 1));

CREATE INDEX idx_webhook_endpoint_fanout ON webhook_endpoints(enabled, reseller_id, include_reseller_events);
