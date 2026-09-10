package logsafe

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHandlerRedactsSecretCorpusFromTextAndJSON(t *testing.T) {
	secrets := map[string]string{
		"password":  "Synthetic-Pass-9384!",
		"bearer":    "bearer.synthetic.secret.value",
		"api_token": "wg_0123456789abcdefghijklmnopqrstuv",
		"telegram":  "1234567890:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi",
		"private":   "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"webhook":   "synthetic-webhook-secret-0123456789",
		"sub":       "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN0123456789_-",
		"userinfo":  "url-user:synthetic-url-password",
		"query":     "synthetic-query-secret",
		"api_key":   "synthetic-api-key-secret",
		"nested":    "synthetic-nested-secret",
	}
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			var out bytes.Buffer
			var base slog.Handler
			if format == "json" {
				base = slog.NewJSONHandler(&out, nil)
			} else {
				base = slog.NewTextHandler(&out, nil)
			}
			logger := slog.New(New(base))
			logger.Info("request Authorization: Bearer "+secrets["bearer"]+" token "+secrets["api_token"],
				"password", secrets["password"],
				"token_id", "tok-safe-id",
				"path", "/var/lib/wg-guard",
				slog.Group("nested",
					slog.String("telegram_bot_token", secrets["telegram"]),
					slog.String("url", "https://"+secrets["userinfo"]+"@example.test/hook?token="+secrets["query"]+"&api_key="+secrets["api_key"]),
				),
				slog.Any("payload", map[string]any{
					"webhook_secret": secrets["webhook"],
					"count":          3,
					"nested":         map[string]any{"err": errors.New("nested failed token=" + secrets["nested"])},
				}),
				slog.String("callback_url", "https://example.test/hook?api_key="+secrets["api_key"]),
				slog.Any("err", errors.New("apply failed: PrivateKey = "+secrets["private"]+" /sub/"+secrets["sub"])),
			)
			logger.With("webhook_secret", secrets["webhook"]).WithGroup("request").
				Info("callback complete", "count", 4)
			body := out.String()
			for name, secret := range secrets {
				if strings.Contains(body, secret) {
					t.Errorf("%s output leaked %s", format, name)
				}
			}
			for _, useful := range []string{"tok-safe-id", "/var/lib/wg-guard", "count", "callback complete", "nested failed", Redacted} {
				if !strings.Contains(body, useful) {
					t.Errorf("%s output lost useful value %q:\n%s", format, useful, body)
				}
			}
		})
	}
}

func TestHandlerRedactsSensitiveWithAttrsAndGroups(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(New(slog.NewTextHandler(&out, nil)).
		WithAttrs([]slog.Attr{slog.String("api_token", "wg_abcdefghijklmnopqrstuv0123456789")})).
		WithGroup("credentials")
	logger.Info("finished", "value", "plain-secret-without-marker", "request_id", "req-safe")
	body := out.String()
	if strings.Contains(body, "wg_abcdefghijklmnopqrstuv0123456789") || strings.Contains(body, "plain-secret-without-marker") {
		t.Fatalf("pre-bound/group secret leaked: %s", body)
	}
	if !strings.Contains(body, "req-safe") {
		t.Fatalf("safe request id removed: %s", body)
	}
}

func TestHandlerBoundsCyclicValues(t *testing.T) {
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	var out bytes.Buffer
	logger := slog.New(New(slog.NewJSONHandler(&out, nil)))
	logger.Info("cycle", "payload", cyclic)
	if !strings.Contains(out.String(), Redacted) {
		t.Fatalf("cycle was not safely terminated: %s", out.String())
	}
}

func TestHandlerHonorsHiddenStructFields(t *testing.T) {
	value := struct {
		Visible string `json:"visible"`
		Hidden  string `json:"-"`
	}{Visible: "safe", Hidden: "unmarked-hidden-value"}
	var out bytes.Buffer
	slog.New(New(slog.NewJSONHandler(&out, nil))).Info("struct", "payload", value)
	if strings.Contains(out.String(), value.Hidden) || !strings.Contains(out.String(), value.Visible) {
		t.Fatalf("hidden field handling failed: %s", out.String())
	}
}

