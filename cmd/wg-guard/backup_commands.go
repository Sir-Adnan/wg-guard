package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
	"io"
	"os"
	"os/signal"
	"strings"
)

type backupPrinter struct{ locale i18n.Locale }

func (p backupPrinter) text(key string, args ...any) string {
	return i18n.T(p.locale, "backup.cli."+key, args...)
}
func backupText(key string, args ...any) string {
	return i18n.T(i18n.Locale(terminalLocale()), "backup.cli."+key, args...)
}
func readPassword(prompt string) (string, error) {
	u := terminal.New(os.Stdin, os.Stderr, terminal.Detect(os.Stdin, os.Stderr, i18n.Locale(terminalLocale())))
	return u.Secret(prompt)
}
func readSettingInput(r io.Reader) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, 4097))
	if err != nil || len(b) > 4096 {
		return "", fmt.Errorf("%s", backupText("input"))
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r"), nil
}

type backupFlags struct {
	lang                                              string
	config, passwordFile, output, reason, id, archive string
	password, disabled                                bool
	days                                              int
	schedule                                          backup.Schedule
}

func parseBackupFlags(command string, args []string) (backupFlags, error) {
	o := backupFlags{}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.config, "config", install.ConfigPath, "")
	fs.StringVar(&o.lang, "lang", terminalLocale(), "")
	switch command {
	case "create":
		fs.BoolVar(&o.password, "password", false, "")
		fs.StringVar(&o.passwordFile, "password-file", "", "")
		fs.StringVar(&o.output, "output", "", "")
		fs.StringVar(&o.reason, "reason", "manual", "")
	case "send":
		fs.StringVar(&o.archive, "archive", "", "")
	case "schedule-add", "schedule-update":
		fs.StringVar(&o.id, "id", "", "")
		fs.StringVar(&o.schedule.Name, "name", "installer-daily", "")
		fs.StringVar(&o.schedule.Kind, "kind", backup.KindDaily, "")
		fs.StringVar(&o.schedule.TimeOfDay, "time", "03:30", "")
		fs.IntVar(&o.schedule.Weekday, "weekday", 0, "")
		fs.IntVar(&o.schedule.IntervalHours, "hours", 0, "")
		fs.IntVar(&o.days, "days", 0, "")
		fs.IntVar(&o.schedule.RetentionCount, "retention", 0, "")
		fs.BoolVar(&o.disabled, "disabled", false, "")
	case "schedule-enable", "schedule-disable", "schedule-delete":
		fs.StringVar(&o.id, "id", "", "")
	case "list", "schedule-list", "telegram-test":
	default:
		return o, fmt.Errorf("%s", backupText("usage"))
	}
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return o, fmt.Errorf("%s", backupText("flags"))
	}
	if !i18n.Locale(o.lang).Valid() {
		return o, fmt.Errorf("%s", backupText("flags"))
	}
	if o.password && o.passwordFile != "" || len(o.reason) > 64 {
		return o, fmt.Errorf("%s", backupText("flags"))
	}
	if command == "send" && o.archive == "" || strings.HasPrefix(command, "schedule-") && command != "schedule-add" && command != "schedule-list" && o.id == "" {
		return o, fmt.Errorf("%s", backupText("flags"))
	}
	if command == "schedule-add" || command == "schedule-update" {
		daysSet, hoursSet := false, false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "days" {
				daysSet = true
			}
			if f.Name == "hours" {
				hoursSet = true
			}
		})
		if hoursSet {
			o.schedule.Kind = backup.KindInterval
		}
		if daysSet {
			if o.days < 1 || o.days > 7 || hoursSet {
				return o, fmt.Errorf("%s", backupText("days"))
			}
			o.schedule.IntervalHours = o.days * 24
			o.schedule.Kind = backup.KindInterval
		}
		o.schedule.Enabled = !o.disabled
		if err := o.schedule.Validate(); err != nil {
			return o, fmt.Errorf("%s", backupText("schedule_invalid"))
		}
	}
	return o, nil
}

