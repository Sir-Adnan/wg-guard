//go:build linux

package hoststats

import (
	"io"
	"math/bits"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// procRoot is the /proc mount; overridable in tests via Reader.root.
var procRoot = "/proc"

const snapshotOKOnThisPlatform = true

const maxProcFileBytes = 1 << 20

// sample fills platform-dependent fields from /proc and statfs.
func (r *Reader) sample(_ time.Time) (Snapshot, cpuTimes) {
	s := Snapshot{OK: true}
	var ct cpuTimes

	if v, ok := readProcStat(r.root); ok {
		ct = v
	}
	if total, avail, ok := readMemInfo(r.root); ok {
		s.MemTotal, s.MemAvail = total, avail
	}
	if l1, l5, l15, ok := readLoadAvg(r.root); ok {
		s.Load1, s.Load5, s.Load15 = &l1, &l5, &l15
	}
	if up, ok := readUptime(r.root); ok {
		s.Uptime = up
	}
	if rss, ok := readProcessRSS(r.root); ok {
		s.ProcessRSSBytes = rss
	}
	if counters, ok := readDefaultRouteCounters(r.root); ok {
		s.HostNetwork = counters
	}
	if total, free, ok := statfsUsage(r.diskPath); ok {
		s.DiskTotal, s.DiskFree = total, free
	}
	return s, ct
}

// readProcStat parses the aggregate "cpu" line: user nice system idle iowait
// irq softirq steal guest guest_nice. Idle = idle + iowait; total = all
// non-guest fields (guest time already runs inside user, counting both
// double-counts it).
func readProcStat(root string) (cpuTimes, bool) {
	data, ok := readProcFile(filepath.Join(root, "stat"))
	if !ok {
		return cpuTimes{}, false
	}
	line, _, ok := strings.Cut(string(data), "\n")
	if !ok || !strings.HasPrefix(line, "cpu ") {
		return cpuTimes{}, false
	}
	fields := strings.Fields(line)[1:]
	if len(fields) < 5 {
		return cpuTimes{}, false
	}
	vals := make([]uint64, len(fields))
	for i, f := range fields {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return cpuTimes{}, false
		}
		vals[i] = v
	}
	idle := vals[3] + vals[4] // idle + iowait
	var total uint64
	for i, v := range vals {
		if i < 8 { // user..steal; guest (8,9) is a subset of user
			total += v
		}
	}
	return cpuTimes{idle: idle, total: total}, true
}

// readMemInfo returns MemTotal and MemAvailable in bytes.
func readMemInfo(root string) (uint64, uint64, bool) {
	data, ok := readProcFile(filepath.Join(root, "meminfo"))
	if !ok {
		return 0, 0, false
	}
	var total, avail uint64
	for _, line := range strings.Split(string(data), "\n") {
		key, val, ok := cutMeminfo(line)
		if !ok {
			continue
		}
		switch key {
		case "MemTotal":
			total = val
		case "MemAvailable":
			avail = val
		}
	}
	if total == 0 {
		return 0, 0, false
	}
	return total, avail, true
}

// cutMeminfo parses "MemTotal:  16384000 kB" (values are KiB).
func cutMeminfo(line string) (string, uint64, bool) {
	key, rest, ok := strings.Cut(line, ":")
	if !ok {
		return "", 0, false
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", 0, false
	}
	kb, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return strings.TrimSpace(key), kb * 1024, true
}

// readLoadAvg parses "0.52 0.58 0.59 3/987 12345".
func readLoadAvg(root string) (float64, float64, float64, bool) {
	data, ok := readProcFile(filepath.Join(root, "loadavg"))
	if !ok {
		return 0, 0, 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0, false
	}
	vals := make([]float64, 3)
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return 0, 0, 0, false
		}
		vals[i] = v
	}
	return vals[0], vals[1], vals[2], true
}

// readUptime parses seconds-since-boot from /proc/uptime.
func readUptime(root string) (time.Duration, bool) {
	data, ok := readProcFile(filepath.Join(root, "uptime"))
	if !ok {
		return 0, false
	}
	up, _, ok := strings.Cut(strings.TrimSpace(string(data)), " ")
	if !ok {
		return 0, false
	}
	sec, err := strconv.ParseFloat(up, 64)
	if err != nil {
		return 0, false
	}
	return time.Duration(sec * float64(time.Second)), true
}

// statfsUsage returns total and free bytes of the filesystem holding path.
func statfsUsage(path string) (uint64, uint64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	total := st.Blocks * uint64(st.Bsize)
	free := st.Bavail * uint64(st.Bsize) // unprivileged-free, matches df
	if total == 0 {
		return 0, 0, false
	}
	return total, free, true
}

// readDefaultRouteCounters selects the lowest-metric active IPv4 default
// route and returns its counters without exposing the interface publicly.
func readDefaultRouteCounters(root string) (NetworkCounters, bool) {
	data, ok := readProcFile(filepath.Join(root, "net", "route"))
	if !ok {
		return NetworkCounters{}, false
	}
	iface := ""
	bestMetric := uint64(^uint32(0))
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[1] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(fields[3], 16, 32)
		if err != nil || flags&1 == 0 {
			continue
		}
		metric, err := strconv.ParseUint(fields[6], 10, 32)
		if err != nil || (iface != "" && metric >= bestMetric) {
			continue
		}
		iface, bestMetric = fields[0], metric
	}
	if iface == "" {
		return NetworkCounters{}, false
	}
	got := readInterfaceCounters(root, []string{iface})
	return got, got.Available
}

func readInterfaceCounters(root string, requested []string) NetworkCounters {
	wanted := make(map[string]struct{}, len(requested))
	for _, name := range requested {
		if name != "" {
			wanted[name] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return NetworkCounters{}
	}
	data, ok := readProcFile(filepath.Join(root, "net", "dev"))
	if !ok {
		return NetworkCounters{}
	}
	var names []string
	var rxTotal, txTotal uint64
	for _, line := range strings.Split(string(data), "\n") {
		left, right, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name := strings.TrimSpace(left)
		if _, ok := wanted[name]; !ok {
			continue
		}
		fields := strings.Fields(right)
		if len(fields) < 9 {
			continue
		}
		rx, errRX := strconv.ParseUint(fields[0], 10, 64)
		tx, errTX := strconv.ParseUint(fields[8], 10, 64)
		if errRX != nil || errTX != nil {
			continue
		}
		var carry uint64
		rxTotal, carry = bits.Add64(rxTotal, rx, 0)
		if carry != 0 {
			return NetworkCounters{}
		}
		txTotal, carry = bits.Add64(txTotal, tx, 0)
		if carry != 0 {
			return NetworkCounters{}
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return NetworkCounters{}
	}
	sort.Strings(names)
	return NetworkCounters{
		Identity:   strings.Join(names, "\x00"),
		RXBytes:    rxTotal,
		TXBytes:    txTotal,
		Interfaces: len(names),
		Available:  true,
	}
}

func readProcessRSS(root string) (uint64, bool) {
	data, ok := readProcFile(filepath.Join(root, "self", "status"))
	if !ok {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := cutMeminfo(line)
		if ok && key == "VmRSS" {
			return value, true
		}
	}
	return 0, false
}

// InterfaceCounters aggregates only the explicitly requested interface
// names. Unknown links are omitted and reflected in Interfaces.
func (r *Reader) InterfaceCounters(names []string) NetworkCounters {
	return readInterfaceCounters(r.root, names)
}

func readProcFile(path string) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxProcFileBytes+1))
	if err != nil || len(data) > maxProcFileBytes {
		return nil, false
	}
	return data, true
}
