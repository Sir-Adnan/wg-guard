package i18n

// Routing workspace strings added by the design-system rewrite. Rows are
// {key, English, Persian}; parity is enforced by the catalog tests.
func init() {
	rows := [][3]string{
		{"routing.rules.order_hint", "Checked top to bottom; the first matching rule decides. Everything else uses the last row.", "از بالا به پایین بررسی می‌شوند و اولین قانون مطابق تصمیم می‌گیرد. بقیهٔ ترافیک از ردیف آخر پیروی می‌کند."},
	}
	for _, row := range rows {
		catalogEN[row[0]] = row[1]
		catalogFA[row[0]] = row[2]
	}
}
