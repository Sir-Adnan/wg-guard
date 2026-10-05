package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	dir, err := s.deliverySpoolDir()
	if err != nil {
		return nil, err
	}
	return &TelegramSink{Token: token, Chat: chat, HTTP: s.HTTPClient, SpoolDir: dir}, nil
}

// Live services already own the node-data lease. Multipart uploads stay on its
// disk volume, rather than competing with AWG's small memory-backed /tmp.
func (s *Service) deliverySpoolDir() (string, error) {
	if s.Cfg == nil || !filepath.IsAbs(s.Cfg.DataDir) {
		return "", fmt.Errorf("backup: private delivery staging is unavailable")
	}
	dir := filepath.Join(s.Cfg.DataDir, "backup-delivery")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("backup: private delivery staging is unavailable")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return "", fmt.Errorf("backup: private delivery staging is unavailable")
	}
	return dir, nil
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
