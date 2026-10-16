package i18n

// Public subscription page and access/error surfaces of the design-system
// rewrite. Rows are {key, English, Persian}; parity is enforced by the
// catalog tests.
func init() {
	rows := [][3]string{
		{"publicsub.ui.theme_picker", "Page style", "سبک صفحه"},
		{"publicsub.ui.theme_default", "Default", "پیش‌فرض"},
		{"publicsub.ui.group.wg-guard", "Built-in", "پیش‌ساخته"},
		{"publicsub.ui.group.shadcn", "shadcn/ui", "shadcn/ui"},
		{"publicsub.ui.group.tweakcn", "Community", "طرح‌های جامعه"},
		{"publicsub.ui.remaining_data", "Data left", "حجم باقی‌مانده"},
		{"publicsub.ui.copy_key", "Copy key", "کپی کلید"},
		{"publicsub.ui.no_devices", "No devices yet", "هنوز دستگاهی ندارید"},
		{"publicsub.ui.choose_platform", "Choose your platform", "سیستم‌عامل خود را انتخاب کنید"},
		{"error.status_label", "Error %d", "خطای %d"},
	}
	for _, row := range rows {
		catalogEN[row[0]] = row[1]
		catalogFA[row[0]] = row[2]
	}
}
