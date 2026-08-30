//go:build !linux

package hoststats

import "time"

// procRoot is unused off Linux; kept so Reader construction is identical.
var procRoot = ""

const snapshotOKOnThisPlatform = false

// sample reports no metrics off Linux; the dashboard hides the host card.
func (r *Reader) sample(_ time.Time) (Snapshot, cpuTimes) {
	return Snapshot{OK: false}, cpuTimes{}
}
