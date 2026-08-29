package network

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

// fakeRunner records argv and returns scripted results (per binary), keeping
// network tests free of any real host state.
type fakeRunner struct {
	calls [][]string
	// out maps argv[0] to a queue of (stdout, stderr, err) responses.
	out map[string][]fakeResponse
}

type fakeResponse struct {
	stdout string
	stderr string
	err    error
}

func (f *fakeRunner) Run(_ context.Context, argv []string) (subprocess.Result, error) {
	f.calls = append(f.calls, append([]string(nil), argv...))
	q := f.out[argv[0]]
	if len(q) == 0 {
		return subprocess.Result{}, nil
	}
	r := q[0]
	f.out[argv[0]] = q[1:]
	return subprocess.Result{Stdout: []byte(r.stdout), Stderr: []byte(r.stderr)}, r.err
}

func (f *fakeRunner) push(binary, stdout, stderr string, err error) {
	if f.out == nil {
		f.out = map[string][]fakeResponse{}
	}
	f.out[binary] = append(f.out[binary], fakeResponse{stdout, stderr, err})
}

func (f *fakeRunner) joined() string {
	var sb strings.Builder
	for _, c := range f.calls {
		sb.WriteString(strings.Join(c, " "))
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestCreateAWGSequence(t *testing.T) {
	f := &fakeRunner{}
	l := &Links{Run: f}
	if err := l.CreateAWG(context.Background(), "awg0", 1420); err != nil {
		t.Fatal(err)
	}
	want := "ip link add awg0 type amneziawg\nip link set dev awg0 mtu 1420\n"
	if got := f.joined(); got != want {
		t.Fatalf("argv =\n%s\nwant\n%s", got, want)
	}
}

func TestCreateAWGNoMTU(t *testing.T) {
	f := &fakeRunner{}
	l := &Links{Run: f}
	if err := l.CreateAWG(context.Background(), "awg1", 0); err != nil {
		t.Fatal(err)
	}
	if got := f.joined(); got != "ip link add awg1 type amneziawg\n" {
		t.Fatalf("argv = %q", got)
	}
}

func TestDeleteMissingIsClassified(t *testing.T) {
	f := &fakeRunner{}
	f.push("ip", "", "Device \"awg0\" does not exist.", &subprocess.ExitError{Name: "ip", ExitCode: 1})
	l := &Links{Run: f}
	err := l.Delete(context.Background(), "awg0")
	if !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("want ErrLinkNotFound, got %v", err)
	}
}

func TestExists(t *testing.T) {
	f := &fakeRunner{}
	l := &Links{Run: f}
	ok, err := l.Exists(context.Background(), "awg0")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	f.push("ip", "", "Device \"awg9\" does not exist.", &subprocess.ExitError{Name: "ip", ExitCode: 1})
	ok, err = l.Exists(context.Background(), "awg9")
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestEnsureIPForwarding(t *testing.T) {
	ctx := context.Background()

	t.Run("already on", func(t *testing.T) {
		f := &fakeRunner{}
		f.push("sysctl", "1\n", "", nil)
		l := &Links{Run: f}
		prev, changed, err := l.EnsureIPForwarding(ctx)
		if err != nil || changed || prev != 1 {
			t.Fatalf("prev=%d changed=%v err=%v", prev, changed, err)
		}
		if got := f.joined(); strings.Contains(got, "sysctl -w") {
			t.Fatalf("must not write when already enabled:\n%s", got)
		}
	})

	t.Run("off then enabled", func(t *testing.T) {
		f := &fakeRunner{}
		f.push("sysctl", "0\n", "", nil)
		l := &Links{Run: f}
		prev, changed, err := l.EnsureIPForwarding(ctx)
		if err != nil || !changed || prev != 0 {
			t.Fatalf("prev=%d changed=%v err=%v", prev, changed, err)
		}
		want := "sysctl -n net.ipv4.ip_forward\nsysctl -w net.ipv4.ip_forward=1\n"
		if got := f.joined(); got != want {
			t.Fatalf("argv =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("garbage value", func(t *testing.T) {
		f := &fakeRunner{}
		f.push("sysctl", "banana\n", "", nil)
		l := &Links{Run: f}
		if _, _, err := l.EnsureIPForwarding(ctx); err == nil {
			t.Fatal("want error on garbage sysctl value")
		}
	})
}
