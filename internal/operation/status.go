// Package operation defines shared presentation states without granting host or
// filesystem authority. Typed queues and lifecycle coordinators retain their own
// closed requests, journals, validation and ownership.
package operation

type State string

const (
	Queued          State = "queued"
	Running         State = "running"
	Succeeded       State = "succeeded"
	Failed          State = "failed"
	Scheduled       State = "scheduled"
	Canceled        State = "canceled"
	AwaitingRestart State = "awaiting_restart"
	RecoveryNeeded  State = "recovery_needed"
	Review          State = "review"
)

func (s State) Active() bool { return s == Queued || s == Running || s == Scheduled }

// Receipt carries catalog keys and safe operation identity, never raw errors,
// subprocess text, credentials or arbitrary commands.
type Receipt struct {
	ID                string
	State             State
	Label, Tone, Next string
}

func Present(id string, state State, recovery bool) Receipt {
	if recovery && !state.Active() {
		state = RecoveryNeeded
	}
	r := Receipt{ID: id, State: state, Label: "operations.state." + string(state), Tone: "neutral"}
	switch state {
	case Queued, Running:
		r.Tone = "info"
		r.Next = "operations.next.wait"
	case Scheduled:
		r.Tone = "info"
		r.Next = "operations.next.schedule"
	case Succeeded:
		r.Tone = "success"
		r.Next = "operations.next.verified"
	case Canceled:
		r.Next = "operations.next.canceled"
	case Review:
		r.Next = "operations.next.review"
	case AwaitingRestart:
		r.Tone = "warning"
		r.Next = "operations.next.restart"
	case Failed:
		r.Tone = "danger"
		r.Next = "operations.next.inspect"
	case RecoveryNeeded:
		r.Tone = "warning"
		r.Next = "operations.next.recover"
	default:
		r.State = ""
		r.Label = "operations.state.idle"
		r.Next = "operations.next.idle"
	}
	return r
}
