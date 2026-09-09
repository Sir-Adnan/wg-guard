package i18n

func init() {
	entries := [][3]string{
		{"Health check", "Health check", "بررسی سلامت"}, {"Docker preflight", "Docker preflight", "بررسی Docker"}, {"Host CLI (shim)", "Host management command", "دستور مدیریت میزبان"}, {"Compose project", "Compose project", "پروژه Compose"}, {"Starting container", "Starting container", "شروع کانتینر"}, {"systemd preflight", "systemd preflight", "بررسی systemd"}, {"Binary", "Install executable", "نصب برنامه"}, {"Systemd unit", "Systemd service", "سرویس systemd"}, {"Preflight", "Preflight checks", "بررسی پیش‌نیازها"}, {"Initial settings", "Initial settings", "تنظیمات اولیه"}, {"Stopping the node", "Stopping the node", "توقف گره"}, {"Removing artifacts", "Removing managed files", "حذف فایل‌های مدیریت‌شده"}, {"Removing installer-installed packages", "Removing recorded packages", "حذف بسته‌های ثبت‌شده"},
		{"System prerequisites", "System prerequisites", "پیش‌نیازهای سیستم"}, {"AmneziaWG core", "AmneziaWG core", "هسته AmneziaWG"}, {"Runtime image", "Runtime image", "تصویر اجرا"},
		{"Update", "Update", "به‌روزرسانی"}, {"Applying update", "Applying selected build", "اعمال نسخه انتخاب‌شده"},
		{"healthy", "Node answers on %s", "گره در %s پاسخ می‌دهد"}, {"shim", "%s → %s (data commands route to the container)", "%s → %s (دستورهای داده به کانتینر هدایت می‌شوند)"}, {"compose", "Wrote %s · image %s", "فایل %s نوشته شد · تصویر %s"}, {"started", "wg-guard.service enabled and started", "سرویس wg-guard.service فعال و شروع شد"}, {"persistence", "Boot persistence could not be written: %v", "ثبت بارگذاری هسته هنگام روشن‌شدن ممکن نشد: %v"}, {"port_free", "Panel TCP port %d is free", "درگاه TCP پنل %d آزاد است"}, {"dns_pending", "%s does not resolve yet. Point DNS here before certificate issuance.", "دامنه %s هنوز قابل حل نیست. پیش از صدور گواهی، DNS را به این میزبان متصل کنید."}, {"dns", "%s resolves to %s", "دامنه %s به %s اشاره می‌کند"},
		{"quiet_log", "Verbose command output is saved to %s", "خروجی کامل فرمان‌ها در %s ذخیره می‌شود"}, {"still_working", "Still working · %ds elapsed", "همچنان در حال اجرا · %d ثانیه گذشته"}, {"core_source", "Preparing reviewed source bundle %s; the first build can take several minutes.", "بسته سورس بررسی‌شده %s آماده می‌شود و ساخت نخست ممکن است چند دقیقه طول بکشد."}, {"core_ready", "Reviewed source bundle %s is ready", "بسته سورس بررسی‌شده %s آماده است"}, {"docker_start", "Starting the Docker service", "سرویس Docker راه‌اندازی می‌شود"}, {"runtime_build", "Building the local Docker runtime; the first build can take several minutes.", "تصویر محلی Docker ساخته می‌شود و ساخت نخست ممکن است چند دقیقه طول بکشد."},
		{"update_prepare", "Preparing lifecycle change", "تغییر چرخه آماده می‌شود"}, {"update_complete", "Update complete", "به‌روزرسانی کامل شد"},
		{"certificate", "Certificate is ready for %s", "گواهی برای %s آماده است"},
		{"nginx_challenge", "Nginx certificate route is ready for %s", "مسیر گواهی Nginx برای %s آماده است"}, {"nginx", "Nginx HTTPS is ready at %s", "HTTPS در Nginx در %s آماده است"},
		{"public endpoint", "Public endpoint", "نشانی عمومی"}, {"AWG port range start", "AWG UDP range start", "ابتدای بازه UDP برای AWG"}, {"AWG port range end", "AWG UDP range end", "انتهای بازه UDP برای AWG"}, {"VPN subnet pool", "VPN subnet pool", "شبکه VPN"}, {"client MTU", "Client MTU", "MTU کاربر"}, {"client DNS servers", "Client DNS servers", "سرورهای DNS کاربر"}, {"Telegram bot token", "Telegram bot token", "توکن ربات تلگرام"}, {"Telegram chat", "Telegram chat", "گفت‌وگوی تلگرام"}, {"backup schedule", "Backup schedule", "برنامه پشتیبان"},
	}
	for _, e := range entries {
		catalogs[En]["progress."+e[0]] = e[1]
		catalogs[Fa]["progress."+e[0]] = e[2]
	}
}
