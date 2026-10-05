package layout

import (
	"path"
	"testing"
)

func TestHostAuthorityIsOutsideNodeWritableMount(t *testing.T) {
	for _, file := range []string{InstallState, LifecycleJournal, LifecycleArtifacts, HostBinary, ManagerCache, ComposeFile} {
		if file == DataDir || path.Dir(file) == DataDir {
			t.Fatal("host authority moved into node-writable data", file)
		}
	}
	if CheckManagedData(ConfigFile, DataDir, DatabaseFile(DataDir), MasterKeyFile(DataDir)) != nil {
		t.Fatal("managed layout rejected")
	}
	if CheckManagedData(ConfigFile, DataDir, "/another/database", MasterKeyFile(DataDir)) == nil {
		t.Fatal("managed restore accepted an arbitrary replacement target")
	}
}
