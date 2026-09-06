package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/install"
)

func runCoreWithHost(args []string, h install.Host, out io.Writer) error {
	return runCoreWithHostContext(context.Background(), args, h, out)
}
func runCoreWithHostContext(ctx context.Context, args []string, h install.Host, out io.Writer) error {
	if len(args) > 0 && args[0] == "switch" {
		if len(args) != 3 || args[2] != "--confirm-impact" {
			return fmt.Errorf("%s", i18n.T(i18n.En, "install.error.core_confirmation"))
		}
		r, err := install.SwitchCore(ctx, h, install.CoreSwitchOptions{Selector: args[1], ConfirmImpact: true, Stdout: out})
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(r)
	}
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

func runCore(args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	return runCoreWithHostContext(ctx, args, install.NewRealHost(), os.Stdout)
}

func runTLSCheck(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("%s", i18n.T(i18n.En, "install.cli.tls_arguments"))
	}
	return install.CheckInstalledTLS(context.Background(), install.NewRealHost(), 90*time.Second)
}
