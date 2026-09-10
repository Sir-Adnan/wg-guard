package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

type doctorRuntimeRunner struct {
	commands [][]string
}

func (r *doctorRuntimeRunner) Run(_ context.Context, argv []string) (subprocess.Result, error) {
	command := append([]string(nil), argv...)
	r.commands = append(r.commands, command)
	if argv[len(argv)-1] == "--version" {
		return subprocess.Result{Stdout: []byte("amneziawg-tools v3.1.20260812\n")}, nil
	}
	fields := []string{
		"private", "public", "39001", "0", "0", "0", "0", "0", "0", "0",
		"0", "0", "0", "0", "(null)", "(null)", "(null)", "(null)", "(null)", "(none)",
		"0", "0", "0", "0", "0", "0", "off", "off", "off",
	}
	return subprocess.Result{Stdout: []byte(strings.Join(fields, "\t") + "\n")}, nil
}

func TestDoctorInspectorUsesRunningContainerInDockerMode(t *testing.T) {
	runner := &doctorRuntimeRunner{}
	backend := newDoctorInspector(&install.State{Mode: install.ModeDocker}, runner)

	if _, err := backend.ToolsVersion(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Dump(context.Background(), "awg0"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"docker", "exec", "-i", install.Container, "awg", "--version"},
		{"docker", "exec", "-i", install.Container, "awg", "show", "awg0", "dump"},
	}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("Docker doctor commands = %#v, want %#v", runner.commands, want)
	}
}

func TestDoctorInspectorUsesHostToolsInNativeMode(t *testing.T) {
	runner := &doctorRuntimeRunner{}
	backend := newDoctorInspector(&install.State{Mode: install.ModeNative}, runner)

	if _, err := backend.ToolsVersion(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"awg", "--version"}}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("native doctor commands = %#v, want %#v", runner.commands, want)
	}
}

func TestPrepareDoctorFixUsesManagedRestartForDocker(t *testing.T) {
	state := &install.State{Mode: install.ModeDocker, ConfigPath: install.ConfigPath}
	restarts := 0
	directFix, summary, err := prepareDoctorFix(context.Background(), state, install.ConfigPath, true, func(context.Context) error {
		restarts++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if directFix || restarts != 1 || !strings.Contains(summary, "startup reconciliation") {
		t.Fatalf("Docker fix = direct:%v restarts:%d summary:%q", directFix, restarts, summary)
	}
}

func TestPrepareDoctorFixKeepsNativeOfflineRepair(t *testing.T) {
	state := &install.State{Mode: install.ModeNative, ConfigPath: install.ConfigPath}
	directFix, summary, err := prepareDoctorFix(context.Background(), state, install.ConfigPath, true, func(context.Context) error {
		t.Fatal("native doctor invoked Docker restart")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !directFix || summary != "" {
		t.Fatalf("native fix = direct:%v summary:%q", directFix, summary)
	}
}

func TestPrepareDoctorFixPropagatesDockerRestartFailure(t *testing.T) {
	state := &install.State{Mode: install.ModeDocker, ConfigPath: install.ConfigPath}
	want := errors.New("health gate failed")
	directFix, summary, err := prepareDoctorFix(context.Background(), state, install.ConfigPath, true, func(context.Context) error {
		return want
	})
	if !errors.Is(err, want) || directFix || summary != "" {
		t.Fatalf("Docker failure = direct:%v summary:%q err:%v", directFix, summary, err)
	}
}
