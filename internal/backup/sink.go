package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Sink is one off-box delivery target. The interface leaves room for future
// sinks (S3, …) without redesign (docs/operations/backup-restore.md).
type Sink interface {
	Name() string
	Deliver(ctx context.Context, archivePath, filename string) error
}

// Telegram limits (Bot API sendDocument accepts up to 50 MB).
const (
	telegramMaxUpload = 50 << 20
	telegramWarnSize  = 45 << 20
	telegramAPIBase   = "https://api.telegram.org"
)

// TelegramSink delivers archives via the Bot API sendDocument method.
// The token never appears in errors or logs — failures report the HTTP
// status only; URL errors and remote descriptions are never exposed.
type TelegramSink struct {
	Token string
	Chat  string
	HTTP  HTTPDoer // nil = default client with httpTimeout
}

func (t *TelegramSink) Name() string { return "telegram" }

// Deliver uploads one archive. The multipart body is assembled on disk (the
// boundary is only known after the writer closes; streaming through a pipe
// would race the header write) — temp file, never memory.
func (t *TelegramSink) Deliver(ctx context.Context, archivePath, filename string) error {
	st, err := os.Stat(archivePath)
	if err != nil {
		return fmt.Errorf("stat archive: %w", err)
	}
	if st.Size() >= telegramMaxUpload {
		return fmt.Errorf("archive is %d MB — over the Telegram Bot API 50 MB sendDocument limit", st.Size()>>20)
	}

	src, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer src.Close()

	body, err := os.CreateTemp("", "wgg-tg-*.body")
	if err != nil {
		return fmt.Errorf("temp body: %w", err)
	}
	defer os.Remove(body.Name())
	defer body.Close()

	mw := multipart.NewWriter(body)
	if err := mw.WriteField("chat_id", t.Chat); err != nil {
		return fmt.Errorf("multipart: %w", err)
	}
	part, err := mw.CreateFormFile("document", filename)
	if err != nil {
		return fmt.Errorf("multipart: %w", err)
	}
	if n, err := io.Copy(part, io.LimitReader(restoreReader{ctx, src}, telegramMaxUpload)); err != nil {
		return fmt.Errorf("multipart: %w", err)
	} else if n >= telegramMaxUpload {
		return fmt.Errorf("telegram: archive exceeds upload limit")
	}
	if err := mw.Close(); err != nil {
		return fmt.Errorf("multipart: %w", err)
	}
	size, err := body.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return err
	}

	client := t.HTTP
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}
	reqCtx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		telegramAPIBase+"/bot"+t.Token+"/sendDocument", body)
	if err != nil {
		return fmt.Errorf("telegram: could not build upload request")
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: upload transport failed")
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("telegram: response could not be read")
	}
	var out struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return fmt.Errorf("unexpected response (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || !out.OK {
		// Remote descriptions are untrusted and may echo credentials or file
		// contents. Status is sufficient for an operator-visible failure.
		return fmt.Errorf("telegram: delivery rejected (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// TestDelivery sends a tiny probe document so operators can verify the
// credentials without waiting for a scheduled archive.
func (t *TelegramSink) TestDelivery(ctx context.Context) error {
	dir, err := os.MkdirTemp("", "wgg-tg-test")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	probe := filepath.Join(dir, "wg-guard-telegram-test.txt")
	if err := os.WriteFile(probe, []byte("WG-Guard telegram delivery test "+time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		return err
	}
	return t.Deliver(ctx, probe, "wg-guard-telegram-test.txt")
}
