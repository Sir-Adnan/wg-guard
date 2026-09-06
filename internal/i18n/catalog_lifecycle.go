package i18n

func init() {
	catalogEN["install.cli.release"] = "published release tag or latest (never falls back to source)"
	catalogFA["install.cli.release"] = "برچسب انتشار یا latest (هرگز خودکار به کد منبع برنمی‌گردد)"
	catalogEN["install.cli.commit"] = "main or full immutable commit SHA (development build)"
	catalogFA["install.cli.commit"] = "main یا SHA کامل تغییرناپذیر (ساخت توسعه)"
	catalogEN["install.cli.metadata"] = "acquired build identity JSON from bootstrap"
	catalogFA["install.cli.metadata"] = "فایل JSON هویت ساخت دریافت‌شده از راه‌انداز"
	catalogEN["install.cli.local_image"] = "use an explicitly staged local image without pulling"
	catalogFA["install.cli.local_image"] = "استفاده از تصویر محلی آماده‌شده بدون دریافت"
	catalogEN["install.cli.recover"] = "recover the interrupted lifecycle operation"
	catalogFA["install.cli.recover"] = "بازیابی عملیات مدیریت قطع‌شده"
	catalogEN["install.cli.sources"] = "Choose one source; recovery/rollback cannot be combined with acquisition or local overrides."
	catalogFA["install.cli.sources"] = "یک منبع انتخاب کنید؛ بازیابی/بازگشت با دریافت یا جایگزین محلی ترکیب نمی‌شود."
	catalogEN["install.error.core_confirmation"] = "Use core switch BUNDLE --confirm-impact after reviewing tunnel interruption and possible maintenance reboot."
	catalogFA["install.error.core_confirmation"] = "پس از بررسی وقفهٔ تونل و راه‌اندازی مجدد احتمالی، core switch BUNDLE --confirm-impact را اجرا کنید."
	catalogEN["install.error.core_transition"] = "Only one verified core bundle is catalogued; this installed version has no verified automatic transition. Back up, plan a maintenance window and follow the manual core migration runbook."
	catalogFA["install.error.core_transition"] = "فقط یک بستهٔ هستهٔ تأییدشده در فهرست است؛ برای نسخهٔ نصب‌شده انتقال خودکار تأییدشده‌ای وجود ندارد. پشتیبان بگیرید، زمان نگهداری تعیین و راهنمای انتقال دستی هسته را دنبال کنید."
	catalogEN["install.core.single_bundle"] = "The catalog has one verified bundle (awg-2026-08); it is already installed. No package change or tunnel interruption was performed."
	catalogFA["install.core.single_bundle"] = "فهرست یک بستهٔ تأییدشده (awg-2026-08) دارد و همان نصب است. بسته‌ای تغییر نکرد و تونلی قطع نشد."
	entries := map[string][2]string{
		"rollback_restore": {"Rollback requires coordinated database and master-key restoration from the recorded backup before old code can run. Active deployment is unchanged; see the lifecycle runbook.", "بازگشت پیش از اجرای نسخهٔ قدیمی به بازگردانی هماهنگ پایگاه داده و کلید اصلی از پشتیبان ثبت‌شده نیاز دارد. استقرار فعال تغییر نکرده است؛ راهنمای عملیات را ببینید."},
		"backup_required":  {"Cannot skip the pre-update backup when data compatibility is unproven.", "وقتی سازگاری داده ثابت نشده است، پشتیبان پیش از به‌روزرسانی را نمی‌توان نادیده گرفت."},
		"state":            {"Install state is invalid, unsupported, or outside the managed layout; inspect it and migrate manually.", "وضعیت نصب نامعتبر، پشتیبانی‌نشده یا خارج از مسیر مدیریت‌شده است؛ آن را بررسی و دستی منتقل کنید."},
		"lock":             {"Another lifecycle operation is running; retry when it finishes.", "عملیات مدیریت دیگری در حال اجراست؛ پس از پایان دوباره تلاش کنید."},
		"journal":          {"Lifecycle journal is invalid; inspect recovery files before proceeding.", "گزارش عملیات نامعتبر است؛ پیش از ادامه فایل‌های بازیابی را بررسی کنید."},
		"contract":         {"Selected build lacks the required Phase 8.1 installer contract; select a compatible build.", "ساخت انتخاب‌شده قرارداد نصب فاز ۸.۱ را ندارد؛ ساخت سازگار انتخاب کنید."},
		"root":             {"Run lifecycle operations as root.", "عملیات مدیریت را با دسترسی root اجرا کنید."},
		"no_state":         {"No managed installation exists.", "نصب مدیریت‌شده‌ای وجود ندارد."},
		"pending":          {"An interrupted operation requires recovery; run wg-guard update --recover.", "عملیات قطع‌شده به بازیابی نیاز دارد؛ wg-guard update --recover را اجرا کنید."},
		"no_previous":      {"No previous healthy artifact is recorded.", "نسخهٔ سالم قبلی ثبت نشده است."},
		"restore_required": {"Recovery requires coordinated database and master-key restore from the recorded backup. Service remains stopped; automatic paired restore is unavailable. See the lifecycle runbook.", "بازیابی به بازگردانی هماهنگ پایگاه داده و کلید اصلی از پشتیبان ثبت‌شده نیاز دارد. سرویس متوقف می‌ماند؛ بازگردانی خودکار جفت در دسترس نیست. راهنمای عملیات را ببینید."},
		"update_failed":    {"Update failed; consult the lifecycle journal for rolled back or recovery-required status.", "به‌روزرسانی شکست خورد؛ وضعیت بازگشت یا نیاز به بازیابی را در گزارش عملیات ببینید."},
		"binary":           {"Provide --binary PATH or an explicitly acquired build (including the matching Docker host command).", "مسیر --binary PATH یا ساخت دریافت‌شدهٔ مشخص را ارائه کنید (شامل فرمان میزبان همسان Docker)."},
		"image_identity":   {"An immutable Docker image identity is required.", "شناسهٔ تغییرناپذیر تصویر Docker الزامی است."},
		"shim":             {"Docker image binary and host command checksums differ.", "چک‌سام برنامهٔ تصویر Docker و فرمان میزبان متفاوت است."},
		"compose":          {"Compose must contain exactly one managed image reference.", "Compose باید دقیقاً یک ارجاع تصویر مدیریت‌شده داشته باشد."},
		"stop":             {"Service stop could not be confirmed; files and data were preserved.", "توقف سرویس تأیید نشد؛ فایل‌ها و داده‌ها حفظ شدند."},
		"recovery_failed":  {"Recovery failed; manual intervention is required. Retained artifacts and journal are preserved.", "بازیابی شکست خورد؛ اقدام دستی لازم است. نسخه‌های نگهداری‌شده و گزارش حفظ شدند."},
		"manual_recovery":  {"This interrupted operation needs manual recovery; inspect lifecycle.json and the runbook.", "این عملیات قطع‌شده به بازیابی دستی نیاز دارد؛ lifecycle.json و راهنمای عملیات را بررسی کنید."},
		"archive":          {"A verified local pre-update archive could not be recorded; active deployment was not changed.", "پشتیبان محلی معتبر پیش از به‌روزرسانی ثبت نشد؛ استقرار فعال تغییر نکرد."},
	}
	for k, v := range entries {
		catalogEN["install.error."+k] = v[0]
		catalogFA["install.error."+k] = v[1]
	}
}
