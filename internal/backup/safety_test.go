package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"filippo.io/age"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type canceledTelegramTransport struct{}

func (canceledTelegramTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, context.Canceled
}
func TestTelegramSafetyLocalizationPreservesCancellation(t *testing.T) {
	sink := &TelegramSink{Token: "123:synthetic-secret", Chat: "-1001", HTTP: &http.Client{Transport: canceledTelegramTransport{}}}
	err := sink.TestDelivery(context.Background())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation cause lost: %v", err)
	}
	message := err
	if !errors.Is(message, context.Canceled) {
		t.Fatal("keyed error lost cause")
	}
	for _, locale := range []i18n.Locale{i18n.Fa, i18n.En} {
		got := ErrorText(message, locale)
		if strings.Contains(got, sink.Token) {
			t.Fatal("localized error leaked token")
		}
		want := "transport failed"
		if locale == i18n.Fa {
			want = "ناموفق"
		}
		if !strings.Contains(got, want) {
			t.Fatalf("untranslated safety body: %s", got)
		}
	}
}

func TestRestoreRejectsExcessiveScryptWorkBeforeKDF(t *testing.T) {
	r, err := age.NewScryptRecipient("synthetic-password")
	if err != nil {
		t.Fatal(err)
	}
	r.SetWorkFactor(1) // Cheap valid header; only its advertised factor is adversarial.
	var b bytes.Buffer
	w, err := age.Encrypt(&b, r)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	raw := strings.Replace(b.String(), " 1\n", " 19\n", 1)
	if raw == b.String() {
		t.Fatal("test did not alter work factor")
	}
	_, err = ageDecrypt(strings.NewReader(raw), "synthetic-password")
	if err == nil || !strings.Contains(err.Error(), "work factor too large") {
		t.Fatalf("expected pre-KDF work-factor refusal, got %v", err)
	}
}

func TestPasswordReadFailureNeverCreatesPlaintext(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	if err := s.Reg.SetRaw(ctx, "backup.password", "synthetic-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE settings SET value='corrupt-envelope' WHERE key='backup.password'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, CreateOpts{Deliver: true}); err == nil {
		t.Fatal("failed password decryption silently created archive")
	}
	arcs, err := s.List()
	if err != nil || len(arcs) != 0 {
		t.Fatal("archive created after failed password read")
	}
}

