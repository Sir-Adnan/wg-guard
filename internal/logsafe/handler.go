// Package logsafe provides the only production entry point for structured
// logging. It removes known secret forms before records reach their output
// handler and attaches stable component labels used by operational tooling.
package logsafe

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"time"
)

const Redacted = "[REDACTED]"

// Component is a stable, non-user-controlled log source identifier.
type Component string

const (
	ComponentServe      Component = "serve"
	ComponentHTTP       Component = "http"
	ComponentScheduler  Component = "scheduler"
	ComponentAccounting Component = "accounting"
	ComponentWebhook    Component = "webhook"
	ComponentBackup     Component = "backup"
	ComponentAWG        Component = "awg"
	ComponentNetwork    Component = "network"
)

var components = [...]Component{
	ComponentServe,
	ComponentHTTP,
	ComponentScheduler,
	ComponentAccounting,
	ComponentWebhook,
	ComponentBackup,
	ComponentAWG,
	ComponentNetwork,
}

// Components returns the closed set accepted by log filtering. The returned
// slice is a copy so callers cannot change the shared registry.
func Components() []Component {
	return append([]Component(nil), components[:]...)
}

// ParseComponent accepts only an exact member of the public component set.
func ParseComponent(value string) (Component, bool) {
	component := Component(value)
	return component, component.valid()
}

// WithComponent adds a stable component field. Invalid values are not emitted.
func WithComponent(logger *slog.Logger, component Component) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	if !component.valid() {
		return logger
	}
	return logger.With("component", string(component))
}

func (component Component) valid() bool {
	for _, allowed := range components {
		if component == allowed {
			return true
		}
	}
	return false
}

// New wraps a slog handler with centralized, recursive secret redaction.
func New(next slog.Handler) slog.Handler {
	if next == nil {
		panic("logsafe: nil handler")
	}
	return handler{next: next}
}

type handler struct {
	next        slog.Handler
	forceRedact bool
}

func (h handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h handler) Handle(ctx context.Context, record slog.Record) error {
	clean := slog.NewRecord(record.Time, record.Level, sanitizeText(record.Message), record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		clean.AddAttrs(sanitizeAttr(attr, h.forceRedact))
		return true
	})
	return h.next.Handle(ctx, clean)
}

func (h handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		clean = append(clean, sanitizeAttr(attr, h.forceRedact))
	}
	return handler{next: h.next.WithAttrs(clean), forceRedact: h.forceRedact}
}

func (h handler) WithGroup(name string) slog.Handler {
	return handler{
		next:        h.next.WithGroup(name),
		forceRedact: h.forceRedact || sensitiveGroup(name),
	}
}

func sanitizeAttr(attr slog.Attr, force bool) slog.Attr {
	if attr.Equal(slog.Attr{}) {
		return attr
	}
	key := normalizeKey(attr.Key)
	if (force && !safeMetadataKey(key)) || sensitiveKey(key) {
		return slog.String(attr.Key, Redacted)
	}

	value := attr.Value.Resolve()
	if value.Kind() == slog.KindGroup {
		groupForce := force || sensitiveGroup(key)
		attrs := value.Group()
		clean := make([]slog.Attr, 0, len(attrs))
		for _, child := range attrs {
			clean = append(clean, sanitizeAttr(child, groupForce))
		}
		return slog.Attr{Key: attr.Key, Value: slog.GroupValue(clean...)}
	}

	switch value.Kind() {
	case slog.KindString:
		return slog.String(attr.Key, sanitizeText(value.String()))
	case slog.KindAny:
		return slog.Any(attr.Key, sanitizeAny(value.Any(), 0, make(map[visit]bool)))
	default:
		return slog.Attr{Key: attr.Key, Value: value}
	}
}

func normalizeKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	return strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(key)
}

func safeMetadataKey(key string) bool {
	switch key {
	case "request_id", "token_id", "token_prefix", "node_id", "public_key", "component", "count":
		return true
	default:
		return strings.HasSuffix(key, "_count") || strings.HasSuffix(key, "_key_set")
	}
}

func sensitiveGroup(key string) bool {
	switch normalizeKey(key) {
	case "auth", "authentication", "authorization", "credential", "credentials", "secret", "secrets":
		return true
	default:
		return false
	}
}

