package i18n

// Users workspace chrome (list toolbar, create sheet, account detail).
// Rows are {key, English, Persian}; parity is enforced by the catalog tests.
func init() {
	rows := [][3]string{
		{"users.ui.act_link", "Link", "لینک ساب"},
		{"users.ui.act_qr", "QR", "QR"},
		{"users.ui.act_config", "Config", "کانفیگ"},
		{"users.ui.count_unit", "users", "کاربر"},
		{"users.ui.of_limit", "of %s", "از %s"},
		{"users.ui.terms", "Subscription terms", "شرایط اشتراک"},
		{"users.ui.terms_hint", "Enter limits yourself or apply a saved template.", "محدودیت‌ها را خودتان وارد کنید یا یک قالب آماده را اعمال کنید."},
		{"users.ui.create_hint", "Create an account with its limits and first config.", "حسابی با محدودیت‌ها و نخستین کانفیگ آن بسازید."},
		{"users.ui.more_actions", "More actions", "کارهای بیشتر"},
		{"users.ui.device_actions", "Device actions", "کارهای دستگاه"},
		{"users.ui.created_on", "Created %s", "ساخته‌شده در %s"},
		{"users.ui.filters", "Filters", "فیلترها"},
		{"users.ui.no_expiry", "No expiry", "بدون انقضا"},
		{"users.ui.selection_help", "Selection help", "راهنمای انتخاب"},
	}
	for _, row := range rows {
		catalogEN[row[0]] = row[1]
		catalogFA[row[0]] = row[2]
	}
}
