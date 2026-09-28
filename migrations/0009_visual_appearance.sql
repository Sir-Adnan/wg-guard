-- The panel's shared default is independent of per-admin overrides. A blank
-- admin value inherits the installation default; existing accounts keep it.
ALTER TABLE admins ADD COLUMN appearance_preset TEXT NOT NULL DEFAULT '';

CREATE TABLE appearance_defaults (
    id        INTEGER PRIMARY KEY CHECK (id = 1),
    preset_id TEXT NOT NULL DEFAULT 'wg-guard-neutral',
    mode      TEXT NOT NULL DEFAULT 'light' CHECK (mode IN ('light', 'dark', 'system'))
);
INSERT INTO appearance_defaults (id, preset_id, mode)
VALUES (1, 'wg-guard-neutral', 'light');