func sensitiveKey(key string) bool {
	if safeMetadataKey(key) {
		return false
	}
	switch key {
	case "authorization", "proxy_authorization", "cookie", "set_cookie", "password", "passphrase",
		"csrf", "csrf_token", "session", "session_id", "token", "secret", "credential", "credentials",
		"private_key", "preshared_key", "header_protection_key", "webhook_secret", "telegram_token",
		"telegram_bot_token", "api_token", "access_token", "refresh_token", "backup_password",
		"subscription_token", "capability", "capability_url", "raw_config", "config_text":
		return true
	}
	for _, suffix := range []string{"_password", "_passphrase", "_secret", "_token", "_credential", "_credentials", "_private_key", "_preshared_key"} {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}

func sensitiveQueryKey(key string) bool {
	if sensitiveKey(key) {
		return true
	}
	switch key {
	case "key", "api_key", "client_key":
		return true
	default:
		return false
	}
}

var textRedactors = []struct {
	pattern *regexp.Regexp
	replace string
}{
	{regexp.MustCompile(`(?i)\b(proxy-authorization|authorization)\s*[:=]\s*(bearer|basic)\s+[^\s,;]+`), `$1: $2 ` + Redacted},
	{regexp.MustCompile(`(?i)\b(set-cookie|cookie)\s*[:=]\s*[^\r\n]*`), `$1=` + Redacted},
	{regexp.MustCompile(`(?i)([?&](?:api[_-]?key|client[_-]?key|access[_-]?token|refresh[_-]?token|token|secret|password|passphrase|credential|private[_-]?key)=)[^&#\s]+`), `$1` + Redacted},
	{regexp.MustCompile(`(?i)\b(password|passphrase|private[_ -]?key|preshared[_ -]?key|header[_ -]?protection[_ -]?key|webhook[_ -]?secret|telegram[_ -]?(?:bot[_ -]?)?token|api[_ -]?token|access[_ -]?token|refresh[_ -]?token|secret|token)\s*[:=]\s*(?:"[^"]*"|'[^']*'|[^\s,;&#]+)`), `$1=` + Redacted},
	{regexp.MustCompile(`\bwg_[A-Za-z0-9_-]{16,}\b`), Redacted},
	{regexp.MustCompile(`\b[0-9]{6,12}:[A-Za-z0-9_-]{20,}\b`), Redacted},
	{regexp.MustCompile(`/sub/[A-Za-z0-9_-]{16,}`), `/sub/` + Redacted},
	{regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/@\s:]+:[^@\s/]+@`), `$1` + Redacted + `@`},
}

func sanitizeText(value string) string {
	for _, redactor := range textRedactors {
		value = redactor.pattern.ReplaceAllString(value, redactor.replace)
	}
	return value
}

type visit struct {
	typ reflect.Type
	ptr uintptr
}

const (
	maxSanitizeDepth = 8
	maxSanitizeItems = 64
)

func sanitizeAny(value any, depth int, seen map[visit]bool) any {
	if value == nil {
		return nil
	}
	if depth >= maxSanitizeDepth {
		return Redacted
	}
	switch typed := value.(type) {
	case error:
		return sanitizeText(typed.Error())
	case fmt.Stringer:
		return sanitizeText(typed.String())
	case []byte:
		return Redacted
	case url.URL:
		return sanitizeURL(typed)
	case *url.URL:
		if typed == nil {
			return nil
		}
		return sanitizeURL(*typed)
	case time.Time, time.Duration:
		return value
	}
	return sanitizeReflect(reflect.ValueOf(value), depth, seen)
}

func sanitizeURL(value url.URL) string {
	if value.User != nil {
		value.User = url.User(Redacted)
	}
	query := value.Query()
	for key := range query {
		if sensitiveQueryKey(normalizeKey(key)) {
			query.Set(key, Redacted)
		}
	}
	value.RawQuery = query.Encode()
	return sanitizeText(value.String())
}

func sanitizeReflect(value reflect.Value, depth int, seen map[visit]bool) any {
	if !value.IsValid() {
		return nil
	}
	if depth >= maxSanitizeDepth {
		return Redacted
	}
	for value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if value.CanInterface() {
		switch typed := value.Interface().(type) {
		case time.Time, time.Duration:
			return typed
		case error:
			return sanitizeText(typed.Error())
		case url.URL:
			return sanitizeURL(typed)
		case fmt.Stringer:
			return sanitizeText(typed.String())
		}
	}

	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		marker := visit{typ: value.Type(), ptr: value.Pointer()}
		if seen[marker] {
			return Redacted
		}
		seen[marker] = true
		defer delete(seen, marker)
		return sanitizeReflect(value.Elem(), depth+1, seen)
	case reflect.Map:
		if value.IsNil() {
			return nil
		}
		if value.Type().Key().Kind() != reflect.String {
			return sanitizeText(fmt.Sprint(value.Interface()))
		}
		marker := visit{typ: value.Type(), ptr: value.Pointer()}
		if seen[marker] {
			return Redacted
		}
		seen[marker] = true
		defer delete(seen, marker)
		limit := min(value.Len(), maxSanitizeItems)
		clean := make(map[string]any, limit+1)
		iterator := value.MapRange()
		for len(clean) < limit && iterator.Next() {
			key := iterator.Key().String()
			if sensitiveKey(normalizeKey(key)) {
				clean[key] = Redacted
				continue
			}
			clean[key] = sanitizeReflect(iterator.Value(), depth+1, seen)
		}
		if value.Len() > limit {
			clean["logsafe_truncated"] = value.Len() - limit
		}
		return clean
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return nil
		}
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return Redacted
		}
		limit := min(value.Len(), maxSanitizeItems)
		clean := make([]any, limit)
		for index := range limit {
			clean[index] = sanitizeReflect(value.Index(index), depth+1, seen)
		}
		return clean
	case reflect.Struct:
		clean := make(map[string]any)
		typ := value.Type()
		limit := min(value.NumField(), maxSanitizeItems)
		for index := range limit {
			field := typ.Field(index)
			if field.PkgPath != "" {
				continue
			}
			name := field.Name
			tag := strings.Split(field.Tag.Get("json"), ",")[0]
			if tag == "-" {
				continue
			}
			if tag != "" {
				name = tag
			}
			if sensitiveKey(normalizeKey(name)) {
				clean[name] = Redacted
				continue
			}
			clean[name] = sanitizeReflect(value.Field(index), depth+1, seen)
		}
		return clean
	case reflect.String:
		return sanitizeText(value.String())
	case reflect.Bool:
		return value.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint()
	case reflect.Float32, reflect.Float64:
		return value.Float()
	default:
		if value.CanInterface() {
			return value.Interface()
		}
		return Redacted
	}
}
