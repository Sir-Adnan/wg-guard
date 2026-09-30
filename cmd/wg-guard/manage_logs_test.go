package main

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
)

func TestManagerLogMenuSelectsLiveServiceAndRecentComponent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers string
		want    []string
	}{
		{"live service", "1\n2\n0\n", []string{"logs", "--source", "service", "--tail", "200", "--since", "7d", "--follow"}},
		{"recent AWG", "2\n7\n1\n0\n", []string{"logs", "--source", "service", "--tail", "200", "--since", "7d", "--component", "awg"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			m := manager{view: managerInstalled, ui: terminal.New(strings.NewReader(tc.answers), io.Discard, terminal.Options{Locale: i18n.En}),
				run: func(_ context.Context, args []string, _ io.Reader) error {
					got = append([]string(nil), args...)
					return nil
				}}
			if err := m.logsMenu(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("log menu command = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFailedSetupCanOpenInstallerLogsWithoutService(t *testing.T) {
	var got []string
	m := manager{view: managerFresh, ui: terminal.New(strings.NewReader("1\n1\n0\n"), io.Discard, terminal.Options{Locale: i18n.En}),
		run: func(_ context.Context, args []string, _ io.Reader) error {
			got = append([]string(nil), args...)
			return nil
		}}
	if err := m.logsMenu(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"logs", "--source", "installer", "--tail", "200"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fresh log command = %v, want %v", got, want)
	}
}
