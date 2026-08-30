-- 0002_speed_limits: independent upload/download speed limits (Phase 4).
-- The single users.speed_limit_kbps / plans.speed_limit_kbps become two
-- nullable columns per table: download (server→client, tc egress) and upload
-- (client→server, tc ingress via IFB). An existing single limit applies to
-- both directions; NULL stays unlimited. Empty state = unlimited per direction
-- (docs/architecture/api.md §speed limits).
--
-- SQLite ≥ 3.35 DROP COLUMN is used instead of a table rebuild: neither
-- column is indexed or FK-referenced. Both statements run inside the
-- migration transaction.

ALTER TABLE users ADD COLUMN speed_limit_down_kbps INTEGER;
ALTER TABLE users ADD COLUMN speed_limit_up_kbps INTEGER;
UPDATE users SET speed_limit_down_kbps = speed_limit_kbps,
                 speed_limit_up_kbps = speed_limit_kbps
WHERE speed_limit_kbps IS NOT NULL;
ALTER TABLE users DROP COLUMN speed_limit_kbps;

ALTER TABLE plans ADD COLUMN speed_limit_down_kbps INTEGER;
ALTER TABLE plans ADD COLUMN speed_limit_up_kbps INTEGER;
UPDATE plans SET speed_limit_down_kbps = speed_limit_kbps,
                 speed_limit_up_kbps = speed_limit_kbps
WHERE speed_limit_kbps IS NOT NULL;
ALTER TABLE plans DROP COLUMN speed_limit_kbps;
