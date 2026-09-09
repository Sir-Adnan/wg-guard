package i18n

func init() {
	en := map[string]string{
		"owner.title": "Administrator account", "owner.username": "Admin username (3–32 letters, digits, _ or -)", "owner.username_invalid": "Use 3–32 letters, digits, _ or -.", "owner.password": "Admin password (10+ characters; Enter generates one)", "owner.password_short": "Password must be at least %d characters. Try again.", "owner.confirm": "Confirm password", "owner.mismatch": "Passwords do not match. Try again.", "owner.generate_failed": "Could not securely generate an administrator password. Setup stopped before the listener started.", "owner.required": "Fresh setup requires --owner-password-file with a private password file (0600).", "owner.check_failed": "Could not verify the administrator account. The listener was not started.", "owner.reused": "Existing administrator retained; credentials were not changed.", "owner.failed": "Administrator creation failed. Check the username and password; the listener was not started.", "owner.ready": "Administrator account is ready. Sign in with the credentials you supplied.", "owner.credentials_status": "SAVE NOW", "owner.credentials_title": "Save administrator credentials", "owner.credentials_notice": "This generated password is shown once and is not stored in readable form.", "owner.file": "Password file must be a regular private file (0600), with one nonempty password of at most 4096 bytes.",
		"terminal.done": "Done.", "terminal.failed": "Could not complete: %s", "terminal.recovery": "Review status and the recorded lifecycle operation; follow its recovery guidance in docs/operations/lifecycle-recovery.md before retrying.",
		"terminal.back": "  0  Back", "terminal.exit": "  0  Exit", "terminal.choice": "Choose an action", "terminal.invalid": "Enter 1–%d, or 0 to go back.", "terminal.invalid_exit": "Enter 1–%d, or 0 to exit.", "terminal.yes_no": "Enter y or n.", "terminal.secret_long": "Input exceeds 4096 bytes.",
	}
	fa := map[string]string{
		"owner.title": "مالک محلی · پیش از شروع سرویس عمومی", "owner.username": "نام کاربری مالک (3 تا 32 حرف لاتین، عدد، _ یا -)", "owner.username_invalid": "Use 3–32 letters, digits, _ or -.", "owner.password": "Admin password (10+ characters; Enter generates one)", "owner.password_short": "Password must be at least %d characters. Try again.", "owner.confirm": "Confirm password", "owner.mismatch": "Passwords do not match. Try again.", "owner.generate_failed": "Could not securely generate an administrator password. Setup stopped before the listener started.", "owner.required": "راه‌اندازی جدید به --owner-password-file و فایل خصوصی گذرواژه (0600) نیاز دارد.", "owner.check_failed": "بررسی مالک محلی ممکن نشد. سرویس عمومی شروع نشد.", "owner.reused": "مالک موجود حفظ شد؛ اطلاعات ورود تغییر نکرد.", "owner.failed": "ساخت مالک ناموفق بود. نام کاربری و گذرواژه را بررسی کنید؛ سرویس عمومی شروع نشد.", "owner.ready": "مالک محلی آماده است. با اطلاعات واردشده وارد پنل شوید.", "owner.credentials_status": "SAVE NOW", "owner.credentials_title": "Save administrator credentials", "owner.credentials_notice": "This generated password is shown once and is not stored in readable form.", "owner.file": "گذرواژه باید در فایل عادی خصوصی (0600)، در یک خط و حداکثر 4096 بایت باشد.",
		"terminal.done": "انجام شد.", "terminal.failed": "عملیات انجام نشد: %s", "terminal.recovery": "پیش از تلاش دوباره وضعیت و عملیات ثبت‌شده را بررسی کنید و راهنمای بازیابی همان عملیات را در docs/operations/lifecycle-recovery.md دنبال کنید.",
		"terminal.back": "  0  بازگشت", "terminal.exit": "  0  خروج", "terminal.choice": "عملیات را انتخاب کنید", "terminal.invalid": "عدد 1 تا %d یا برای بازگشت 0 را وارد کنید.", "terminal.invalid_exit": "عدد 1 تا %d یا برای خروج 0 را وارد کنید.", "terminal.yes_no": "y یا n را وارد کنید.", "terminal.secret_long": "ورودی بیشتر از 4096 بایت است.",
	}
	for k, v := range en {
		catalogs[En][k] = v
	}
	for k, v := range fa {
		catalogs[Fa][k] = v
	}
}
