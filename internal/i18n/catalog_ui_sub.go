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
		{"publicsub.ui.pass_kind", "Connection pass", "بلیط اتصال"},
		{"publicsub.ui.holder", "Holder", "دارنده"},
		{"publicsub.ui.valid_until", "Valid until", "معتبر تا"},
		{"publicsub.ui.no_end", "No end date", "بدون تاریخ پایان"},
		{"publicsub.ui.data", "Data", "حجم"},
		{"publicsub.ui.time", "Time", "زمان"},
		{"publicsub.ui.allowance", "Data allowance", "حجم کل"},
		{"publicsub.ui.plan", "Plan", "طرح"},
		{"publicsub.ui.devices", "Devices", "دستگاه‌ها"},
		{"publicsub.ui.status", "Status", "وضعیت"},
		{"publicsub.ui.device_used", "%s used", "%s مصرف"},
		{"publicsub.ui.last_seen", "Last connected %s", "آخرین اتصال %s"},
		{"publicsub.ui.never_connected", "Not connected yet", "هنوز وصل نشده"},
		{"publicsub.ui.sections", "Page sections", "بخش‌های صفحه"},
		{"publicsub.ui.details_intro", "Everything about your data and time in one place.", "همهٔ جزئیات حجم و زمان اشتراک شما در یک جا."},
		{"publicsub.tab.connect", "Connect", "اتصال"},
		{"publicsub.tab.guide", "Setup guide", "راهنمای نصب"},
		{"publicsub.tab.details", "Details", "جزئیات"},
		{"publicsub.faq.title", "Common questions", "پرسش‌های رایج"},
		{"publicsub.faq.no_internet_q", "I am connected but nothing loads", "وصل هستم اما چیزی باز نمی‌شود"},
		{"publicsub.faq.no_internet_a", "Turn the connection off and on again, and check that you still have data and days left. If it continues, contact your provider.", "اتصال را یک بار خاموش و روشن کنید و ببینید هنوز حجم و روز باقی‌مانده دارید. اگر مشکل ادامه داشت با ارائه‌دهنده تماس بگیرید."},
		{"publicsub.faq.new_device_q", "Can I use one configuration on two devices?", "می‌توانم یک کانفیگ را روی دو دستگاه استفاده کنم؟"},
		{"publicsub.faq.new_device_a", "No. Each configuration belongs to one device and the two would disconnect each other. Ask your provider for another device.", "خیر. هر کانفیگ مخصوص یک دستگاه است و دو دستگاه همدیگر را قطع می‌کنند. برای دستگاه دیگر از ارائه‌دهنده درخواست کنید."},
		{"publicsub.faq.share_q", "Is it safe to share this page?", "اشتراک‌گذاری این صفحه امن است؟"},
		{"publicsub.faq.share_a", "No. Anyone with this link can download your configurations. Keep it to yourself.", "خیر. هر کسی این لینک را داشته باشد می‌تواند کانفیگ‌های شما را دریافت کند. آن را پیش خود نگه دارید."},
		{"publicsub.faq.update_q", "My connection stopped after a change", "بعد از یک تغییر، اتصالم قطع شد"},
		{"publicsub.faq.update_a", "Your provider may have updated your configuration. Download it again from Connect and import it in your app.", "ممکن است ارائه‌دهنده کانفیگ شما را به‌روز کرده باشد. آن را دوباره از بخش اتصال دریافت و در برنامه وارد کنید."},
		{"error.status_label", "Error %d", "خطای %d"},
	}
	for _, row := range rows {
		catalogEN[row[0]] = row[1]
		catalogFA[row[0]] = row[2]
	}
}
