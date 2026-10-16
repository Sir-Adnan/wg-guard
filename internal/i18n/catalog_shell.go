package i18n

// App shell, command palette and design-system chrome. Rows are
// {key, English, Persian}; parity is enforced by the catalog tests.
func init() {
	rows := [][3]string{
		{"shell.search", "Search…", "جستجو…"},
		{"shell.search_label", "Search pages and actions", "جستجوی صفحه‌ها و کارها"},
		{"shell.account", "Account", "حساب کاربری"},
		{"shell.toggle_sidebar", "Toggle sidebar", "باز و بسته کردن نوار کناری"},
		{"shell.node", "Node", "نود"},
		{"command.placeholder", "Type a page, a user or an action…", "نام صفحه، کاربر یا کاری را بنویسید…"},
		{"command.empty", "Nothing matches.", "موردی پیدا نشد."},
		{"command.pages", "Pages", "صفحه‌ها"},
		{"command.actions", "Actions", "کارها"},
		{"command.find_user", "Find user “%s”", "جستجوی کاربر «%s»"},
		{"command.new_user", "Create a user", "ساخت کاربر جدید"},
		{"command.new_rule", "Add a routing rule", "افزودن قانون مسیریابی"},
		{"command.new_outbound", "Add an outbound", "افزودن خروجی"},
		{"command.backup_now", "Back up now", "پشتیبان‌گیری الان"},
		{"command.theme_light", "Switch to light mode", "تغییر به حالت روشن"},
		{"command.theme_dark", "Switch to dark mode", "تغییر به حالت تیره"},
		{"command.navigate_hint", "to move", "برای جابه‌جایی"},
		{"command.open_hint", "to open", "برای باز کردن"},
		{"command.close_hint", "to close", "برای بستن"},
	}
	for _, row := range rows {
		catalogEN[row[0]] = row[1]
		catalogFA[row[0]] = row[2]
	}
}
