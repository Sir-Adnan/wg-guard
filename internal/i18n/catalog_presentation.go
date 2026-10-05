package i18n

func init() {
	rows := [][3]string{
		{"appearance.sections", "Appearance sections", "بخش‌های ظاهر"}, {"appearance.style_tab", "Visual style", "سبک بصری"},
		{"appearance.numbers_tab", "Numerals", "اعداد"}, {"appearance.reset_tab", "Reset", "بازنشانی"},
		{"appearance.live_preview", "Live preview", "پیش‌نمایش زنده"},
		{"appearance.account_scope", "My account", "حساب من"}, {"appearance.panel_scope", "Panel default", "پیش‌فرض پنل"},
		{"appearance.preview_only", "Preview locally, then apply to save your choice.", "ابتدا پیش‌نمایش را ببینید؛ برای ذخیره، اعمال را بزنید."},
		{"nav.group.preferences", "Preferences", "تنظیمات"},
		{"users.form.creation_mode", "User creation method", "روش ساخت کاربر"},
		{"users.form.mode_custom", "Standard", "معمولی"}, {"users.form.mode_template", "From template", "از روی قالب"},
		{"users.form.choose_template", "Choose a template", "انتخاب قالب"},
		{"users.form.template_required", "Choose a template or switch to Standard.", "یک قالب انتخاب کنید یا به حالت معمولی بروید."},
		{"ifaces.editor.parameters", "Inspect and edit packet parameters", "بررسی و ویرایش پارامترهای بسته"},
		{"ifaces.editor.sections", "Interface sections", "بخش‌های اینترفیس"}, {"ifaces.editor.general", "General", "عمومی"}, {"ifaces.editor.addresses", "Address pools", "بازه‌های آدرس"},
		{"ifaces.editor.title", "IPv4 address pools", "بازه‌های آدرس IPv4"}, {"ifaces.editor.mode", "Primary pool mode", "حالت بازهٔ اصلی"}, {"ifaces.editor.automatic", "Automatic", "خودکار"}, {"ifaces.editor.custom", "Choose or enter a pool", "انتخاب یا واردکردن بازه"},
		{"ifaces.editor.hint", "Choose a capacity or enter a canonical private CIDR. Each config uses one address. Suggested networks are a snapshot; save checks overlap again.", "ظرفیت موردنیاز را انتخاب کنید یا CIDR خصوصی صحیح وارد کنید. هر کانفیگ یک آدرس مصرف می‌کند. پیشنهاد بازه یک پیش‌نمایش است؛ ذخیره دوباره تداخل را بررسی می‌کند."},
		{"ifaces.editor.proposed", "Available automatic pool", "بازهٔ خودکار آزاد"}, {"ifaces.editor.size", "Suggested network size", "اندازهٔ بازهٔ پیشنهادی"}, {"ifaces.editor.preset", "Prepared pools and capacity", "بازه‌های آماده و ظرفیت"}, {"ifaces.editor.choose", "Choose a prepared pool", "انتخاب بازهٔ آماده"},
		{"ifaces.editor.primary_hint", "The primary pool stays fixed after creation. Add overflow pools to expand capacity while preserving existing configs.", "بازهٔ اصلی پس از ساخت ثابت می‌ماند. برای افزایش ظرفیت با حفظ کانفیگ‌های قبلی، بازهٔ تکمیلی اضافه کنید."},
		{"ifaces.editor.add", "Add selected overflow pool", "افزودن بازهٔ تکمیلی انتخاب‌شده"}, {"ifaces.editor.available", "Available", "آزاد"}, {"ifaces.editor.overlap", "Overlaps a reserved pool or host route", "تداخل با بازهٔ رزروشده یا مسیر میزبان"},
		{"ifaces.editor.current", "Already assigned to this profile", "در این پروفایل ثبت‌شده"}, {"ifaces.editor.full", "Full; allocation advances to the next pool", "پر؛ تخصیص از بازهٔ بعدی ادامه می‌یابد"}, {"ifaces.editor.failed", "Suggestions could not be loaded. You can enter a CIDR manually; save still validates it.", "پیشنهاد بازه دریافت نشد. CIDR را دستی وارد کنید؛ ذخیره همچنان آن را اعتبارسنجی می‌کند."},
		{"appearance.digits.title", "Number display", "نمایش اعداد"}, {"appearance.digits.hint", "Choose numeral glyphs independently of language and theme. Inputs, IP/CIDR, ports, keys, identifiers and copied values remain canonical Latin.", "شکل اعداد را مستقل از زبان و تم انتخاب کنید. ورودی‌ها، IP/CIDR، پورت، کلید، شناسه و مقادیر کپی‌شده لاتین می‌مانند."},
		{"appearance.digits.personal", "For my account", "برای حساب من"}, {"appearance.digits.default", "Panel default", "پیش‌فرض پنل"}, {"appearance.digits.inherit", "Use panel default", "استفاده از پیش‌فرض پنل"},
		{"appearance.digits.latin", "English numerals", "اعداد انگلیسی"}, {"appearance.digits.persian", "Persian numerals", "اعداد فارسی"},
		{"common.field_help", "Help for %s", "راهنمای %s"}, {"common.help", "Help", "راهنما"},
	}
	for _, row := range rows {
		catalogEN[row[0]] = row[1]
		catalogFA[row[0]] = row[2]
	}
}
