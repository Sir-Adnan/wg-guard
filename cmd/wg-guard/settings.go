package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/doctor"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/shaper"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/amneziawg"
)

// runSettings reads and writes the runtime settings registry (the same
// keys the panel and the API manage). Secrets are never printed — `get`
// answers "<set>"/"<unset>" so verification never puts a secret on screen.
//
//	wg-guard settings list
//	wg-guard settings get node.endpoint
//	wg-guard settings set backup.telegram_chat 123456789
//	echo SECRET | wg-guard settings set backup.telegram_token -stdin
func runSettings(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: wg-guard settings <list|get|set> [args] [-config PATH]")
	}
	var configPath = "/etc/wg-guard/wg-guard.toml"
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == "-config" || args[i] == "--config" {
			i++
			configPath = args[i]
			continue
		}
		rest = append(rest, args[i])
	}
	if len(rest) == 0 {
		return fmt.Errorf("usage: wg-guard settings <list|get|set> [args] [-config PATH]")
	}

	env, err := loadCLIEnv(configPath)
	if err != nil {
		return err
	}
	defer env.Close()
	ctx := context.Background()

	switch rest[0] {
	case "list":
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "KEY\tVALUE")
		defs := env.Reg.Definitions()
		sort.Slice(defs, func(i, j int) bool { return defs[i].Key < defs[j].Key })
		for _, def := range defs {
			value := "<set>"
			if !def.Secret {
				v, err := env.Reg.Get(ctx, def.Key)
				if err != nil {
					value = "?"
				} else {
					value = settingsDisplay(v)
				}
			} else if s, err := env.Reg.GetSecret(ctx, def.Key); err == nil && s == "" {
				value = "<unset>"
			}
			fmt.Fprintf(tw, "%s\t%s\n", def.Key, value)
		}
		return tw.Flush()
	case "get":
		if len(rest) < 2 {
			return fmt.Errorf("usage: wg-guard settings get KEY")
		}
		key := rest[1]
		for _, def := range env.Reg.Definitions() {
			if def.Key != key {
				continue
			}
			if def.Secret {
				s, err := env.Reg.GetSecret(ctx, key)
				if err != nil {
					return err
				}
				if s == "" {
					fmt.Println("<unset>")
				} else {
					fmt.Println("<set>")
				}
				return nil
			}
			v, err := env.Reg.Get(ctx, key)
			if err != nil {
				return err
			}
			fmt.Println(settingsDisplay(v))
			return nil
		}
		return domain.E(domain.CodeSettingUnknown, "unknown setting %q", key)
	case "set":
		if len(rest) < 3 {
			return fmt.Errorf("usage: wg-guard settings set KEY VALUE (or: echo SECRET | wg-guard settings set KEY -stdin)")
		}
		key, value := rest[1], rest[2]
		if value == "-stdin" {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read value from stdin: %w", err)
			}
			// Strip exactly one trailing newline; the value itself is
			// untouched (secrets must not pass through argv).
			value = strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
		}
		if err := env.Reg.SetRaw(ctx, key, value); err != nil {
			return err
		}
		_ = audit.NewService(env.DB).Record(ctx, audit.Entry{
			ActorType: audit.ActorAdmin, ActorID: "cli",
			Action: "settings.updated", Target: key,
			Metadata: map[string]any{"source": "cli"},
		})
		fmt.Printf("%s updated\n", key)
		return nil
	default:
		return fmt.Errorf("unknown settings command %q", rest[0])
	}
}

func settingsDisplay(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return fmt.Sprintf("%d", x)
	case bool:
		return fmt.Sprintf("%v", x)
	case []string:
		return fmt.Sprintf("%s", x)
	default:
		return fmt.Sprintf("%v", x)
	}
}

// runDoctor executes the health checks (docs/operations/runbook.md). Read-only
// by default; --fix re-runs the boot repairs and requires the service stopped.
//
//	wg-guard doctor [-fix]
func runDoctor(args []string) error {
	var (
		fix        bool
		configPath = "/etc/wg-guard/wg-guard.toml"
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-config", "--config":
			i++
			configPath = args[i]
		case "-fix", "--fix":
			fix = true
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	env, err := loadCLIEnv(configPath)
	if err != nil {
		return err
	}
	defer env.Close()
	ctx := context.Background()

	var backend tunnel.Backend = amneziawg.New(subprocess.NewSystem())
	serviceUp := backup.ServiceRunning(env.Cfg.HTTPListen)
	if fix && serviceUp {
		return fmt.Errorf("doctor --fix refuses to run while the service is up (stop the service first)")
	}

	report, err := doctor.Run(ctx, doctor.Deps{
		Cfg: env.Cfg, ConfigPath: env.ConfigPath,
		DB: env.DB, Reg: env.Reg, Ring: env.Ring,
		Backend: backend, Run: subprocess.NewSystem(),
		Shaper: shaper.New(subprocess.NewSystem()),
		Fix:    fix, ServiceUp: serviceUp,
	})
	if err != nil {
		printReport(report)
		return err
	}
	printReport(report)
	if report.Failures() > 0 {
		return fmt.Errorf("%d check(s) failed", report.Failures())
	}
	return nil
}

func printReport(report *doctor.Report) {
	if report == nil {
		return
	}
	for _, c := range report.Checks {
		mark := map[doctor.Status]string{
			doctor.StatusPass: "pass", doctor.StatusWarn: "WARN",
			doctor.StatusFail: "FAIL", doctor.StatusSkip: "skip",
		}[c.Status]
		line := fmt.Sprintf("%-4s %-14s %s", mark, c.Name, c.Detail)
		fmt.Println(line)
		if c.Remedy != "" && (c.Status == doctor.StatusWarn || c.Status == doctor.StatusFail) {
			fmt.Printf("          remedy: %s\n", c.Remedy)
		}
	}
	if len(report.Fixes) > 0 {
		fmt.Println("\nfixes applied:")
		for _, f := range report.Fixes {
			fmt.Println("  - " + f)
		}
	}
}
