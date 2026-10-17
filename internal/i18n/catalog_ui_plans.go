package i18n

// Templates and interfaces strings added by the design-system rewrite. Rows
// are {key, English, Persian}; parity is enforced by the catalog tests.
func init() {
	rows := [][3]string{
		{"ifaces.form.junk", "Junk packets", "بسته‌های بی‌محتوا"},
		{"ifaces.form.sizes", "Handshake padding", "پرکنندهٔ دست‌دهی"},
		{"ifaces.form.protection", "Padding and header protection", "پرکننده و حفاظت سرآیند"},
		{"ifaces.form.timers", "Timers and handshakes", "زمان‌سنج‌ها و دست‌دهی"},
		{"ifaces.form.advanced_summary", "I1–I5, S3–S4, header protection and timers", "I1–I5، S3–S4، حفاظت سرآیند و زمان‌سنج‌ها"},
	}
	for _, row := range rows {
		catalogEN[row[0]] = row[1]
		catalogFA[row[0]] = row[2]
	}
}
