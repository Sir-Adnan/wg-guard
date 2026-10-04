package main

import (
	"context"
	"fmt"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/doctor"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/shaper"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
	"time"
)

// Host diagnostics parse CLI input here, then call the shared node/deployment services.
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

	ctx := context.Background()
	host := install.NewRealHost()
	state, stateErr := install.LoadState(host)
	dockerFixSummary := ""
	if stateErr == nil {
		var err error
		fix, dockerFixSummary, err = prepareDoctorFix(ctx, state, configPath, fix, func(ctx context.Context) error {
			return install.Restart(ctx, host)
		})
		if err != nil {
			return err
		}
	}

	env, err := loadCLIEnv(configPath)
	if err != nil {
		return err
	}
	defer env.Close()
	hostRunner := subprocess.NewSystem()
	backend := newDoctorInspector(nil, hostRunner)
	inspector := newDoctorInspector(state, hostRunner)

	serviceUp := backup.ServiceRunning(env.Cfg.HTTPListen)
	if fix && serviceUp {
		return fmt.Errorf("doctor --fix refuses to run while the service is up (stop the service first)")
	}

	report, err := doctor.Run(ctx, doctor.Deps{
		Cfg: env.Cfg, ConfigPath: env.ConfigPath,
		DB: env.DB, Reg: env.Reg, Ring: env.Ring,
		Backend: backend, Inspector: inspector, Run: hostRunner,
		Shaper: shaper.New(hostRunner),
		Fix:    fix, ServiceUp: serviceUp,
	})
	if report != nil {
		if dockerFixSummary != "" {
			report.Fixes = append(report.Fixes, dockerFixSummary)
		}
		if stateErr != nil {
			report.Checks = append(report.Checks, doctor.Check{Name: "panel-access", Status: doctor.StatusFail, Detail: "install state is unreadable", Remedy: "recover the installer lifecycle state before changing access"})
		} else if state != nil && state.ConfigPath == configPath {
			exposure, exposureErr := install.DiagnoseExposure(ctx, host, state, time.Now())
			if exposureErr != nil {
				report.Checks = append(report.Checks, doctor.Check{Name: "panel-access", Status: doctor.StatusFail, Detail: exposureErr.Error(), Remedy: "run sudo wg-guard and review Panel access & HTTPS"})
			} else {
				for _, check := range exposure.Checks {
					report.Checks = append(report.Checks, doctor.Check{
						Name: "access-" + check.Name, Status: doctorExposureStatus(check.Status), Detail: check.Detail, Remedy: check.Remedy,
					})
				}
			}
		}
	}
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

func doctorExposureStatus(status install.ExposureHealthStatus) doctor.Status {
	switch status {
	case install.ExposureHealthPass:
		return doctor.StatusPass
	case install.ExposureHealthWarn:
		return doctor.StatusWarn
	case install.ExposureHealthFail:
		return doctor.StatusFail
	default:
		return doctor.StatusSkip
	}
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
