package main

import (
	"context"
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
