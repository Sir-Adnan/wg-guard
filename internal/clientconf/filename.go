// Short client-compatible config filenames. A stable device-ID fingerprint
// keeps multiple devices distinct without exposing or embedding device names.
package clientconf

import (
	"crypto/sha256"
	"encoding/base32"
	"strings"
)

// ConfigFilename keeps the profile-name stem at 15 ASCII characters or less
// for conservative mobile-client compatibility. The 40-bit suffix derives
// from the stable device ID, so changing a device label does not rename its
// downloaded profile. Optional legacy prefix/suffix each use at most two
// characters; the remaining label budget goes to the username.
func ConfigFilename(prefix, username, deviceID, suffix string) string {
	p := filenameAlnum(prefix, 2)
	s := filenameAlnum(suffix, 2)
	label := p + filenameAlnum(username, 6-len(p)-len(s)) + s
	if label == "" {
		label = "wg"
	}
	digest := sha256.Sum256([]byte(deviceID))
	shortID := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:5]))
	return label + "-" + shortID + ".conf"
}

// ConfigArchiveFilename is an ordinary ZIP name, not an importable profile.
func ConfigArchiveFilename(username string) string {
	label := filenameAlnum(username, 20)
	if label == "" {
		label = "wg"
	}
	return label + "-configs.zip"
}

func filenameAlnum(s string, max int) string {
	if max <= 0 {
		return ""
	}
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
		if b.Len() >= max {
			break
		}
	}
	return b.String()
}
