package terminal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Helper input deliberately lives on FD3, while stdin is an unrelated pipe.
func TestTTYHelper(t *testing.T) {
	mode := os.Getenv("WGG_TERMINAL_TEST")
	if mode == "" {
		return
	}
	f := os.NewFile(3, "actual-terminal")
	defer f.Close()
	initial, err := term.GetState(int(f.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	u := New(f, os.Stdout, Options{Context: ctx})
	if mode == "normal-cancel" {
		_, err = u.Ask("normal-input", "")
	} else {
		var name, secret, next string
		name, err = u.Ask("name-input", "")
		if err == nil {
			secret, err = u.Secret("secret-input")
		}
		if err == nil {
			next, err = u.Ask("next-input", "")
		}
		if mode == "sequence" && (name != "fixture" || secret != "synthetic-private-۱۲۳" || next != "next") {
			t.Fatal("input bytes/order changed")
		}
	}
	restored, e := term.GetState(int(f.Fd()))
	if e != nil || !reflect.DeepEqual(initial, restored) {
		t.Fatal("terminal state not restored")
	}
	if mode != "sequence" && err != ErrCanceled {
		t.Fatalf("input cancellation failed: %v", err)
	}
	fmt.Println("TTY-RESTORED")
}

func TestTTYActualFDSecretAndCancellation(t *testing.T) {
	for _, mode := range []string{"sequence", "normal-cancel", "secret-cancel", "secret-signal"} {
		t.Run(mode, func(t *testing.T) {
			fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY, 0)
			if err != nil {
				t.Fatal(err)
			}
			master := os.NewFile(uintptr(fd), "master")
			defer master.Close()
			if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
				t.Fatal(err)
			}
			n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
			if err != nil {
				t.Fatal(err)
			}
			slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer slave.Close()
			cmd := exec.Command(os.Args[0], "-test.run=^TestTTYHelper$")
			cmd.Env = append(os.Environ(), "WGG_TERMINAL_TEST="+mode)
			cmd.ExtraFiles = []*os.File{slave}
			cmd.Stdin = strings.NewReader("wrong-fd\n")
			cmd.Stdout = slave
			cmd.Stderr = slave
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Kill()
			transcript := ""
			wait := func(needle string) {
				t.Helper()
				deadline := time.Now().Add(8 * time.Second)
				for !strings.Contains(transcript, needle) {
					if time.Now().After(deadline) {
						t.Fatalf("timeout waiting for %s: %q", needle, transcript)
					}
					p := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
					n, e := unix.Poll(p, 100)
					if e != nil && e != unix.EINTR {
						t.Fatal(e)
					}
					if n > 0 {
						var b [1024]byte
						n, e := master.Read(b[:])
						if e != nil {
							t.Fatal(e)
						}
						transcript += string(b[:n])
					}
				}
			}
			send := func(s string) {
				t.Helper()
				if _, err := master.WriteString(s); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "normal-cancel" {
				wait("normal-input")
				if err := cmd.Process.Signal(os.Interrupt); err != nil {
					t.Fatal(err)
				}
			} else {
				wait("name-input")
				send("fixture\n")
				wait("secret-input")
				switch mode {
				case "sequence":
					send("synthetic-private-۱۲۳\nnext\n")
				case "secret-cancel":
					send("\x03")
				case "secret-signal":
					if err := cmd.Process.Signal(os.Interrupt); err != nil {
						t.Fatal(err)
					}
				}
			}
			wait("TTY-RESTORED")
			if err := cmd.Wait(); err != nil {
				t.Fatalf("TTY child: %v %q", err, transcript)
			}
			if strings.Contains(transcript, "synthetic-private") {
				t.Fatal("secret echoed")
			}
		})
	}
}