type handlerProbe struct {
	mu      sync.Mutex
	enabled bool
	ctx     context.Context
	level   slog.Level
	record  slog.Record
	err     error
	attrs   []slog.Attr
	groups  []string
}

type probeHandler struct {
	probe  *handlerProbe
	attrs  []slog.Attr
	groups []string
}

func (h probeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	h.probe.mu.Lock()
	defer h.probe.mu.Unlock()
	h.probe.ctx, h.probe.level = ctx, level
	return h.probe.enabled
}

func (h probeHandler) Handle(ctx context.Context, record slog.Record) error {
	h.probe.mu.Lock()
	defer h.probe.mu.Unlock()
	h.probe.ctx, h.probe.record = ctx, record.Clone()
	h.probe.attrs = append([]slog.Attr(nil), h.attrs...)
	h.probe.groups = append([]string(nil), h.groups...)
	return h.probe.err
}

func (h probeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := h
	clone.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return clone
}

func (h probeHandler) WithGroup(name string) slog.Handler {
	clone := h
	clone.groups = append(append([]string(nil), h.groups...), name)
	return clone
}

func TestHandlerPreservesSlogContractAndUnderlyingError(t *testing.T) {
	sentinel := errors.New("underlying failure")
	probe := &handlerProbe{enabled: true, err: sentinel}
	wrapped := New(probeHandler{probe: probe})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !wrapped.Enabled(ctx, slog.LevelWarn) || probe.ctx != ctx || probe.level != slog.LevelWarn {
		t.Fatal("Enabled was not delegated exactly")
	}

	at := time.Unix(1_700_000_000, 123)
	record := slog.NewRecord(at, slog.LevelError, "password=hidden-value", 12345)
	record.AddAttrs(slog.String("request_id", "req-1"))
	err := wrapped.WithAttrs([]slog.Attr{slog.String("secret", "bound-hidden")}).
		WithGroup("http").Handle(ctx, record)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Handle error = %v", err)
	}
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if probe.ctx != ctx || probe.record.Time != at || probe.record.Level != slog.LevelError || probe.record.PC != 12345 {
		t.Fatalf("record metadata changed: %+v", probe.record)
	}
	if strings.Contains(probe.record.Message, "hidden-value") || !strings.Contains(probe.record.Message, Redacted) {
		t.Fatalf("message not sanitized: %q", probe.record.Message)
	}
	if len(probe.attrs) != 1 || probe.attrs[0].Value.String() != Redacted {
		t.Fatalf("WithAttrs not sanitized: %+v", probe.attrs)
	}
	if len(probe.groups) != 1 || probe.groups[0] != "http" {
		t.Fatalf("groups changed: %v", probe.groups)
	}
	var requestID string
	probe.record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "request_id" {
			requestID = attr.Value.String()
		}
		return true
	})
	if requestID != "req-1" {
		t.Fatalf("safe record attrs changed: %q", requestID)
	}
}

func TestComponentLoggerUsesClosedStableValue(t *testing.T) {
	for _, component := range Components() {
		parsed, ok := ParseComponent(string(component))
		if !ok || parsed != component {
			t.Errorf("ParseComponent(%q) = %q, %v", component, parsed, ok)
		}
		var out bytes.Buffer
		WithComponent(slog.New(New(slog.NewTextHandler(&out, nil))), component).Info("event")
		if !strings.Contains(out.String(), "component="+string(component)) {
			t.Errorf("component %q missing: %s", component, out.String())
		}
	}
	for _, invalid := range []string{"", "HTTP", "http ", "database", "http --follow"} {
		if _, ok := ParseComponent(invalid); ok {
			t.Errorf("ParseComponent accepted %q", invalid)
		}
	}
}
