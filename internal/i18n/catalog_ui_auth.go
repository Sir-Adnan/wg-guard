package i18n

// Access surfaces (sign-in, first-run setup): the brand panel and field aids.
// Rows are {key, English, Persian}; parity is enforced by the catalog tests.
func init() {
	rows := [][3]string{
		{"auth.hero.eyebrow", "Self-hosted AmneziaWG", "AmneziaWG روی سرور خودتان"},
		{"auth.hero.title", "Your VPN node, fully under your control.", "نود VPN شما، کاملاً در اختیار خودتان."},
		{"auth.hero.body", "Users, quotas, devices and routing for this node, in one focused panel.", "کاربران، سهمیه‌ها، دستگاه‌ها و مسیریابی این نود، در یک پنل یکپارچه."},
		{"auth.hero.feature_tunnel", "Obfuscated AmneziaWG tunnels", "تونل‌های AmneziaWG با مبهم‌سازی"},
		{"auth.hero.feature_users", "Quota, expiry and devices for every user", "سهمیه، انقضا و دستگاه برای هر کاربر"},
		{"auth.hero.feature_routing", "Routing through your own outbounds", "مسیریابی از طریق خروجی‌های خودتان"},
		{"auth.capslock", "Caps Lock is on.", "کلید Caps Lock روشن است."},
		{"auth.signing_in", "Signing in…", "در حال ورود…"},
	}
	for _, row := range rows {
		catalogEN[row[0]] = row[1]
		catalogFA[row[0]] = row[2]
	}
}
