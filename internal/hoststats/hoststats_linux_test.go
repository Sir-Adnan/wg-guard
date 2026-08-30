//go:build linux

package hoststats

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Reader.root accepts any filesystem root, so /proc readers are tested
// against fixture files instead of the real kernel (which changes).
func fakeProc(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestSnapshotFromFixtureProc(t *testing.T) {
	root := fakeProc(t, map[string]string{
		"stat": "cpu  100 0 50 700 40 0 10 0 0 0\n" +
			"cpu0 50 0 25 350 20 0 5 0 0 0\nintr 123\n",
		"meminfo": "MemTotal:       16000000 kB\n" +
			"MemFree:         2000000 kB\n" +
			"MemAvailable:    8000000 kB\n" +
			"Buffers:          100000 kB\n",
		"loadavg": "0.52 0.58 0.59 3/987 12345\n",
		"uptime":  "86400.50 123456.78\n",
	})
	r := New(t.TempDir()).withRoot(root)

	s := r.Snapshot(time.Unix(1700000000, 0))
	if !s.OK {
		t.Fatal("Linux snapshot must be OK")
	}
	if s.CPUPercent != nil {
		t.Fatalf("first snapshot must have nil CPU, got %v", *s.CPUPercent)
	}
	if s.MemTotal != 16_000_000*1024 || s.MemAvail != 8_000_000*1024 {
		t.Fatalf("mem = %d/%d", s.MemAvail, s.MemTotal)
	}
	if s.MemUsed() != 8_000_000*1024 {
		t.Fatalf("MemUsed = %d", s.MemUsed())
	}
	if s.Load1 == nil || *s.Load1 != 0.52 || s.Load15 == nil || *s.Load15 != 0.59 {
		t.Fatalf("load = %v %v", s.Load1, s.Load15)
	}
	if s.Uptime.Seconds() != 86400.50 {
		t.Fatalf("uptime = %v", s.Uptime)
	}
	if s.DiskTotal == 0 || s.DiskFree == 0 {
		t.Fatal("statfs on temp dir must work")
	}

	// A second snapshot over static fixture counters has a zero delta —
	// the reader reports "no measurement" (nil) rather than a fake 0%.
	s2 := r.Snapshot(time.Unix(1700000001, 0))
	if s2.CPUPercent != nil {
		t.Fatalf("zero counter delta must yield nil, got %v", *s2.CPUPercent)
	}
}

func TestMalformedProcFiles(t *testing.T) {
	root := fakeProc(t, map[string]string{
		"stat":    "garbage without cpu line\n",
		"meminfo": "MemTotal: notanumber kB\n",
		"loadavg": "only one field\n",
		"uptime":  "x y\n",
	})
	r := New(t.TempDir()).withRoot(root)
	s := r.Snapshot(time.Now())
	if !s.OK {
		t.Fatal("OK stays true (Linux); individual metrics degrade")
	}
	if s.CPUPercent != nil || s.MemTotal != 0 || s.Load1 != nil || s.Uptime != 0 {
		t.Fatal("malformed files must leave metrics unset, not zero-valued")
	}
}

func TestMissingProcDir(t *testing.T) {
	r := New(t.TempDir()).withRoot(filepath.Join(t.TempDir(), "no-such-dir"))
	s := r.Snapshot(time.Now())
	if s.MemTotal != 0 || s.Load1 != nil || s.Uptime != 0 || s.CPUPercent != nil {
		t.Fatal("missing /proc must yield no metrics")
	}
}
