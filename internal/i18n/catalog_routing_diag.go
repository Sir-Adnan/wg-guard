package i18n

// Routing diagnostics: the outbound list's bulk and speed tests and the site
// tests page. Rows are {key, English, Persian}; parity is enforced by the
// catalog tests.
func init() {
	rows := [][3]string{
		{"routing.tests", "Tests", "تست‌ها"},
		{"routing.tests.intro", "Check which exit each service accepts, from this server and every enabled outbound.", "ببینید هر سرویس کدام خروجی را می‌پذیرد؛ از مسیر همین سرور و همهٔ خروجی‌های فعال."},

		{"routing.ob.filter", "Show outbounds", "نمایش خروجی‌ها"},
		{"routing.ob.filter.all", "All", "همه"},
		{"routing.ob.filter.problems", "Problems", "مشکل‌دار"},
		{"routing.ob.search", "Search name, IP or endpoint", "جست‌وجوی نام، IP یا اندپوینت"},
		{"routing.ob.no_match", "No outbound matches this filter.", "هیچ خروجی‌ای با این فیلتر پیدا نشد."},
		{"routing.ob.col.outbound", "Outbound", "خروجی"},
		{"routing.ob.col.state", "State", "وضعیت"},
		{"routing.ob.col.speed", "Speed", "سرعت"},
		{"routing.ob.test_all", "Test all", "تست همه"},
		{"routing.ob.test_tip", "Check health now", "بررسی سلامت"},
		{"routing.ob.speed", "Speed test", "تست سرعت"},
		{"routing.ob.speed_tip", "Speed test (about 20 seconds)", "تست سرعت (حدود 20 ثانیه)"},
		{"routing.ob.speed_all", "Speed test all", "تست سرعت همه"},
		{"routing.ob.speed_stop", "Stop speed tests", "توقف تست سرعت"},
		{"routing.ob.speed_tested", "Speed tested", "تست سرعت"},
		{"routing.ob.download", "Download", "دانلود"},
		{"routing.ob.upload", "Upload", "آپلود"},
		{"routing.speed.error.timeout", "Timed out", "پاسخی نیامد"},
		{"routing.speed.error.unreachable", "Unreachable", "در دسترس نیست"},
		{"routing.speed.error.tls", "TLS error", "خطای TLS"},
		{"routing.speed.error.dns", "Name did not resolve", "نام پیدا نشد"},
		{"routing.speed.error.http", "Speed server refused", "سرور تست سرعت نپذیرفت"},
		{"routing.speed.error.busy", "Another speed test is running", "تست سرعت دیگری در حال اجراست"},
		{"routing.speed.error.unsupported", "Not supported here", "اینجا پشتیبانی نمی‌شود"},
		{"routing.toast.tested_all", "All enabled outbounds were tested.", "همهٔ خروجی‌های فعال تست شدند."},
		{"routing.toast.speed_done", "Speed test finished.", "تست سرعت تمام شد."},
		{"routing.toast.speed_busy", "Another speed test is running. Try again when it finishes.", "تست سرعت دیگری در حال اجراست. بعد از پایان آن دوباره امتحان کنید."},

		{"routing.site.all", "All services", "همهٔ سرویس‌ها"},
		{"routing.site.group.ai", "AI", "هوش مصنوعی"},
		{"routing.site.group.google", "Google", "گوگل"},
		{"routing.site.group.media", "Streaming", "استریم"},
		{"routing.site.group.network", "Network", "شبکه"},
		{"routing.site.exits", "%s exits", "%s خروجی"},
		{"routing.site.testing", "Testing…", "در حال تست…"},
		{"routing.site.region", "Country the service places this exit in", "کشوری که سرویس این خروجی را در آن می‌بیند"},
		{"routing.site.retry", "Test again", "تست دوباره"},
		{"routing.site.retried", "Answered on the second try", "در تلاش دوم پاسخ داد"},
		{"routing.site.legend", "What the results mean", "معنی نتیجه‌ها"},
		{"routing.site.legend_hint", "Why a site that opens on your phone can show “Bot check” here", "چرا سایتی که روی گوشی باز می‌شود اینجا «بررسی ربات» نشان می‌دهد"},
		{"routing.site.verdict.limited", "Limited", "محدود"},
		{"routing.site.verdict.challenge", "Bot check", "بررسی ربات"},
		{"routing.site.verdict.refused", "Refused", "رد شد"},
		{"routing.site.means.available", "The service works from this exit.", "سرویس از این خروجی کار می‌کند."},
		{"routing.site.means.reachable", "The site opened. Without a service detector, the test cannot tell whether every feature works.", "سایت باز شد. چون تشخیص‌دهندهٔ مخصوص ندارد، معلوم نیست همهٔ امکاناتش کار کند."},
		{"routing.site.means.limited", "The service answers but limits this exit (Netflix: only its own originals).", "سرویس پاسخ می‌دهد اما این خروجی را محدود می‌کند (نتفلیکس: فقط تولیدات خودش)."},
		{"routing.site.means.challenge", "The site is up and showed a “prove you are human” check. A real browser normally passes it, so the site usually opens on devices.", "سایت در دسترس است و صفحهٔ «ربات نیستید» نشان داد. مرورگر واقعی معمولاً از آن رد می‌شود، پس سایت روی دستگاه‌ها معمولاً باز می‌شود."},
		{"routing.site.means.refused", "The site’s firewall refused the test without naming a region (HTTP 401, 403 or 429). Often it refuses scripts only; check from a device.", "فایروال سایت تست را بدون ذکر منطقه رد کرد (HTTP 401، 403 یا 429). اغلب فقط اسکریپت‌ها را رد می‌کند؛ از یک دستگاه هم امتحان کنید."},
		{"routing.site.means.blocked", "The service refuses this exit’s country or network.", "سرویس کشور یا شبکهٔ این خروجی را نمی‌پذیرد."},
		{"routing.site.means.captcha", "Google flags this exit’s IP and asks for a CAPTCHA.", "گوگل IP این خروجی را مشکوک می‌داند و کپچا می‌خواهد."},
		{"routing.site.means.failed", "No answer through this exit: a timeout, a DNS failure or a broken path. The test retries once before reporting it.", "از این خروجی پاسخی نیامد: پایان زمان، خطای DNS یا مسیر قطع. تست قبل از گزارش یک بار دوباره امتحان می‌کند."},
		{"routing.site.means.unknown", "The answer was not recognized. Services change their pages; this is not a block.", "پاسخ شناخته نشد. سرویس‌ها صفحه‌هایشان را تغییر می‌دهند؛ این به معنی مسدود بودن نیست."},

		// Rule editor.
		{"routing.rules.selected", "Selected", "انتخاب‌شده"},
		{"routing.rules.services_filter", "Service groups", "گروه سرویس‌ها"},
		{"routing.rules.services_search", "Search services or domains", "جست‌وجوی سرویس یا دامنه"},
		{"routing.rules.services_none", "No service matches.", "سرویسی پیدا نشد."},
		{"routing.rules.preset_lines", "%s entries", "%s مورد"},
		{"routing.rules.preset_show", "Show what it adds", "نمایش موارد"},
		{"routing.rules.where", "Destination and name", "مقصد و نام"},
		{"routing.rules.enabled_switch", "Rule is on", "قانون فعال باشد"},
		{"routing.rules.sum_services", "{n} services", "{n} سرویس"},
		{"routing.rules.sum_lines", "{n} custom entries", "{n} مورد دلخواه"},
		{"routing.rules.sum_none", "Nothing selected yet", "هنوز چیزی انتخاب نشده"},
		{"routing.dns.before", "Before you turn it on", "پیش از روشن کردن"},
		{"routing.category_short.ai", "AI", "هوش مصنوعی"},
		{"routing.category_short.google", "Google", "گوگل"},
		{"routing.category_short.media", "Media", "رسانه"},
		{"routing.category_short.dev", "Dev", "برنامه‌نویسی"},
		{"routing.category_short.ipcheck", "IP check", "تشخیص IP"},
	}
	for _, row := range rows {
		catalogEN[row[0]] = row[1]
		catalogFA[row[0]] = row[2]
	}
}
