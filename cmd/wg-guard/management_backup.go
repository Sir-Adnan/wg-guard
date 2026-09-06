package main

import (
	"context"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
	"io"
	"strings"
)

func (m *manager) backupChoose(title string, keys ...string) (int, error) {
	labels := make([]string, len(keys))
	for i, k := range keys {
		labels[i] = m.ui.T("backup.cli." + k)
	}
	return m.ui.Choose(m.ui.T("backup.cli."+title), labels, 0)
}
func (m *manager) backupAsk(key, def string) (string, error) {
	return m.ui.Ask(m.ui.T("backup.cli."+key), def)
}
func (m *manager) backupExecute(ctx context.Context, args []string, input io.Reader, review string) error {
	if review != "" {
		ok, err := m.ui.Confirm(m.ui.T(review))
		if err != nil {
			return err
		}
		if !ok {
			return terminal.ErrBack
		}
	}
	err := m.run(ctx, args, input)
	m.ui.Result(err)
	return err
}

func (m *manager) backupAction(ctx context.Context, n int) error {
	switch n {
	case 1:
		m.ui.Text(m.ui.T("backup.cli.unencrypted"))
		return m.backupExecute(ctx, []string{"backup", "create"}, nil, "manage.backup_review")
	case 2:
		return m.backupExecute(ctx, []string{"backup", "list"}, nil, "")
	case 3, 7:
		args := []string{"restore"}
		if n == 7 {
			args = append(args, "--recover")
		} else {
			path, err := m.backupAsk("archive", "")
			if err != nil {
				return err
			}
			if path == "" {
				return terminal.ErrBack
			}
			args = append(args, path)
		}
		encrypted, err := m.ui.Confirm(m.ui.T("manage.encrypted"))
		if err != nil {
			return err
		}
		var input io.Reader
		if encrypted {
			password, err := m.ui.Secret(m.ui.T("backup.cli.password"))
			if err != nil {
				return err
			}
			input = strings.NewReader(password + "\n")
			args = append(args, "--password")
		}
		// Shared restore prints the actual validated archive review and obtains consent.
		return m.backupExecute(ctx, args, input, "")
	case 4:
		return m.backupScheduleForm(ctx)
	case 5:
		choice, err := m.ui.Choose(m.ui.T("manage.telegram"), []string{m.ui.T("manage.telegram_test"), m.ui.T("manage.telegram_setup"), m.ui.T("backup.cli.send")}, 0)
		if err != nil {
			return err
		}
		switch choice {
		case 1:
			return m.backupExecute(ctx, []string{"backup", "telegram-test"}, nil, "manage.telegram_review")
		case 2:
			token, err := m.ui.Secret(m.ui.T("manage.token"))
			if err != nil {
				return err
			}
			chat, err := m.ui.Ask(m.ui.T("manage.chat"), "")
			if err != nil {
				return err
			}
			ok, err := m.ui.Confirm(m.ui.T("manage.telegram_save"))
			if err != nil {
				return err
			}
			if !ok {
				return terminal.ErrBack
			}
			if err := m.run(ctx, []string{"settings", "set", "backup.telegram_chat", terminal.Digits(chat)}, nil); err != nil {
				return err
			}
			return m.backupExecute(ctx, []string{"settings", "set", "backup.telegram_token", "-stdin"}, strings.NewReader(token+"\n"), "")
		case 3:
			path, err := m.backupAsk("archive", "")
			if err != nil {
				return err
			}
			m.ui.Text(m.ui.T("backup.cli.unencrypted"))
			return m.backupExecute(ctx, []string{"backup", "send", "--archive", path}, nil, "manage.telegram_review")
		}
	case 6:
		password, err := m.ui.Secret(m.ui.T("backup.cli.password"))
		if err != nil {
			return err
		}
		return m.backupExecute(ctx, []string{"settings", "set", "backup.password", "-stdin"}, strings.NewReader(password+"\n"), "backup.cli.password_save")
	}
	return terminal.ErrBack
}

func (m *manager) backupScheduleForm(ctx context.Context) error {
	action, err := m.backupChoose("schedule_action", "list", "add", "edit", "enable", "disable", "delete")
	if err != nil {
		return err
	}
	verbs := []string{"schedule-list", "schedule-add", "schedule-update", "schedule-enable", "schedule-disable", "schedule-delete"}
	args := []string{"backup", verbs[action-1]}
	if action == 1 {
		return m.backupExecute(ctx, args, nil, "")
	}
	if action >= 3 {
		if err := m.run(ctx, []string{"backup", "schedule-list"}, nil); err != nil {
			return err
		}
		id, err := m.backupAsk("id", "")
		if err != nil {
			return err
		}
		args = append(args, "--id", id)
	}
	if action == 2 || action == 3 {
		name, err := m.backupAsk("name", "daily")
		if err != nil {
			return err
		}
		args = append(args, "--name", name)
		kind, err := m.backupChoose("kind", "daily", "weekly", "hours", "days_menu")
		if err != nil {
			return err
		}
		if kind <= 2 {
			args = append(args, "--kind", []string{"daily", "weekly"}[kind-1])
			tod, err := m.backupAsk("time", "03:30")
			if err != nil {
				return err
			}
			args = append(args, "--time", terminal.Digits(tod))
			if kind == 2 {
				day, err := m.backupAsk("weekday", "0")
				if err != nil {
					return err
				}
				args = append(args, "--weekday", terminal.Digits(day))
			}
		} else {
			n, err := m.backupAsk("interval_value", "1")
			if err != nil {
				return err
			}
			flag := "--hours"
			if kind == 4 {
				flag = "--days"
			}
			args = append(args, flag, terminal.Digits(n))
		}
		retention, err := m.backupAsk("retention", "0")
		if err != nil {
			return err
		}
		args = append(args, "--retention", terminal.Digits(retention))
		enabled, err := m.ui.Confirm(m.ui.T("backup.cli.enabled"))
		if err != nil {
			return err
		}
		if !enabled {
			args = append(args, "--disabled")
		}
		// Validate the complete form before the user approves any mutation.
		if _, err := parseBackupFlags(args[1], args[2:]); err != nil {
			return err
		}
	}
	return m.backupExecute(ctx, args, nil, "backup.cli.save")
}
