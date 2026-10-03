-- Keep the primary pool stable; ordered extra pools expand a profile without
-- changing existing client addresses. Runtime rollback must retain this schema.
ALTER TABLE tunnel_interfaces ADD COLUMN ipv4_extra_pools TEXT NOT NULL DEFAULT '[]';
CREATE INDEX idx_devices_interface ON devices(interface_id);
CREATE INDEX idx_users_cleanup ON users(status, expires_at, id);
