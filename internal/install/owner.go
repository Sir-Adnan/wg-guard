package install

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
)

type OwnerOptions struct {
	Username, PasswordFile string
	Yes                    bool
	Stdin                  io.Reader
	Stdout                 io.Writer
	Locale                 i18n.Locale
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
		ui.Text(ui.T("owner.reused"))
		return nil
	case "absent":
	default:
		return fmt.Errorf("%s", ui.T("owner.check_failed"))
	}
	username := o.Username
	password := ""
	if o.PasswordFile != "" {
		password, err = ReadProtectedPassword(h, o.PasswordFile)
	} else if o.Yes {
		return fmt.Errorf("%s", ui.T("owner.required"))
	} else {
		ui.Section(ui.T("owner.title"))
		if username == "" {
			username, err = ui.Ask(ui.T("owner.username"), "owner")
			if err != nil {
				return err
			}
		}
		password, err = ui.Secret(ui.T("owner.password"))
		if err != nil {
			return err
		}
		confirm, e := ui.Secret(ui.T("owner.confirm"))
		if e != nil {
			return e
		}
		if confirm != password {
			return fmt.Errorf("%s", ui.T("owner.mismatch"))
		}
	}
	if err != nil {
		return err
	}
	if username == "" {
		username = "owner"
	}
	payload, err := json.Marshal(OwnerInput{Username: username, Password: password})
	if err != nil {
		return err
	}
	defer clear(payload)
	if err := h.RunWithInput(ctx, append(args, "--stdin"), bytes.NewReader(payload), 2*time.Minute); err != nil {
		return fmt.Errorf("%s", ui.T("owner.failed"))
	}
	ui.Text(ui.T("owner.ready"))
	return nil
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
