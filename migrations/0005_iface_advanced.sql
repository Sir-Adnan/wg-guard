-- 2.0/3.x-generation obfuscation parameters (capability-gated: parser-accepted
-- against the pinned tools v3.1, runtime verification pending the Phase 8 VPS
-- matrix — docs/integrations/amneziawg.md). All default to "unset"; the plain
-- and legacy-1.0 sets are untouched.
ALTER TABLE tunnel_interfaces ADD COLUMN s3 INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tunnel_interfaces ADD COLUMN s4 INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tunnel_interfaces ADD COLUMN header_protection_key TEXT NOT NULL DEFAULT '';
ALTER TABLE tunnel_interfaces ADD COLUMN content_padding_addition TEXT NOT NULL DEFAULT '';
ALTER TABLE tunnel_interfaces ADD COLUMN rekey_after_time TEXT NOT NULL DEFAULT '';
ALTER TABLE tunnel_interfaces ADD COLUMN rekey_timeout TEXT NOT NULL DEFAULT '';
ALTER TABLE tunnel_interfaces ADD COLUMN reject_after_time TEXT NOT NULL DEFAULT '';
ALTER TABLE tunnel_interfaces ADD COLUMN keepalive_timeout TEXT NOT NULL DEFAULT '';
ALTER TABLE tunnel_interfaces ADD COLUMN max_handshake_attempts TEXT NOT NULL DEFAULT '';
ALTER TABLE tunnel_interfaces ADD COLUMN random_trailers INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tunnel_interfaces ADD COLUMN disable_cookies INTEGER NOT NULL DEFAULT 0;
