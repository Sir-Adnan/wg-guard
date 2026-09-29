-- A reseller may provision only products assigned by the node owner. Removing
-- an assignment affects future purchases; existing customer plans remain.
CREATE TABLE reseller_plan_access (
    reseller_id TEXT NOT NULL REFERENCES resellers(id) ON DELETE CASCADE,
    plan_id     TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    PRIMARY KEY (reseller_id, plan_id)
);
CREATE INDEX idx_reseller_plan_access_plan ON reseller_plan_access(plan_id);
