package i18n

func init() {
	en := map[string]string{
		"state.idle":             "No active operation",
		"state.queued":           "Queued",
		"state.running":          "Running",
		"state.scheduled":        "Scheduled",
		"state.succeeded":        "Completed",
		"state.failed":           "Failed",
		"state.canceled":         "Canceled",
		"state.review":           "Ready for review",
		"state.awaiting_restart": "Approved · awaiting restart",
		"state.recovery_needed":  "Recovery required",
		"next.idle":              "Review the current state before choosing an operation.",
		"next.wait":              "The current operation owns its slot. Wait for its recorded result.",
		"next.schedule":          "The approved operation is scheduled; it can be canceled before execution.",
		"next.verified":          "The operation completed its checks; review the resulting state.",
		"next.inspect":           "Review the host status and operation record before retrying.",
		"next.recover":           "Finish the recorded recovery before starting another operation.",
		"next.review":            "Nothing has been applied. Review the source and target, then confirm or cancel.",
		"next.restart":           "Data has not been replaced yet. Restart the managed node to apply the approved archive, or cancel before restarting.",
		"next.canceled":          "The pending action was canceled; no new execution will start.",
	}
	fa := map[string]string{
		"state.idle":             "عملیات فعالی نیست",
		"state.queued":           "در صف",
		"state.running":          "در حال اجرا",
		"state.scheduled":        "زمان‌بندی‌شده",
		"state.succeeded":        "کامل‌شده",
		"state.failed":           "ناموفق",
		"state.canceled":         "لغوشده",
		"state.review":           "آمادهٔ بررسی",
		"state.awaiting_restart": "تأییدشده · در انتظار راه‌اندازی مجدد",
		"state.recovery_needed":  "نیازمند بازیابی",
		"next.idle":              "پیش از انتخاب عملیات، وضعیت فعلی را بررسی کنید.",
		"next.wait":              "عملیات فعلی جایگاه اجرا را در اختیار دارد؛ منتظر نتیجهٔ ثبت‌شده بمانید.",
		"next.schedule":          "عملیات تأییدشده زمان‌بندی شده است؛ پیش از اجرا می‌توانید آن را لغو کنید.",
		"next.verified":          "بررسی‌های عملیات کامل شد؛ وضعیت حاصل را بررسی کنید.",
		"next.inspect":           "پیش از تلاش دوباره، وضعیت میزبان و رکورد عملیات را بررسی کنید.",
		"next.recover":           "پیش از عملیات جدید، بازیابی ثبت‌شده را کامل کنید.",
		"next.review":            "هنوز تغییری اعمال نشده است؛ مبدأ و مقصد را بررسی کنید و سپس تأیید یا لغو کنید.",
		"next.restart":           "هنوز داده‌ها جایگزین نشده‌اند؛ نود مدیریت‌شده را برای اعمال آرشیو تأییدشده دوباره راه‌اندازی کنید یا پیش از آن لغو کنید.",
		"next.canceled":          "عملیات در انتظار لغو شد؛ اجرای جدیدی آغاز نمی‌شود.",
	}
	for key, value := range en {
		catalogEN["operations."+key] = value
	}
	catalogEN["operations.restart.cli"] = "Restart from the host manager after reviewing the approved archive. This starts the existing paired restore workflow."
	catalogFA["operations.restart.cli"] = "پس از بررسی آرشیو تأییدشده، از مدیر میزبان نود را دوباره راه‌اندازی کنید؛ فرایند بازیابی جفت داده و کلید اجرا می‌شود."
	for key, value := range fa {
		catalogFA["operations."+key] = value
	}
	for _, locale := range []Locale{En, Fa} {
		catalogs[locale]["manage.install_restore"] = "Install from verified backup"
		catalogs[locale]["manage.domains"] = "Domains & SSL certificates"
	}
}
