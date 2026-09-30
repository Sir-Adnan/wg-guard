-- A reseller may provision only products assigned by the node owner. Removing
-- an assignment affects future purchases; existing customer plans remain.
CREATE TABLE reseller_template_access (
    reseller_id TEXT NOT NULL REFERENCES resellers(id) ON DELETE CASCADE,
    template_id     TEXT NOT NULL REFERENCES templates(id) ON DELETE CASCADE,
    PRIMARY KEY (reseller_id, template_id)
);
CREATE INDEX idx_reseller_template_access_plan ON reseller_template_access(template_id);
