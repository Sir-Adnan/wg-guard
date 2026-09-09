package install

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/admin"
	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
)

type OwnerOptions struct {
	Username, PasswordFile string
	Yes                    bool
	Stdin                  io.Reader
	Stdout                 io.Writer
	Locale                 i18n.Locale
	Result                 *OwnerResult
}

// OwnerResult carries only the one-time interactive handoff required by the
// final success screen. It is never written to lifecycle state or journals.
type OwnerResult struct {
	Username          string
	GeneratedPassword string
	Created           bool
	Reused            bool
}

// OwnerInput is the bounded stdin protocol of the local bootstrap command.
// It is never stored in installer state, journal or argument vectors.
type OwnerInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// BootstrapLocalOwner executes the shared admin service on the host and shared
// data volume before the public listener starts, under the caller's lifecycle lock.
func BootstrapLocalOwner(ctx context.Context, h Host, p Plan, o OwnerOptions) error {
	ui := terminal.New(o.Stdin, o.Stdout, terminal.Detect(o.Stdin, o.Stdout, o.Locale))
	if p, ok := o.Stdout.(*progressOutput); ok {
		ui = p.ui
	}
	ui.Context = ctx
	args := []string{BinPath, "owner-bootstrap", "--config", p.BootConfigPath()}
	result, err := h.Output(ctx, append(append([]string{}, args...), "--check"), 30*time.Second)
	if err != nil {
		return fmt.Errorf("%s", ui.T("owner.check_failed"))
	}
	switch strings.TrimSpace(result) {
	case "present":
		if o.Result != nil {
			o.Result.Reused = true
		}
		ui.Text(ui.T("owner.reused"))
		return nil
	case "absent":
	default:
		return fmt.Errorf("%s", ui.T("owner.check_failed"))
	}
	username := strings.TrimSpace(o.Username)
	password := ""
	generatedPassword := ""
	interactive := o.PasswordFile == "" && !o.Yes
	if interactive {
		ui.Section(ui.T("owner.title"))
	}
	if username == "" {
		if !interactive {
			username = "admin"
		} else {
			for {
				username, err = ui.Ask(ui.T("owner.username"), "admin")
				if err != nil {
					return err
				}
				if admin.ValidateUsername(username) == nil {
					break
				}
				ui.Warning(ui.T("owner.username_invalid"))
			}
		}
	} else if err := admin.ValidateUsername(username); err != nil {
		return fmt.Errorf("%s", ui.T("owner.username_invalid"))
	}
	if o.PasswordFile != "" {
		password, err = ReadProtectedPassword(h, o.PasswordFile)
	} else if o.Yes {
		return fmt.Errorf("%s", ui.T("owner.required"))
	} else {
		for {
			password, err = ui.Secret(ui.T("owner.password"))
			if err != nil {
				return err
			}
			if password == "" {
				password, err = generateOwnerPassword()
				if err != nil {
					return fmt.Errorf("%s", ui.T("owner.generate_failed"))
				}
				generatedPassword = password
				break
			}
			if auth.ValidatePassword(password) != nil {
				ui.Warning(ui.T("owner.password_short", auth.MinPasswordLength))
				continue
			}
			confirm, e := ui.Secret(ui.T("owner.confirm"))
			if e != nil {
				return e
			}
			if confirm != password {
				ui.Warning(ui.T("owner.mismatch"))
				continue
			}
			break
		}
	}
	if err != nil {
		return err
	}
	if auth.ValidatePassword(password) != nil {
		return fmt.Errorf("%s", ui.T("owner.password_short", auth.MinPasswordLength))
	}
	payload, err := json.Marshal(OwnerInput{Username: username, Password: password})
	if err != nil {
		return err
	}
	defer clear(payload)
	if err := h.RunWithInput(ctx, append(args, "--stdin"), bytes.NewReader(payload), 2*time.Minute); err != nil {
		return fmt.Errorf("%s", ui.T("owner.failed"))
	}
	if o.Result != nil {
		o.Result.Username = username
		o.Result.GeneratedPassword = generatedPassword
		o.Result.Created = true
	}
	ui.Text(ui.T("owner.ready"))
	return nil
}

func generateOwnerPassword() (string, error) {
	raw := make([]byte, 18) // 144 bits; RawURL encoding is exactly 24 characters.
	defer clear(raw)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func ReadProtectedPassword(h Host, path string) (string, error) {
	// Real host path validation rejects symlinks before opening privileged input.
	if _, ok := h.(realHost); ok {
		if err := safeHostPath(path); err != nil {
			return "", terminalError("owner.file")
		}
	}
	info, err := h.Stat(path)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", terminalError("owner.file")
	}
	f, err := h.Open(path)
	if err != nil {
		return "", terminalError("owner.file")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(b) > 4096 {
		return "", terminalError("owner.file")
	}
	password := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	clear(b)
	if password == "" || strings.ContainsAny(password, "\r\n\x00") {
		return "", terminalError("owner.file")
	}
	return password, nil
}
