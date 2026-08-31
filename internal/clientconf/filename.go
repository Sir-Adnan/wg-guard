// Config download filenames: [prefix]username-device[suffix].conf built from
// the downloads.filename_prefix / downloads.filename_suffix settings.
package clientconf

import "strings"

// ConfigFilename builds a safe, descriptive download filename. Every part is
// sanitized to [A-Za-z0-9._-] (URL/Windows/Unix-safe, no path separators) and
// the result stays ASCII so Content-Disposition can quote it directly.
func ConfigFilename(prefix, username, deviceName, suffix string) string {
	part := func(s string, max int) string {
		var b strings.Builder
		for _, r := range strings.TrimSpace(s) {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
				r == '_', r == '-', r == '.':
				b.WriteRune(r)
			default:
				b.WriteByte('-')
			}
			if b.Len() >= max {
				break
			}
		}
		return strings.Trim(b.String(), "-._")
	}
	var sb strings.Builder
	add := func(s string) {
		if sb.Len() > 0 && s != "" {
			sb.WriteByte('-')
		}
		sb.WriteString(s)
	}
	add(part(prefix, 24))
	add(part(username, 32))
	add(part(deviceName, 32))
	add(part(suffix, 24))
	if sb.Len() == 0 {
		sb.WriteString("config")
	}
	return sb.String() + ".conf"
}
