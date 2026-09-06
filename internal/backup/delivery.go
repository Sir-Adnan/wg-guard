package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func (s *Service) telegram(ctx context.Context) (*TelegramSink, error) {
	token, err := s.Reg.GetSecret(ctx, "backup.telegram_token")
	if err != nil {
		return nil, fmt.Errorf("backup: Telegram credentials could not be read")
	}
	chat, err := s.Reg.GetString(ctx, "backup.telegram_chat")
	if err != nil || token == "" || chat == "" {
		return nil, fmt.Errorf("backup: Telegram is not configured")
	}
	return &TelegramSink{Token: token, Chat: chat, HTTP: s.HTTPClient}, nil
}
func (s *Service) TestTelegram(ctx context.Context) error {
	tg, err := s.telegram(ctx)
	if err != nil {
		return err
	}
	return tg.TestDelivery(ctx)
}

// Send explicitly delivers one existing archive; it neither creates nor prunes.
func (s *Service) Send(ctx context.Context, path string) (*Result, error) {
	tg, err := s.telegram(ctx)
	if err != nil {
		return nil, err
	}
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("backup: archive must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	kind, _, err := sniffContainer(f)
	f.Close()
	if err != nil {
		return nil, err
	}
	r := &Result{Name: filepath.Base(path), Path: path, Size: st.Size(), Encrypted: kind == "age"}
	if !r.Encrypted {
		r.Warnings = append(r.Warnings, "UNENCRYPTED off-host archive: set a backup password; this file contains readable node secrets")
	}
	if st.Size() >= telegramWarnSize {
		r.Warnings = append(r.Warnings, "archive is near the Telegram upload limit")
	}
	if err := tg.Deliver(ctx, path, r.Name); err != nil {
		return r, err
	}
	r.Delivered = []string{"telegram"}
	return r, nil
}
