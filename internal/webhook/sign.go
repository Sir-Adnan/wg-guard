package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// Sign produces the X-WG-Signature header value:
//
//	t=<unix seconds>,v1=<hex hmac-sha256(secret, "<t>.<body>")>
//
// (docs/integrations/webhooks.md). Receivers reject timestamps older than
// their replay window (~5 minutes recommended).
func Sign(secret string, t int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(t, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	return "t=" + strconv.FormatInt(t, 10) + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

// Verify is the receiver-side check used by tests to validate signatures.
func Verify(secret string, t int64, body []byte, signature string) bool {
	expected := Sign(secret, t, body)
	return hmac.Equal([]byte(expected), []byte(signature))
}
