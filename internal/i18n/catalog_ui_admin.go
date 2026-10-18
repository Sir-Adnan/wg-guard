package i18n

// Admin workspaces chrome (settings, domains, resellers, tokens, webhooks).
// Rows are {key, English, Persian}; parity is enforced by the catalog tests.
func init() {
	rows := [][3]string{
		{"admin.ui.permissions_n", "%d permissions", "%d مجوز"},
		{"admin.ui.templates_n", "%d templates", "%d قالب"},
		{"admin.ui.accounts_n", "%d accounts", "%d حساب"},
		{"admin.ui.reseller_actions", "Reseller actions", "کارهای فروشنده"},
		{"admin.ui.search_customers", "Search customers", "جستجوی مشتری‌ها"},
		{"admin.ui.limit_of", "of %s", "از %s"},
	}
	for _, row := range rows {
		catalogEN[row[0]] = row[1]
		catalogFA[row[0]] = row[2]
	}
}
