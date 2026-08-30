-- 0003_admin_locale: per-admin panel language preference (Phase 5).
-- fa is the product default (docs/product/requirements.md); the column is
-- read with the session on every request and writable from the panel header.
-- Values are validated in application code ('fa' | 'en').

ALTER TABLE admins ADD COLUMN locale TEXT NOT NULL DEFAULT 'fa';
