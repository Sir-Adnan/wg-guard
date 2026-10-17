package i18n

// Appearance workspace: theme gallery, panel and subscription defaults.
// Rows are {key, English, Persian}; parity is enforced by the catalog tests.
func init() {
	rows := [][3]string{
		{"appearance.tab.theme", "Theme", "تم"},
		{"appearance.tab.subscription", "Subscription page", "صفحهٔ اشتراک"},
		{"appearance.filter", "Theme source", "منبع تم"},
		{"appearance.filter_all", "All", "همه"},
		{"appearance.active", "Active", "فعال"},
		{"appearance.previewing", "Previewing", "پیش‌نمایش"},
		{"appearance.unsaved", "Not saved yet. Leaving the page restores your current theme.", "هنوز ذخیره نشده است. با ترک صفحه، تم فعلی برمی‌گردد."},
		{"appearance.ui.compact", "Compact", "فشرده"},
		{"appearance.ui.default", "Standard", "استاندارد"},
		{"appearance.ui.comfortable", "Spacious", "جادار"},
		{"appearance.ui.plain", "flush sidebar", "نوار کناری یکپارچه"},
		{"appearance.ui.inset", "inset content", "محتوای قاب‌دار"},
		{"appearance.ui.floating", "floating sidebar", "نوار کناری شناور"},
		{"appearance.panel_default_title", "Panel default", "پیش‌فرض پنل"},
		{"appearance.panel_default_hint", "Applies the selected theme to everyone without a personal theme, and to the subscription page unless it has its own.", "تم انتخاب‌شده برای همهٔ کسانی که تم شخصی ندارند اعمال می‌شود؛ صفحهٔ اشتراک هم اگر تم جداگانه نداشته باشد از آن پیروی می‌کند."},
		{"appearance.panel_default_jump", "Panel default…", "پیش‌فرض پنل…"},
		{"appearance.sub.title", "Subscription page appearance", "ظاهر صفحهٔ اشتراک"},
		{"appearance.sub.hint", "What your users see when they open their subscription link.", "آنچه کاربران با باز کردن لینک اشتراک خود می‌بینند."},
		{"appearance.sub.preset", "Default theme", "تم پیش‌فرض"},
		{"appearance.sub.preset_hint", "Visitors can still pick another theme on their page; that choice stays in their own browser.", "بازدیدکننده همچنان می‌تواند در صفحهٔ خودش تم دیگری انتخاب کند؛ این انتخاب فقط در مرورگر خودش می‌ماند."},
		{"appearance.sub.mode", "Default mode", "حالت پیش‌فرض"},
		{"appearance.sub.mode_hint", "A visitor's own light or dark choice takes priority.", "انتخاب روشن یا تاریکِ خود بازدیدکننده اولویت دارد."},
		{"appearance.sub.follow", "Follow the panel default", "پیروی از پیش‌فرض پنل"},
		{"appearance.sub.save", "Save subscription appearance", "ذخیرهٔ ظاهر صفحهٔ اشتراک"},
		{"appearance.sub.saved", "The subscription page appearance was updated.", "ظاهر صفحهٔ اشتراک به‌روز شد."},
		{"appearance.reset.me_hint", "Removes your personal theme and light/dark choice.", "تم شخصی و انتخاب روشن یا تاریک شما حذف می‌شود."},
		{"appearance.reset.panel_title", "Panel defaults", "پیش‌فرض‌های پنل"},
		{"appearance.reset.panel_hint", "Restores the built-in theme for the panel and the subscription page. Personal choices stay.", "تم پیش‌ساخته را برای پنل و صفحهٔ اشتراک برمی‌گرداند. انتخاب‌های شخصی حفظ می‌شوند."},
	}
	for _, row := range rows {
		catalogEN[row[0]] = row[1]
		catalogFA[row[0]] = row[2]
	}
}
