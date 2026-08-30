package i18n

// catalogs binds the locale string tables. Lookup order in T is
// requested-locale → English → key; the parity test guarantees en covers
// every fa key so the fallback chain never shows a raw key for committed
// strings.
var catalogs = map[Locale]map[string]string{
	Fa: catalogFA,
	En: catalogEN,
}
