package i18n

func init() {
	en := map[string]string{
		"owner.title": "Local owner · before the listener opens", "owner.username": "Owner username (3–32 letters, digits, _ or -)", "owner.password": "Owner password (at least 10 bytes; hidden)", "owner.confirm": "Confirm password (hidden)", "owner.mismatch": "Passwords do not match. Run setup again.", "owner.required": "Fresh setup requires --owner-password-file with a private password file (0600).", "owner.check_failed": "Could not verify the local owner. The listener was not started.", "owner.reused": "Existing owner retained; credentials were not changed.", "owner.failed": "Owner creation failed. Check username and password requirements; the listener was not started.", "owner.ready": "Local owner is ready. Sign in with the credentials you supplied.", "owner.file": "Password file must be a regular private file (0600), with one nonempty password of at most 4096 bytes.",
		"terminal.done": "Done.", "terminal.failed": "Could not complete: %s", "terminal.recovery": "Review status and doctor before retrying. Recovery details: wg-guard update --recover.",
		"terminal.back": "  0  Back    q  Cancel", "terminal.choice": "Choose an action", "terminal.invalid": "Enter 1–%d, 0 to go back, or q to cancel.", "terminal.yes_no": "(yes/no)", "terminal.secret_long": "Input exceeds 4096 bytes.",
	}
	fa := map[string]string{
		"owner.title": "مالک محلی · پیش از شروع سرویس عمومی", "owner.username": "نام کاربری مالک (3 تا 32 حرف لاتین، عدد، _ یا -)", "owner.password": "گذرواژه مالک (حداقل 10 بایت؛ مخفی)", "owner.confirm": "تکرار گذرواژه (مخفی)", "owner.mismatch": "گذرواژه‌ها یکسان نیستند. راه‌اندازی را دوباره اجرا کنید.", "owner.required": "راه‌اندازی جدید به --owner-password-file و فایل خصوصی گذرواژه (0600) نیاز دارد.", "owner.check_failed": "بررسی مالک محلی ممکن نشد. سرویس عمومی شروع نشد.", "owner.reused": "مالک موجود حفظ شد؛ اطلاعات ورود تغییر نکرد.", "owner.failed": "ساخت مالک ناموفق بود. نام کاربری و گذرواژه را بررسی کنید؛ سرویس عمومی شروع نشد.", "owner.ready": "مالک محلی آماده است. با اطلاعات واردشده وارد پنل شوید.", "owner.file": "گذرواژه باید در فایل عادی خصوصی (0600)، در یک خط و حداکثر 4096 بایت باشد.",
		"terminal.done": "انجام شد.", "terminal.failed": "عملیات انجام نشد: %s", "terminal.recovery": "پیش از تلاش دوباره وضعیت و doctor را بررسی کنید. بازیابی: wg-guard update --recover",
		"terminal.back": "  0  بازگشت    q  لغو", "terminal.choice": "عملیات را انتخاب کنید", "terminal.invalid": "عدد 1 تا %d، برای بازگشت 0 یا برای لغو q را وارد کنید.", "terminal.yes_no": "(yes/no)", "terminal.secret_long": "ورودی بیشتر از 4096 بایت است.",
	}
	for k, v := range en {
		catalogs[En][k] = v
	}
	for k, v := range fa {
		catalogs[Fa][k] = v
	}
}
