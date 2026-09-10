package install

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

type logStreamHost struct {
	*memHost
	chunks    [][]byte
	streamErr error
	wait      bool
	stderr    []byte
}

func (h *logStreamHost) Stream(ctx context.Context, argv []string, stdout, stderr io.Writer) error {
	h.commands = append(h.commands, memCmd{argv: append([]string(nil), argv...)})
	if h.wait {
		<-ctx.Done()
		return ctx.Err()
	}
	if len(h.stderr) > 0 {
		if _, err := stderr.Write(h.stderr); err != nil {
			return err
		}
	}
	for _, chunk := range h.chunks {
		if _, err := stdout.Write(chunk); err != nil {
			return err
		}
	}
	return h.streamErr
}

func installedLogState(mode Mode) *State {
	state := &State{Schema: StateSchema, Mode: mode, ConfigPath: ConfigPath, DataDir: DataDir, BinPath: BinPath}
	if mode == ModeDocker {
		state.ComposePath = ComposePth
	} else {
		state.UnitPath = UnitPath
	}
	return state
}

func TestStreamLogsUsesExactModeNativeArgv(t *testing.T) {
	since := time.Date(2026, 9, 9, 8, 30, 45, 0, time.UTC)
	for _, tc := range []struct {
		name string
		mode Mode
		want []string
	}{
		{
			name: "docker", mode: ModeDocker,
			want: []string{"docker", "logs", "--tail", "275", "--since", "2026-09-09T08:30:45Z", "--follow", Container},
		},
		{
			name: "native", mode: ModeNative,
			want: []string{"journalctl", "--namespace=wg-guard", "--unit=wg-guard.service", "--no-pager", "--output=cat", "--lines", "275", "--since", "2026-09-09T08:30:45Z", "--follow"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &logStreamHost{memHost: newMemHost(), chunks: [][]byte{[]byte("line\n")}}
			var out, errOut bytes.Buffer
			err := StreamLogs(context.Background(), h, installedLogState(tc.mode), LogOptions{
				Tail: 275, Since: since, Follow: true,
			}, &out, &errOut)
			if err != nil {
				t.Fatal(err)
			}
			if len(h.commands) != 1 || !slices.Equal(h.commands[0].argv, tc.want) {
				t.Fatalf("argv = %v, want %v", h.ranCommands(), tc.want)
			}
			if strings.Contains(strings.Join(h.commands[0].argv, " "), "sh -c") {
				t.Fatal("logs invoked a shell")
			}
		})
	}
}

func TestStreamLogsFiltersCompleteBoundedLinesLocally(t *testing.T) {
	oversized := strings.Repeat("x", maxLogLineBytes+1) + " component=http\n"
	h := &logStreamHost{memHost: newMemHost(), chunks: [][]byte{
		[]byte("time=x level=INFO component=awg msg=skip\ntime=x level=INFO msg=\"fake component=http\" component=awg\ntime=x level=INFO comp"),
		[]byte("onent=http msg=keep\n" + oversized + `{"component":"awg","msg":"fake \"component\":\"http\""}` + "\n" + `{"component":"http","msg":"json keep"}` + "\npartial component=http"),
	}}
	var out bytes.Buffer
	err := StreamLogs(context.Background(), h, installedLogState(ModeDocker), LogOptions{
		Tail: 200, Since: time.Now().UTC(), Component: "http",
	}, &out, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "time=x level=INFO component=http msg=keep\n"+`{"component":"http","msg":"json keep"}`+"\n"; got != want {
		t.Fatalf("filtered output:\n%q\nwant:\n%q", got, want)
	}
	for _, arg := range h.commands[0].argv {
		if strings.Contains(arg, "http") {
			t.Fatalf("local component filter entered subprocess argv: %v", h.commands[0].argv)
		}
	}
}

func TestStreamLogsDockerMergesContainerStderrIntoOutput(t *testing.T) {
	h := &logStreamHost{
		memHost: newMemHost(),
		stderr:  []byte("time=x level=INFO component=serve msg=ready\n"),
		chunks:  [][]byte{[]byte("time=x level=INFO component=http msg=request\n")},
	}
	var out, errOut bytes.Buffer
	err := StreamLogs(context.Background(), h, installedLogState(ModeDocker), LogOptions{
		Tail: 200, Since: time.Now().UTC(),
	}, &out, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"component=serve", "component=http"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("unified Docker output %q does not contain %q", out.String(), want)
		}
	}
	if errOut.Len() != 0 {
		t.Fatalf("Docker container stderr escaped unified output: %q", errOut.String())
	}
}

type brokenLogWriter struct{ err error }

func (w brokenLogWriter) Write([]byte) (int, error) { return 0, w.err }

func TestStreamLogsPropagatesSourceAndOutputFailures(t *testing.T) {
	sentinel := errors.New("source unavailable")
	h := &logStreamHost{memHost: newMemHost(), streamErr: sentinel}
	err := StreamLogs(context.Background(), h, installedLogState(ModeDocker), LogOptions{
		Tail: 200, Since: time.Now().UTC(),
	}, io.Discard, io.Discard)
	if !errors.Is(err, sentinel) {
		t.Fatalf("source error = %v", err)
	}

	broken := errors.New("broken output")
	h = &logStreamHost{memHost: newMemHost(), chunks: [][]byte{[]byte("component=http\n")}}
	err = StreamLogs(context.Background(), h, installedLogState(ModeNative), LogOptions{
		Tail: 200, Since: time.Now().UTC(), Component: "http",
	}, brokenLogWriter{broken}, io.Discard)
	if !errors.Is(err, broken) {
		t.Fatalf("output error = %v", err)
	}
}

func TestStreamLogsHonorsCancellation(t *testing.T) {
	h := &logStreamHost{memHost: newMemHost(), wait: true}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := StreamLogs(ctx, h, installedLogState(ModeDocker), LogOptions{
		Tail: 200, Since: time.Now().UTC(), Follow: true,
	}, io.Discard, io.Discard)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}

func TestStreamLogsRejectsInvalidStateAndOptionsBeforeExecution(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state *State
		opts  LogOptions
	}{
		{"invalid source while following", installedLogState(Mode("podman")), LogOptions{Tail: 200, Since: time.Now().UTC(), Follow: true}},
		{"zero tail", installedLogState(ModeDocker), LogOptions{Since: time.Now().UTC()}},
		{"tail too large", installedLogState(ModeDocker), LogOptions{Tail: MaxLogTail + 1, Since: time.Now().UTC()}},
		{"missing since", installedLogState(ModeDocker), LogOptions{Tail: 200}},
		{"unknown component", installedLogState(ModeDocker), LogOptions{Tail: 200, Since: time.Now().UTC(), Component: "http --follow"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &logStreamHost{memHost: newMemHost()}
			if err := StreamLogs(context.Background(), h, tc.state, tc.opts, io.Discard, io.Discard); err == nil {
				t.Fatal("expected rejection")
			}
			if len(h.commands) != 0 {
				t.Fatalf("source executed after invalid input: %v", h.ranCommands())
			}
		})
	}
}
