package backup

import (
	"context"
	"os"
	"path/filepath"
)

func (s *Service) telegram(ctx context.Context) (*TelegramSink, error) {
	token, err := s.Reg.GetSecret(ctx, "backup.telegram_token")
	if err != nil {
		return nil, safetyError("telegram_credentials", err)
	}
	chat, err := s.Reg.GetString(ctx, "backup.telegram_chat")
	if err != nil || token == "" || chat == "" {
		return nil, safetyError("telegram_unset", err)
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
		return nil, safetyError("regular_archive", nil)
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
		r.Warnings = append(r.Warnings, warning("plaintext"))
	}
	if st.Size() >= telegramWarnSize {
		r.Warnings = append(r.Warnings, warning("telegram_near"))
	}
	if err := tg.Deliver(ctx, path, r.Name); err != nil {
		return r, err
	}
	r.Delivered = []string{"telegram"}
	return r, nil
}