func TestCreateSameSecondRetainsDistinctArchives(t *testing.T) {
	s, _ := newService(t)
	fixed := s.now()
	s.Now = func() time.Time { return fixed }
	a, err := s.Create(context.Background(), CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create(context.Background(), CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if a.Path == b.Path {
		t.Fatal("a second backup overwrote the prior archive")
	}
}

func TestExplicitSendDeliversSelectedArchiveToNegativeChat(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	a, err := s.Create(ctx, CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("chat_id") != "-1001234567890" {
			t.Error("negative chat ID altered")
		}
		f, h, err := r.FormFile("document")
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		got, err := io.ReadAll(f)
		if err != nil || !bytes.Equal(got, want) || h.Filename != a.Name {
			t.Error("explicit send selected different archive bytes")
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()
	s.HTTPClient = routingClient{base: ts.URL, inner: ts.Client()}
	if err := s.Reg.SetRaw(ctx, "backup.telegram_token", "12345:synthetic-token"); err != nil {
		t.Fatal(err)
	}
	if err := s.Reg.SetRaw(ctx, "backup.telegram_chat", "-1001234567890"); err != nil {
		t.Fatal(err)
	}
	result, err := s.Send(ctx, a.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Delivered) != 1 || len(result.Warnings) == 0 || requests != 1 {
		t.Fatal("delivery result or plaintext warning missing")
	}
	if !strings.Contains(result.Warnings[0].Localized(i18n.Fa), "اسرار خواندنی") {
		t.Fatal("service did not retain keyed plaintext warning")
	}
	arcs, err := s.List()
	if err != nil || len(arcs) != 1 {
		t.Fatal("explicit send created or removed an archive")
	}
}

func TestBackupSettingsObserveOtherProcess(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	if _, err := s.Reg.GetString(ctx, "backup.telegram_chat"); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.LoadKeyRing(s.Cfg.MasterKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	other, err := settings.New(s.DB, ring, settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if err := other.SetRaw(ctx, "backup.telegram_chat", "-1001234567890"); err != nil {
		t.Fatal(err)
	}
	chat, err := s.Reg.GetString(ctx, "backup.telegram_chat")
	if err != nil || chat != "-1001234567890" {
		t.Fatal("running backup service retained stale settings")
	}
}

func TestRestoreRejectsUnsafeMembersAndMissingHashes(t *testing.T) {
	for _, kind := range []string{"traversal", "duplicate", "symlink", "missing-hash", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			s, dir := newService(t)
			snapshot := filepath.Join(dir, "snap")
			if err := s.snapshotDB(context.Background(), snapshot); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			manifest := Manifest{Schema: 1, Files: map[string]string{DBMember: hashBytes(data)}}
			if kind == "missing-hash" {
				manifest.Files = map[string]string{}
			}
			raw, _ := json.Marshal(manifest)
			path := filepath.Join(dir, "unsafe.wgg")
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(f)
			tw := tar.NewWriter(gz)
			name := DBMember
			if kind == "traversal" {
				name = "../" + name
			}
			hdr := &tar.Header{Name: name, Size: int64(len(data)), Mode: 0600}
			if kind == "symlink" {
				hdr.Typeflag = tar.TypeSymlink
				hdr.Linkname = "db.sqlite"
				hdr.Size = 0
			}
			if kind == "oversize" {
				hdr.Name = ConfigMember
				hdr.Size = 2 << 20
				data = make([]byte, 2<<20)
			}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatal(err)
			}
			if hdr.Size > 0 {
				if _, err := tw.Write(data); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "duplicate" {
				tw.WriteHeader(hdr)
				tw.Write(data)
			}
			tw.WriteHeader(&tar.Header{Name: ManifestName, Size: int64(len(raw)), Mode: 0600})
			tw.Write(raw)
			tw.Close()
			gz.Close()
			f.Close()
			if _, _, err := s.Stage(context.Background(), path, ""); err == nil {
				t.Fatal("unsafe archive accepted")
			} else if kind == "oversize" && !strings.Contains(err.Error(), "oversized archive member") {
				t.Fatalf("oversized member not rejected at its header: %v", err)
			}
			if p, _ := s.Pending(); p != nil {
				t.Fatal("unsafe archive published")
			}
		})
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("synthetic transport failure")
}

type tokenEchoTransport struct{}

func (tokenEchoTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"ok":false,"description":"echo ` + r.URL.Path + `"}`)), Header: make(http.Header), Request: r}, nil
}

func TestTelegramErrorsNeverExposeToken(t *testing.T) {
	for _, transport := range []http.RoundTripper{failingTransport{}, tokenEchoTransport{}} {
		sink := &TelegramSink{Token: "123456:synthetic_secret_only", Chat: "-1001234567890", HTTP: &http.Client{Transport: transport}}
		err := sink.TestDelivery(context.Background())
		if err == nil {
			t.Fatal("expected delivery error")
		}
		if strings.Contains(err.Error(), sink.Token) || strings.Contains(err.Error(), "synthetic_secret_only") {
			t.Fatal("delivery error exposed synthetic credential")
		}
		jsonLog, marshalErr := json.Marshal([]Message{warning("telegram_failed", err)})
		if marshalErr != nil || strings.Contains(string(jsonLog), sink.Token) {
			t.Fatal("structured warning log leaked transport cause")
		}
	}
}

func TestRestorePreviewCannotApplyOnBoot(t *testing.T) {
	s, _ := newService(t)
	arc, err := s.Create(context.Background(), CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Stage(context.Background(), arc.Path, ""); err != nil {
		t.Fatal(err)
	}
	p, err := s.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if p != nil {
		t.Fatal("unconfirmed preview is visible to boot restore consumer")
	}
}
