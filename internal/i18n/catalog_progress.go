package i18n

func init() {
	entries := [][3]string{
		{"Health check", "Health check", "بررسی سلامت"}, {"Docker preflight", "Docker preflight", "بررسی Docker"}, {"Host CLI (shim)", "Host management command", "دستور مدیریت میزبان"}, {"Compose project", "Compose project", "پروژه Compose"}, {"Starting container", "Starting container", "شروع کانتینر"}, {"systemd preflight", "systemd preflight", "بررسی systemd"}, {"Binary", "Install executable", "نصب برنامه"}, {"Systemd unit", "Systemd service", "سرویس systemd"}, {"Preflight", "Preflight checks", "بررسی پیش‌نیازها"}, {"Initial settings", "Initial settings", "تنظیمات اولیه"}, {"Stopping the node", "Stopping the node", "توقف گره"}, {"Removing artifacts", "Removing managed files", "حذف فایل‌های مدیریت‌شده"}, {"Removing installer-installed packages", "Removing recorded packages", "حذف بسته‌های ثبت‌شده"},
		{"healthy", "Node answers on %s", "گره در %s پاسخ می‌دهد"}, {"shim", "%s → %s (data commands route to the container)", "%s → %s (دستورهای داده به کانتینر هدایت می‌شوند)"}, {"compose", "Wrote %s · image %s", "فایل %s نوشته شد · تصویر %s"}, {"started", "wg-guard.service enabled and started", "سرویس wg-guard.service فعال و شروع شد"}, {"persistence", "Boot persistence could not be written: %v", "ثبت بارگذاری هسته هنگام روشن‌شدن ممکن نشد: %v"}, {"port_free", "Panel TCP port %d is free", "درگاه TCP پنل %d آزاد است"}, {"dns_pending", "%s does not resolve yet. Point DNS here before certificate issuance.", "دامنه %s هنوز قابل حل نیست. پیش از صدور گواهی، DNS را به این میزبان متصل کنید."}, {"dns", "%s resolves to %s", "دامنه %s به %s اشاره می‌کند"},
		{"public endpoint", "Public endpoint", "نشانی عمومی"}, {"AWG port range start", "AWG UDP range start", "ابتدای بازه UDP برای AWG"}, {"AWG port range end", "AWG UDP range end", "انتهای بازه UDP برای AWG"}, {"VPN subnet pool", "VPN subnet pool", "شبکه VPN"}, {"client MTU", "Client MTU", "MTU کاربر"}, {"client DNS servers", "Client DNS servers", "سرورهای DNS کاربر"}, {"Telegram bot token", "Telegram bot token", "توکن ربات تلگرام"}, {"Telegram chat", "Telegram chat", "گفت‌وگوی تلگرام"}, {"backup schedule", "Backup schedule", "برنامه پشتیبان"},
	}
	for _, e := range entries {
		catalogs[En]["progress."+e[0]] = e[1]
		catalogs[Fa]["progress."+e[0]] = e[2]
	}
}
