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
		{"publicsub.ui.copy_failed", "Copy failed. Select the text and copy it manually.", "کپی انجام نشد. متن را انتخاب و دستی کپی کنید."},
		{"publicsub.attention.data_low", "Your data is almost used up", "حجم شما رو به اتمام است"},
		{"publicsub.attention.time_low", "Your subscription ends soon", "اشتراک شما به‌زودی تمام می‌شود"},
		{"publicsub.attention.both_low", "Your data and time are almost used up", "حجم و زمان اشتراک شما رو به اتمام است"},
		{"publicsub.attention.hint", "You can still connect. Ask your provider to renew before it runs out.", "هنوز می‌توانید وصل شوید. پیش از تمام شدن، برای تمدید با ارائه‌دهنده تماس بگیرید."},
		{"error.status_label", "Error %d", "خطای %d"},
	}
	for _, row := range rows {
		catalogEN[row[0]] = row[1]
		catalogFA[row[0]] = row[2]
	}
}
