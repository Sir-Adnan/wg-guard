package operation

import "testing"

func TestReceiptsSeparateReviewPendingExecutionAndRecovery(t *testing.T) {
	for _, state := range []State{Queued, Running, Scheduled, AwaitingRestart, Review, Succeeded, Failed, Canceled} {
		r := Present("opaque", state, false)
		if r.State != state || r.Label == "" || r.Next == "" {
			t.Fatal("receipt lost state")
		}
		if state == AwaitingRestart && state.Active() {
			t.Fatal("approved backup represented an executing operation")
		}
	}
	if r := Present("opaque", Failed, true); r.State != RecoveryNeeded || r.Next != "operations.next.recover" {
		t.Fatal("failed recovery lacked actionable state")
	}
	if r := Present("", State("untrusted message"), false); r.State != "" || r.Label != "operations.state.idle" {
		t.Fatal("unknown state became product text")
	}
}
