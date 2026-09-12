package web

import (
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
)

func (v *View) HasChoice(values []string, value string) bool {
	for _, s := range values {
		if s == value {
			return true
		}
	}
	return false
}
func (v *View) UnknownChoices(values []string) []string {
	var out []string
	for _, s := range values {
		if auth.ValidateScopes([]string{s}) != nil {
			out = append(out, s)
		}
	}
	return out
}
func (v *View) OpsLabel(kind, value string) string {
	key := "ops." + kind + "." + value
	label := v.T(key)
	if label == key {
		return value
	}
	return label
}
func (v *View) ScopeLabel(value string) string {
	if strings.HasSuffix(value, ".*") {
		return v.T("ops.scope.family_all", v.OpsLabel("family", strings.TrimSuffix(value, ".*")))
	}
	return v.OpsLabel("scope", value)
}
func (v *View) ScopeDescription(value string) string {
	if strings.HasSuffix(value, ".*") {
		return v.T("ops.scope.family_hint")
	}
	return v.OpsLabel("scope_help", value)
}
func (v *View) OpsTimestamp(t time.Time) string {
	return i18n.FormatDateTime(v.Locale, t.UTC(), nil) + t.UTC().Format(":05") + " UTC"
}
func (v *View) TokenExpired(t *time.Time) bool { return t != nil && !t.After(time.Now()) }

// CompactAuditID shortens UUID-shaped identifiers only. Human names, addresses
// and other meaningful target values retain their exact presentation.
func (v *View) CompactAuditID(value string) string {
	if len(value) != 36 {
		return value
	}
	for i, c := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return value
			}
		} else if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return value
		}
	}
	return value[:8] + "…" + value[32:]
}

func (v *View) DeliveryError(message string) string {
	if raw, ok := strings.CutPrefix(message, "receiver returned HTTP "); ok {
		if status, err := strconv.Atoi(raw); err == nil && status >= 100 && status <= 599 {
			return v.T("ops.delivery_http", status)
		}
	}
	if strings.Contains(strings.ToLower(message), "timeout") || strings.Contains(message, "deadline exceeded") {
		return v.T("ops.delivery_timeout")
	}
	return v.T("ops.delivery_failed")
}
