-- One explicit, prepaid/authorized successor per customer. Terms are copied
-- when queued so later catalog edits cannot silently change an entitlement.
CREATE TABLE next_plan_queue (
    user_id              TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    plan_id              TEXT NOT NULL REFERENCES plans(id) ON DELETE RESTRICT,
    source_plan_id       TEXT,
    terms_json           TEXT NOT NULL,
    carry_unused_traffic INTEGER NOT NULL DEFAULT 0 CHECK (carry_unused_traffic IN (0, 1)),
    state                TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'needs_review')),
    review_reason        TEXT NOT NULL DEFAULT '',
    principal_scope      TEXT NOT NULL,
    created_at           TEXT NOT NULL,
    updated_at           TEXT NOT NULL
);
CREATE INDEX idx_next_plan_queue_state ON next_plan_queue(state, created_at);

CREATE TABLE next_plan_activations (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_id         TEXT NOT NULL,
    trigger_kind    TEXT NOT NULL CHECK (trigger_kind IN ('time', 'quota')),
    previous_json   TEXT NOT NULL,
    applied_json    TEXT NOT NULL,
    activated_at    TEXT NOT NULL
);
CREATE INDEX idx_next_plan_activations_user ON next_plan_activations(user_id, activated_at DESC);
CREATE INDEX idx_next_plan_activations_time ON next_plan_activations(activated_at);
