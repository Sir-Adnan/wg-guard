-- Preserve AmneziaWG scalar-or-range values without changing the legacy
-- scalar columns needed by a rollback binary. Empty text is the canonical
-- unset representation; enabled legacy scalar values become decimal text.
ALTER TABLE tunnel_interfaces ADD COLUMN h1_range TEXT NOT NULL DEFAULT '';
ALTER TABLE tunnel_interfaces ADD COLUMN h2_range TEXT NOT NULL DEFAULT '';
ALTER TABLE tunnel_interfaces ADD COLUMN h3_range TEXT NOT NULL DEFAULT '';
ALTER TABLE tunnel_interfaces ADD COLUMN h4_range TEXT NOT NULL DEFAULT '';

UPDATE tunnel_interfaces SET
    h1_range = CASE WHEN h1 IS NULL THEN '' ELSE CAST(h1 AS TEXT) END,
    h2_range = CASE WHEN h2 IS NULL THEN '' ELSE CAST(h2 AS TEXT) END,
    h3_range = CASE WHEN h3 IS NULL THEN '' ELSE CAST(h3 AS TEXT) END,
    h4_range = CASE WHEN h4 IS NULL THEN '' ELSE CAST(h4 AS TEXT) END;

-- Keep the old integer setting for rollback compatibility. An operator who
-- has already written the new range-aware key always wins.
INSERT INTO settings (key, value, updated_at)
SELECT 'network.client_persistent_keepalive', value, updated_at
FROM settings
WHERE key = 'network.client_keepalive_seconds'
  AND NOT EXISTS (
      SELECT 1 FROM settings WHERE key = 'network.client_persistent_keepalive'
  );
