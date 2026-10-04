package i18n

func init() {
	for key, value := range map[string]string{
		"backup.safety.verification_incomplete": "backup: verification was interrupted or timed out; no validity result is available; retry before approving a restore",
		"backup.safety.archive_busy":            "backup: another archive operation is running; retry after it finishes",
		"backup.safety.migration_inspection":    "backup: schema could not be safely inspected; migration stopped",
		"backup.safety.migration_backup":        "backup: required pre-migration archive failed; schema was not changed; retain the original database/key and repair the archive destination before retrying",
		"backup.safety.data_snapshot":           "backup: snapshot must be a regular database within the restore size limit",
		"backup.safety.data_inspection":         "backup: offline database validation failed; no data was replaced",
		"backup.safety.data_references":         "backup: database contains broken record references; no data was replaced",
		"backup.safety.data_key":                "backup: archived master key is missing or does not decrypt all stored secrets; retain the original node and repair its database/key pair before migration",
		"backup.safety.data_envelope":           "backup: stored encrypted value is malformed or exceeds the supported size; no data was replaced",
		"backup.cli.verified":                   "Verified archive: %s",
		"backup.cli.inventory":                  "Stored users: %d · devices: %d · templates: %d · customer links: %d",
		"backup.cli.inventory_access":           "Administrators: %d · API tokens: %d · webhooks: %d · resellers: %d",
		"backup.cli.inventory_backends":         "Kernel interfaces: %d · userspace interfaces: %d · encrypted values checked: %d",
		"backup.cli.verified_scope":             "Offline data verification completed. Deployment settings, TLS, host networking and client connectivity still require review on the target server.",
		"backups.review_inventory":              "Stored accounts / devices",
		"backups.review_secrets":                "Encrypted values checked",
		"backups.review_backends":               "Interfaces: kernel / userspace",
	} {
		catalogs[En][key] = value
	}
	for key, value := range map[string]string{
		"backup.safety.verification_incomplete": "بررسی پشتیبان لغو شد یا به پایان مهلت رسید؛ معتبر یا خراب بودن آن مشخص نشد؛ پیش از تأیید بازیابی دوباره بررسی کنید",
		"backup.safety.archive_busy":            "یک عملیات پشتیبان‌گیری دیگر در حال اجراست؛ پس از پایان آن دوباره تلاش کنید",
		"backup.safety.migration_inspection":    "بررسی ایمن ساختار پایگاه داده ممکن نشد؛ مهاجرت متوقف شد",
		"backup.safety.migration_backup":        "ساخت پشتیبان لازم پیش از مهاجرت ناموفق بود؛ ساختار پایگاه داده تغییر نکرد؛ جفت پایگاه داده و کلید اصلی را حفظ و پیش از تلاش دوباره مسیر پشتیبان را اصلاح کنید",
		"backup.safety.data_snapshot":           "نسخهٔ پایگاه داده باید فایل عادی و در سقف حجم پشتیبانی‌شدهٔ بازیابی باشد",
		"backup.safety.data_inspection":         "بررسی مستقل پایگاه داده ناموفق بود؛ داده‌ای جایگزین نشد",
		"backup.safety.data_references":         "ارتباط برخی رکوردهای پایگاه داده ناقص است؛ داده‌ای جایگزین نشد",
		"backup.safety.data_key":                "کلید اصلی آرشیو وجود ندارد یا همهٔ داده‌های محرمانه را رمزگشایی نمی‌کند؛ پیش از مهاجرت، نصب اصلی را حفظ و جفت پایگاه داده و کلید آن را اصلاح کنید",
		"backup.safety.data_envelope":           "یکی از مقادیر رمز‌شده نامعتبر یا بزرگ‌تر از سقف پشتیبانی‌شده است؛ داده‌ای جایگزین نشد",
		"backup.cli.verified":                   "آرشیو بررسی شد: %s",
		"backup.cli.inventory":                  "کاربران ذخیره‌شده: %d · دستگاه‌ها: %d · قالب‌ها: %d · لینک‌های اشتراک: %d",
		"backup.cli.inventory_access":           "مدیران: %d · توکن‌های API: %d · وب‌هوک‌ها: %d · نمایندگان: %d",
		"backup.cli.inventory_backends":         "اینترفیس‌های کرنل: %d · اینترفیس‌های userspace: %d · مقادیر رمز‌شدهٔ بررسی‌شده: %d",
		"backup.cli.verified_scope":             "بررسی مستقل داده‌ها انجام شد. تنظیمات استقرار، TLS، شبکه و اتصال کلاینت باید روی سرور مقصد بررسی شوند.",
		"backups.review_inventory":              "حساب‌ها و دستگاه‌های ذخیره‌شده",
		"backups.review_secrets":                "مقادیر رمز‌شدهٔ بررسی‌شده",
		"backups.review_backends":               "اینترفیس‌ها: کرنل / userspace",
	} {
		catalogs[Fa][key] = value
	}
}
