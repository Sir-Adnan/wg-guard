-- The public subscription page may use its own visual preset and mode.
-- Blank values follow the panel default, which is the previous behavior.
ALTER TABLE appearance_defaults ADD COLUMN sub_preset_id TEXT NOT NULL DEFAULT '';
ALTER TABLE appearance_defaults ADD COLUMN sub_mode TEXT NOT NULL DEFAULT ''
    CHECK (sub_mode IN ('', 'light', 'dark', 'system'));
