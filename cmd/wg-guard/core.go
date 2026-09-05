package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/install"
)

func runCoreWithHost(args []string, h install.Host, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", i18n.T(i18n.En, "install.cli.core_usage"))
	}
	selector := args[0]
	if selector == "exact" {
		if len(args) != 2 {
			return fmt.Errorf("%s", i18n.T(i18n.En, "install.cli.core_exact"))
		}
		selector = args[1]
	} else if len(args) != 1 {
		return fmt.Errorf("%s", i18n.T(i18n.En, "install.cli.core_arguments"))
	}
	if selector == "installed" {
		r, err := install.InspectInstalledCore(context.Background(), h)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(r)
	}
	b, err := install.SelectCore(selector)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(b)
}

func runCore(args []string) error { return runCoreWithHost(args, install.NewRealHost(), os.Stdout) }

func runTLSCheck(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("%s", i18n.T(i18n.En, "install.cli.tls_arguments"))
	}
	return install.CheckInstalledTLS(context.Background(), install.NewRealHost(), 90*time.Second)
}
