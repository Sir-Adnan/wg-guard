package install

import "testing"

func TestPersistentManagerContractAdmissionKeepsRevisionOneDataCompatible(t *testing.T) {
	current := CurrentContract()
	if current.Revision != 2 || !current.PersistentManager || !current.SecureExposure {
		t.Fatalf("current installer contract lacks Phase 8.2 capabilities: %+v", current)
	}

	legacy := current
	legacy.Revision = 1
	legacy.PersistentManager = false
	legacy.SecureExposure = false
	if CheckContract(legacy) == nil {
		t.Fatal("revision-one installer admitted as a fresh-install candidate")
	}
	if !knownDataContract(legacy) {
		t.Fatal("revision-one artifact lost rollback data compatibility")
	}
}