func runBackup(args []string) (resultErr error) {
	if len(args) == 0 {
		return fmt.Errorf("%s", backupText("usage"))
	}
	o, err := parseBackupFlags(args[0], args[1:])
	if err != nil {
		return err
	}
	printer := backupPrinter{i18n.Locale(o.lang)}
	defer func() { resultErr = backup.InLocale(resultErr, printer.locale) }()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	password := ""
	if o.password {
		password, err = readPassword(printer.text("password"))
	} else if o.passwordFile != "" {
		password, err = install.ReadProtectedPassword(install.NewRealHost(), o.passwordFile)
	}
	if err != nil {
		return err
	}
	if o.password || o.passwordFile != "" {
		if err := backup.ValidatePassword(password); err != nil {
			return err
		}
	}
	env, err := loadCLIEnv(o.config)
	if err != nil {
		return err
	}
	defer env.Close()
	s := env.newBackupService()
	switch args[0] {
	case "create":
		r, err := s.Create(ctx, backup.CreateOpts{Password: password, Reason: o.reason, Dir: o.output, Deliver: true})
		if err != nil {
			return err
		}
		// Stable legacy machine-readable prefix used by pre-update backup capture.
		fmt.Printf("created %s (%d KiB)\n", r.Name, r.Size>>10)
		printer.printBackupResult(r)
	case "send":
		r, err := s.Send(ctx, o.archive)
		if r != nil {
			printer.printBackupResult(r)
		}
		if err != nil {
			return err
		}
	case "telegram-test":
		if err := s.TestTelegram(ctx); err != nil {
			return err
		}
		fmt.Println(printer.text("telegram_ok"))
	case "list":
		arcs, err := s.List()
		if err != nil {
			return err
		}
		fmt.Println(printer.text("archives"))
		for _, a := range arcs {
			fmt.Printf("%s | %d KiB | %s | age=%v\n", terminal.Clean(a.Name), a.Size>>10, a.ModTime.UTC().Format("2006-01-02 15:04 UTC"), a.Encrypted)
		}
		return printer.printSchedules(ctx, s)
	case "schedule-list":
		return printer.printSchedules(ctx, s)
	case "schedule-add", "schedule-update":
		var sc *backup.Schedule
		if args[0] == "schedule-add" {
			sc, err = s.CreateSchedule(ctx, &o.schedule)
		} else {
			sc, err = s.UpdateSchedule(ctx, o.id, &o.schedule)
		}
		if err != nil {
			return err
		}
		printer.printSchedule(sc)
	case "schedule-enable", "schedule-disable":
		sc, err := s.GetSchedule(ctx, o.id)
		if err != nil {
			return err
		}
		sc.Enabled = args[0] == "schedule-enable"
		sc, err = s.UpdateSchedule(ctx, o.id, sc)
		if err != nil {
			return err
		}
		printer.printSchedule(sc)
	case "schedule-delete":
		if err := s.DeleteSchedule(ctx, o.id); err != nil {
			return err
		}
		fmt.Println(printer.text("deleted"))
	}
	return nil
}
func (printer backupPrinter) printBackupResult(r *backup.Result) {
	fmt.Println(printer.text("result", r.Encrypted, strings.Join(r.Delivered, ", ")))
	for _, w := range r.Warnings {
		fmt.Println(printer.text("warning", terminal.Clean(w.Localized(printer.locale))))
	}
}
func (printer backupPrinter) printSchedules(ctx context.Context, s *backup.Service) error {
	rows, err := s.Schedules(ctx)
	if err != nil {
		return err
	}
	fmt.Println(printer.text("schedules"))
	for _, sc := range rows {
		printer.printSchedule(sc)
	}
	return nil
}
func (printer backupPrinter) printSchedule(sc *backup.Schedule) {
	fmt.Printf("%s | %s | %s | %s\n", sc.ID, terminal.Clean(sc.Name), printer.describeSchedule(sc), printer.text("schedule_state", sc.RetentionCount, sc.Enabled, sc.NextRunAt.UTC().Format("2006-01-02 15:04 UTC")))
}
func (printer backupPrinter) describeSchedule(sc *backup.Schedule) string {
	switch sc.Kind {
	case backup.KindInterval:
		return printer.text("interval", sc.IntervalHours)
	case backup.KindWeekly:
		return fmt.Sprintf("%d@%s UTC", sc.Weekday, sc.TimeOfDay)
	default:
		return sc.TimeOfDay + " UTC"
	}
}
