-- Number presentation is independent of locale, visual preset and machine data.
ALTER TABLE appearance_defaults ADD COLUMN digits TEXT NOT NULL DEFAULT 'latin'
    CHECK (digits IN ('latin', 'persian'));
ALTER TABLE admins ADD COLUMN appearance_digits TEXT NOT NULL DEFAULT ''
    CHECK (appearance_digits IN ('', 'latin', 'persian'));
