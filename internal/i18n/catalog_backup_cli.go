package i18n

func init() {
	en := map[string]string{
		"node":         "Node ID",
		"secret_stdin": "Secret settings must be supplied with -stdin, never as an argument.",
		"usage":        "backup create|list|send --archive PATH|telegram-test|schedule-add|schedule-update|schedule-list|schedule-enable|schedule-disable|schedule-delete",
		"flags":        "Invalid or missing backup flag value.", "input": "Input must not exceed 4096 bytes.", "days": "Interval days must be 1–7; do not combine --days with --hours.", "schedule_invalid": "Invalid schedule: daily/weekly HH:MM UTC, weekday 0–6, interval 1–168 hours, retention 0–365.",
		"password": "Archive password (at least 8 characters; hidden)", "telegram_ok": "Telegram delivery verified; check the destination chat.", "archives": "Archive | size | created (UTC) | encrypted", "schedules": "Schedule ID | name | timing (UTC) | retention, enabled, next run", "deleted": "Schedule deleted.", "result": "Encrypted: %v · delivered: %s", "warning": "Warning: %s", "schedule_state": "keep %d (0 = default), enabled=%v, next=%s", "interval": "every %d hours",
		"restore_usage": "restore ARCHIVE [--password|--password-file PATH] [--yes] [--retry], or restore --recover [--password-file PATH] [--yes]",
		"host":          "Run restore on the deployment host.", "layout": "Managed restore requires the installed fixed config and data layout.", "review": "Restore environment review", "archive": "Archive", "source": "Source host / build", "endpoint": "Endpoint", "config_review": "Archived config is saved as .restored for separate review. Correct endpoint and node settings after restore; configuration is not applied automatically.", "original": "Recovery restores the recorded original database schema and matching key before starting the retained previous build.", "apply": "Stop the service, apply this verified restore, and start it?", "restored": "Restore applied; previous database, WAL and key files are retained in restore.previous. Service startup is being verified.",
		"schedule_action": "Schedule action", "add": "Create", "edit": "Replace definition", "enable": "Enable", "disable": "Disable", "delete": "Delete", "list": "List", "id": "Schedule ID", "name": "Schedule name", "kind": "Schedule kind", "daily": "Daily", "weekly": "Weekly", "hours": "Every N hours", "days_menu": "Every N days", "time": "Time (HH:MM UTC)", "weekday": "Weekday (0=Sunday … 6=Saturday)", "interval_value": "Interval", "retention": "Retention (0=default, 1–365)", "send": "Send existing archive", "backup_password": "Set backup password", "password_save": "Save this backup password?", "unencrypted": "Off-host backups without a password contain readable node secrets. Set a backup password before delivery.", "recover": "Recover previous build and original data", "enabled": "Enable this schedule?", "save": "Save this schedule change?",
	}
	fa := map[string]string{
		"node":         "شناسه گره",
		"secret_stdin": "تنظیمات محرمانه باید از -stdin دریافت شوند و نباید در آرگومان فرمان قرار گیرند.",
		"usage":        "backup create|list|send --archive PATH|telegram-test|schedule-add|schedule-update|schedule-list|schedule-enable|schedule-disable|schedule-delete",
		"flags":        "مقدار گزینه پشتیبان نامعتبر است یا وارد نشده است.", "input": "ورودی نباید بیشتر از 4096 بایت باشد.", "days": "فاصله روزانه باید 1 تا 7 باشد؛ --days و --hours را هم‌زمان وارد نکنید.", "schedule_invalid": "زمان‌بندی نامعتبر: ساعت HH:MM به UTC، روز هفته 0 تا 6، فاصله 1 تا 168 ساعت و نگهداری 0 تا 365.",
		"password": "گذرواژه آرشیو (حداقل 8 کاراکتر؛ مخفی)", "telegram_ok": "ارسال تلگرام تأیید شد؛ گفت‌وگوی مقصد را بررسی کنید.", "archives": "آرشیو | حجم | زمان ساخت UTC | رمزگذاری", "schedules": "شناسه | نام | زمان UTC | نگهداری، فعال بودن، اجرای بعدی", "deleted": "زمان‌بندی حذف شد.", "result": "رمزگذاری: %v · مقصدهای ارسال: %s", "warning": "هشدار: %s", "schedule_state": "نگهداری %d (0 = پیش‌فرض)، فعال=%v، بعدی=%s", "interval": "هر %d ساعت",
		"restore_usage": "restore ARCHIVE [--password|--password-file PATH] [--yes] [--retry] یا restore --recover [--password-file PATH] [--yes]",
		"host":          "بازیابی را روی میزبان نصب اجرا کنید.", "layout": "بازیابی مدیریت‌شده به مسیرهای ثابت تنظیمات و داده نصب نیاز دارد.", "review": "بازبینی محیط بازیابی", "archive": "آرشیو", "source": "میزبان و نسخه مبدأ", "endpoint": "نشانی اتصال", "config_review": "تنظیمات آرشیو برای بررسی جداگانه با پسوند .restored ذخیره می‌شود. نشانی اتصال و تنظیمات گره را پس از بازیابی اصلاح کنید؛ تنظیمات خودکار اعمال نمی‌شود.", "original": "بازیابی پیش از شروع نسخه قبلی، پایگاه داده با ساختار اصلی و کلید متناظر ثبت‌شده را برمی‌گرداند.", "apply": "سرویس متوقف شود، این بازیابی تأییدشده اعمال و سرویس شروع شود؟", "restored": "بازیابی اعمال شد؛ پایگاه داده، WAL و کلید قبلی در restore.previous حفظ شده‌اند. شروع سرویس در حال بررسی است.",
		"schedule_action": "عملیات زمان‌بندی", "add": "ساخت", "edit": "جایگزینی تعریف", "enable": "فعال‌سازی", "disable": "غیرفعال‌سازی", "delete": "حذف", "list": "فهرست", "id": "شناسه زمان‌بندی", "name": "نام زمان‌بندی", "kind": "نوع زمان‌بندی", "daily": "روزانه", "weekly": "هفتگی", "hours": "هر چند ساعت", "days_menu": "هر چند روز", "time": "ساعت (HH:MM به UTC)", "weekday": "روز هفته (0=یکشنبه تا 6=شنبه)", "interval_value": "فاصله اجرا", "retention": "نگهداری (0=پیش‌فرض، 1 تا 365)", "send": "ارسال آرشیو موجود", "backup_password": "تنظیم گذرواژه پشتیبان", "password_save": "این گذرواژه پشتیبان ذخیره شود؟", "unencrypted": "پشتیبان خارج از سرور بدون گذرواژه دارای اسرار خواندنی گره است. پیش از ارسال گذرواژه پشتیبان تنظیم کنید.", "recover": "بازیابی نسخه قبلی و داده اصلی", "enabled": "این زمان‌بندی فعال شود؟", "save": "این تغییر زمان‌بندی ذخیره شود؟",
	}
	for k, v := range en {
		catalogs[En]["backup.cli."+k] = v
	}
	for k, v := range fa {
		catalogs[Fa]["backup.cli."+k] = v
	}
}
