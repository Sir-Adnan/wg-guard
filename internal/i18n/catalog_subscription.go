package i18n

// Public subscription page: meters, AmneziaVPN hand-off and the per-platform
// setup guide. Rows are {key, English, Persian}.
func init() {
	rows := [][3]string{
		{"publicsub.time_left", "Time left", "زمان باقی‌مانده"},
		{"publicsub.days", "days", "روز"},
		{"publicsub.used_percent", "%d%% used", "%d٪ مصرف شده"},
		{"publicsub.open_amneziavpn", "Open in AmneziaVPN", "باز کردن در AmneziaVPN"},
		{"publicsub.amnezia_file_ios", "If AmneziaVPN is not in the list, open the downloaded file from Files and share it to AmneziaVPN.", "اگر AmneziaVPN در فهرست نیست، فایل دریافت‌شده را از برنامهٔ Files باز کنید و با «اشتراک‌گذاری» به AmneziaVPN بفرستید."},
		{"publicsub.amnezia_file_desktop", "File downloaded. In AmneziaVPN choose “Add” → “File with connection settings” and pick it.", "فایل دریافت شد. در AmneziaVPN گزینهٔ «افزودن» و سپس «فایل با تنظیمات اتصال» را بزنید و همین فایل را انتخاب کنید."},
		{"publicsub.amnezia_tap_again", "Ready. Tap “Open in AmneziaVPN” again.", "آماده است. دوباره «باز کردن در AmneziaVPN» را بزنید."},
		{"publicsub.key_copied", "Key copied. In AmneziaVPN choose “Add” → “Insert key”.", "کلید کپی شد. در AmneziaVPN گزینهٔ «افزودن» و سپس «درج کلید» را بزنید."},
		{"publicsub.guide_eyebrow", "Install and connect", "نصب و اتصال"},
		{"publicsub.guide_intro", "Choose your device, install one of these apps and import your configuration.", "دستگاه خود را انتخاب کنید، یکی از این برنامه‌ها را نصب کنید و کانفیگ را وارد کنید."},
		{"publicsub.os.android", "Android", "اندروید"},
		{"publicsub.os.ios", "iPhone / iPad", "آیفون و آیپد"},
		{"publicsub.os.windows", "Windows", "ویندوز"},
		{"publicsub.os.macos", "macOS", "مک"},
		{"publicsub.os.linux", "Linux", "لینوکس"},
		{"publicsub.app.recommended", "Recommended", "پیشنهادی"},
		{"publicsub.app.simple", "Easiest", "ساده‌ترین"},
		{"publicsub.app.advanced", "Advanced", "پیشرفته"},
		{"publicsub.app.amneziawg_desc", "The official AmneziaWG app. Lightweight and made for this connection type.", "برنامهٔ رسمی AmneziaWG؛ سبک و مخصوص همین نوع اتصال."},
		{"publicsub.app.wgtunnel_desc", "A free, open-source client with auto-connect and split tunneling. Supports AmneziaWG configs.", "کلاینت رایگان و متن‌باز با اتصال خودکار و تونل جداگانه برای برنامه‌ها؛ از کانفیگ AmneziaWG پشتیبانی می‌کند."},
		{"publicsub.app.amneziavpn_desc", "One app for every platform. Open the link below or paste the copied key.", "یک برنامه برای همهٔ سیستم‌ها. لینک پایین را باز کنید یا کلید کپی‌شده را وارد کنید."},
		{"publicsub.app.awgtools_desc", "Command-line tools (awg-quick) for servers and advanced users.", "ابزار خط فرمان (awg-quick) برای سرورها و کاربران حرفه‌ای."},
		{"publicsub.step.install", "Install the app.", "برنامه را نصب کنید."},
		{"publicsub.step.conf", "Download your config above and open it in the app, or scan its QR code.", "کانفیگ خود را از بالا دانلود و در برنامه باز کنید، یا کد QR آن را اسکن کنید."},
		{"publicsub.step.key", "Tap “Open in AmneziaVPN” on your device card, or copy the key and paste it in the app.", "روی «باز کردن در AmneziaVPN» در کارت دستگاه بزنید، یا کلید را کپی و در برنامه وارد کنید."},
		{"publicsub.step.cli_save", "Save the downloaded config as:", "کانفیگ دانلودشده را با این مسیر ذخیره کنید:"},
		{"publicsub.step.cli_up", "Bring the tunnel up:", "تونل را با این دستور روشن کنید:"},
		{"publicsub.step.connect", "Turn the connection on.", "اتصال را روشن کنید."},
		{"publicsub.store.play", "Google Play", "گوگل‌پلی"},
		{"publicsub.store.apk", "APK file", "فایل APK"},
		{"publicsub.store.download", "Download", "دانلود"},
		{"publicsub.store.website", "Website", "وب‌سایت"},
		{"publicsub.store.source", "Source and guide", "کد و راهنما"},
	}
	for _, row := range rows {
		catalogEN[row[0]] = row[1]
		catalogFA[row[0]] = row[2]
	}
}
