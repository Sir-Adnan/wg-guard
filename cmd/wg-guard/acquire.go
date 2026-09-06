package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/version"
	"io"
	"os"
	"path/filepath"
)

func lifecycleArgsError() error { return fmt.Errorf("%s", i18n.T(i18n.En, "install.cli.sources")) }
func sourceSelection(release, commit string) (distribution.Selection, error) {
	if release != "" && commit != "" {
		return distribution.Selection{}, lifecycleArgsError()
	}
	if release != "" {
		return distribution.Selection{Channel: "release", Ref: release}, nil
	}
	if commit != "" {
		return distribution.Selection{Channel: "commit", Ref: commit}, nil
	}
	return distribution.Selection{}, nil
}

func prepareBuild(ctx context.Context, s distribution.Selection, metadata string) (distribution.Build, string, func(), error) {
	parent, err := os.MkdirTemp("", "wg-guard-lifecycle-")
	if err != nil {
		return distribution.Build{}, "", func() {}, err
	}
	child := ""
	cleanup := func() {
		if child != "" && filepath.Dir(child) == parent {
			_ = os.RemoveAll(child)
		}
		_ = os.Remove(parent)
	}
	var b distribution.Build
	if s.Channel != "" {
		b, err = distribution.NewClient(nil, distribution.Options{}).Acquire(ctx, s, parent)
		if err == nil {
			child = filepath.Dir(b.BinaryPath)
		}
	} else if metadata != "" {
		var f *os.File
		f, err = os.Open(metadata)
		if err == nil {
			raw, e := io.ReadAll(io.LimitReader(f, 8193))
			f.Close()
			err = e
			if len(raw) > 8192 {
				err = lifecycleArgsError()
			}
			if err == nil {
				err = json.Unmarshal(raw, &b)
			}
		}
	} else {
		b = distribution.Build{Channel: "local", Commit: version.Commit, Version: version.Version}
		b.BinaryPath, err = os.Executable()
		if err == nil {
			var f *os.File
			f, err = os.Open(b.BinaryPath)
			if err == nil {
				hash := sha256.New()
				_, err = io.Copy(hash, io.LimitReader(f, 256<<20+1))
				f.Close()
				b.SHA256 = hex.EncodeToString(hash.Sum(nil))
			}
		}
	}
	if err != nil {
		cleanup()
		return distribution.Build{}, "", func() {}, err
	}
	return b, parent, cleanup, nil
}
