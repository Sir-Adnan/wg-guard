package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
)

func prepareInstallArchive(ctx context.Context, h install.Host, o install.InstallOptions) (*backup.PreparedInstall, error) {
	f, err := h.Open(o.ArchivePath)
	if err != nil {
		return nil, fmt.Errorf("install: backup archive is unreadable")
	}
	var header [32]byte
	n, err := io.ReadFull(f, header[:])
	_ = f.Close()
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("install: backup archive is unreadable")
	}
	encrypted := strings.HasPrefix(string(header[:n]), "age-encryption.org/v1")
	password := ""
	if o.ArchivePasswordFile != "" {
		password, err = install.ReadProtectedPassword(h, o.ArchivePasswordFile)
	} else if o.AskArchivePassword || encrypted && !o.Yes {
		u := terminal.New(o.Stdin, o.Stdout, terminal.Detect(o.Stdin, o.Stdout, i18n.En))
		u.Context = ctx
		password, err = u.Secret("Backup archive password")
	} else if encrypted {
		return nil, fmt.Errorf("install: encrypted backup requires --backup-password-file or hidden --backup-password input")
	}
	if err != nil {
		return nil, err
	}
	_, finish := startUpdateHeartbeat(ctx, o.Stdout, "Validating portable backup and source access")
	prepared, err := backup.PrepareInstall(ctx, o.ArchivePath, password)
	finish(err)
	password = ""
	return prepared, err
}
