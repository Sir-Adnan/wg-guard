package install

import "testing"

func TestOldReleaseSelectionCannotDiscardOverflowPools(t *testing.T) {
	current := &Artifact{Contract: CurrentContract()}
	old := &Artifact{Contract: CurrentContract()}
	old.Contract.DataContract = "schema7-h-ranges-v1"
	if dataCompatible(current, old) || !incompatiblePoolDowngrade(current, old) {
		t.Fatal("old pool-unaware binary admitted as compatible")
	}
	if incompatiblePoolDowngrade(old, current) {
		t.Fatal("forward migration rejected")
	}
	if incompatiblePoolDowngrade(current, current) {
		t.Fatal("compatible revision rejected")
	}
}
