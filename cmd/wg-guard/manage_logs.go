package main

import (
	"context"
	"errors"

	"github.com/Sir-Adnan/wg-guard/internal/terminal"
)

// logsMenu keeps log inspection reachable even when initial installation
// failed, and never constructs a shell command from operator input.
func (m *manager) logsMenu(ctx context.Context) error {
	for {
		keys := []string{"logs_installer", "logs_operations"}
		sources := []string{"installer", "operations"}
		if m.view == managerInstalled || m.view == managerRecovery {
			keys = append([]string{"logs_service", "logs_component"}, keys...)
			sources = append([]string{"service", "component"}, sources...)
		}
		choice, err := m.menu("logs_menu", keys...)
		if errors.Is(err, terminal.ErrBack) || errors.Is(err, terminal.ErrCanceled) && ctx.Err() == nil {
			return nil
		}
		if err != nil {
			return err
		}
		source := sources[choice-1]
		component := ""
		if source == "component" {
			components := []string{"serve", "http", "scheduler", "accounting", "webhook", "backup", "awg", "network"}
			labels := []string{"Service lifecycle", "HTTP requests", "Scheduler", "Traffic accounting", "Webhooks", "Backups", "AmneziaWG", "Network"}
			componentChoice, chooseErr := m.ui.Choose(m.ui.T("manage.logs_components"), labels, 0)
			if errors.Is(chooseErr, terminal.ErrBack) {
				continue
			}
			if chooseErr != nil {
				return chooseErr
			}
			component = components[componentChoice-1]
			source = "service"
		}
		args := []string{"logs", "--source", source, "--tail", "200"}
		if source != "installer" {
			args = append(args, "--since", "7d")
		}
		if component != "" {
			args = append(args, "--component", component)
		}
		if source != "operations" {
			view, chooseErr := m.menu("logs_view", "logs_recent", "logs_live")
			if errors.Is(chooseErr, terminal.ErrBack) {
				continue
			}
			if chooseErr != nil {
				return chooseErr
			}
			if view == 2 {
				args = append(args, "--follow")
				m.ui.Info(m.ui.T("manage.logs_stop"))
			}
		}
		title := m.ui.T("manage.logs_output") + " · " + source
		if component != "" {
			title += " / " + component
		}
		m.ui.Section(title)
		if err := m.run(ctx, args, nil); err != nil {
			if errors.Is(err, context.Canceled) && ctx.Err() != nil {
				return terminal.ErrCanceled
			}
			m.ui.Result(err)
		}
	}
}
