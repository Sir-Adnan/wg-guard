// Package nodestatus composes safe operational receipts for panel and REST.
// Readers never run reconciliation, collect host telemetry, or execute commands.
package nodestatus

import (
	"context"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/metrics"
	"github.com/Sir-Adnan/wg-guard/internal/runtimeapply"
	"github.com/Sir-Adnan/wg-guard/internal/telemetry"
)

type State string

const (
	Ready       State = "ready"
	NotReady    State = "not_ready"
	Unavailable State = "unavailable"
	Unobserved  State = "unobserved"
	Applied     State = "applied"
	Pending     State = "pending"
	Current     State = "current"
	Failed      State = "failed"
	Healthy     State = "healthy"
	Degraded    State = "degraded"
)

type Runtime struct {
	State             State      `json:"state"`
	RequestedSequence uint64     `json:"requested_sequence"`
	AppliedSequence   uint64     `json:"applied_sequence"`
	InFlight          bool       `json:"in_flight"`
	LastCompletedAt   *time.Time `json:"last_completed_at"`
	LastResult        State      `json:"last_result"`
}

type Accounting struct {
	State         State      `json:"state"`
	ObservedAt    *time.Time `json:"observed_at"`
	MaxAgeSeconds int64      `json:"max_age_seconds"`
}

type Sample struct {
	State          State      `json:"state"`
	ObservedAt     *time.Time `json:"observed_at"`
	CadenceSeconds int64      `json:"cadence_seconds"`
	Issues         []string   `json:"issues"`
}

type Snapshot struct {
	CapturedAt time.Time  `json:"captured_at"`
	Readiness  State      `json:"readiness"`
	Runtime    Runtime    `json:"runtime"`
	Accounting Accounting `json:"accounting"`
	Telemetry  Sample     `json:"telemetry"`
}

// Source is composed once by serve. The only active read is the existing bounded
// DB readiness probe; remaining callbacks read fixed process-local observations.
type Source struct {
	Readiness        func(context.Context) State
	Runtime          func() runtimeapply.Observation
	Accounting       func(time.Time, time.Duration) metrics.AccountingObservation
	AccountingWindow func(context.Context) time.Duration
	Telemetry        func(time.Time) telemetry.History
}

func (s Source) Read(ctx context.Context, now time.Time) Snapshot {
	d := Snapshot{CapturedAt: now.UTC(), Readiness: Unavailable,
		Runtime:    Runtime{State: Unavailable, LastResult: Unobserved},
		Accounting: Accounting{State: Unavailable},
		Telemetry:  Sample{State: Unavailable, Issues: []string{}},
	}
	if s.Readiness != nil {
		d.Readiness = s.Readiness(ctx)
	}
	if s.Runtime != nil {
		r := s.Runtime()
		d.Runtime.RequestedSequence, d.Runtime.AppliedSequence, d.Runtime.InFlight = r.Desired, r.Applied, r.InFlight
		d.Runtime.LastCompletedAt = timestamp(r.At)
		d.Runtime.State = Unobserved
		if r.Status == runtimeapply.Pending {
			d.Runtime.State = Pending
		}
		if r.Status == runtimeapply.Applied {
			d.Runtime.State = Applied
		}
		if !r.At.IsZero() {
			d.Runtime.LastResult = Applied
			if r.LastFailed {
				d.Runtime.LastResult = Failed
			}
		}
	}
	window := 5 * time.Minute
	if s.AccountingWindow != nil {
		if configured := s.AccountingWindow(ctx); configured > window {
			window = configured
		}
	}
	d.Accounting.MaxAgeSeconds = int64(window / time.Second)
	if s.Accounting != nil {
		a := s.Accounting(now, window)
		d.Accounting.ObservedAt = timestamp(a.ObservedAt)
		if a.Available {
			d.Accounting.State = Current
			if a.Failed {
				d.Accounting.State = Failed
			}
		}
	}
	if s.Telemetry != nil {
		h := s.Telemetry(now)
		d.Telemetry.ObservedAt = timestamp(h.Latest.At)
		d.Telemetry.CadenceSeconds = int64(h.Cadence / time.Second)
		d.Telemetry.Issues = h.Latest.Issues.Codes()
		if h.Available {
			d.Telemetry.State = State(h.Latest.Health)
		}
	}
	return d
}

func timestamp(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}
