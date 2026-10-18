package i18n

// Operations workspace strings (backups, cleanup, audit, updates, system)
// added by the design-system rewrite. Rows are {key, English, Persian};
// parity is enforced by the catalog tests.
func init() {
	rows := [][3]string{
		{"admins.error.grant_exceeds", "You can grant only permissions you hold yourself.", "فقط دسترسی‌هایی را می‌توانید بدهید که خودتان دارید."},
		{"audit.col_id", "Event ID", "شناسهٔ رویداد"},
		{"audit.col_request", "Request ID", "شناسهٔ درخواست"},
		{"ops.action.backup.verified", "Backup verified", "صحت پشتیبان بررسی شد"},
		{"ops.action.backup.imported", "Backup imported", "پشتیبان وارد شد"},
	}
	for _, row := range rows {
		catalogEN[row[0]] = row[1]
		catalogFA[row[0]] = row[2]
	}
}
