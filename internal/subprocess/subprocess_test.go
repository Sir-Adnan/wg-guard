package subprocess

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// `go` is the one binary guaranteed to exist wherever the tests run.
func TestSystemRunCapturesOutput(t *testing.T) {
	r := NewSystem()
	res, err := r.Run(context.Background(), []string{"go", "env", "GOVERSION"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(res.Stdout)), "go") {
		t.Fatalf("stdout = %q", string(res.Stdout))
	}
}

func TestSystemRunExitErrorKeepsStderr(t *testing.T) {
	r := NewSystem()
	_, err := r.Run(context.Background(), []string{"go", "--definitely-not-a-flag-wgguard"})
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("want ExitError, got %v", err)
	}
	if ee.Name != "go" || ee.ExitCode == 0 {
		t.Fatalf("unexpected ExitError %+v", ee)
	}
	if !strings.Contains(ee.Error(), "exited with status") {
		t.Fatalf("Error() = %q", ee.Error())
	}
}

func TestSystemRunMissingBinary(t *testing.T) {
	r := NewSystem()
	_, err := r.Run(context.Background(), []string{"definitely-not-a-binary-wgguard-xyz", "arg"})
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("want exec.ErrNotFound, got %v", err)
	}
}

func TestSystemRunEmptyArgv(t *testing.T) {
	if _, err := NewSystem().Run(context.Background(), nil); err == nil {
		t.Fatal("empty argv must be rejected")
	}
}
