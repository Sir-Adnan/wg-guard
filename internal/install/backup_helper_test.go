package install

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

type backupHelperHost struct {
	*memHost
	oldErr     error
	helperErr  error
	helperRuns int
}

func (h *backupHelperHost) Output(ctx context.Context, args []string, timeout time.Duration) (string, error) {
	if len(args) > 1 && args[0] == "docker" && args[1] == "exec" && strings.Contains(strings.Join(args, " "), "backup create") {
		return "", h.oldErr
	}
	if args[0] == "env" {
		h.helperRuns++
		h.commands = append(h.commands, memCmd{argv: args})
		if h.helperErr != nil {
			return "", h.helperErr
		}
		dir := args[len(args)-1]
		h.files[dir+"/wg-guard-helper.wgg"] = memFile{data: []byte("age-encryption.org/v1\nfixture"), perm: 0600}
		return "created wg-guard-helper.wgg (1 KiB, age-encrypted)\n", nil
	}
	return h.memHost.Output(ctx, args, timeout)
}

func TestDockerBackupCompatibleHelperRecovery(t *testing.T) {
	for _, tc := range []string{"success", "wrong_key", "incompatible", "missing_lease", "tampered_helper", "custom_layout", "unrelated_error"} {
		t.Run(tc, func(t *testing.T) {
			m := installedFixture(t, ModeDocker)
			st, err := LoadState(m)
			if err != nil {
				t.Fatal(err)
			}
			h := &backupHelperHost{memHost: m, oldErr: errors.New("secrets: master key does not decrypt existing node data; restore the matching key or archive")}
			candidate := &Artifact{Binary: ArtifactDir + "/" + strings.Repeat("a", 32) + "/binary", Contract: CurrentContract()}
			m.files[candidate.Binary] = memFile{data: []byte("verified helper"), perm: 0755}
			candidate.BinarySHA256 = fmt.Sprintf("%x", sha256.Sum256(m.files[candidate.Binary].data))
			previous := &Artifact{Contract: CurrentContract()}
			switch tc {
			case "wrong_key":
				h.helperErr = h.oldErr
			case "incompatible":
				candidate.Contract.DataContract = "different-schema"
			case "missing_lease":
				previous.Contract.DataLease = false
			case "tampered_helper":
				m.files[candidate.Binary] = memFile{data: []byte("changed helper"), perm: 0755}
			case "custom_layout":
				m.files[st.ConfigPath] = memFile{data: []byte("data_dir = '/custom/data'\n"), perm: 0600}
			case "unrelated_error":
				h.oldErr = errors.New("snapshot disk full")
			}
			before := string(m.files[StatePath].data)
			b, err := createUpdateBackup(context.Background(), h, st, strings.Repeat("b", 32), previous, candidate, io.Discard)
			if tc == "success" {
				if err != nil || b == nil || !b.Encrypted || b.SHA256 == "" || h.helperRuns != 1 {
					t.Fatalf("compatible backup retry: %+v %v runs=%d", b, err, h.helperRuns)
				}
				args := h.commands[len(h.commands)-1].argv
				for _, pin := range []string{"WGG_DATA_DIR=" + DataDir, "WGG_DATABASE_PATH=" + DataDir + "/wg-guard.db", "WGG_MASTER_KEY_FILE=" + DataDir + "/master.key", candidate.Binary, st.ConfigPath} {
					if !strings.Contains(strings.Join(args, " "), pin) {
						t.Fatalf("helper path not pinned: %s", pin)
					}
				}
			} else if err == nil || b != nil {
				t.Fatal("failed safety gate accepted a backup")
			}
			if tc != "success" && tc != "wrong_key" && h.helperRuns != 0 {
				t.Fatal("unsafe/unrelated failure invoked candidate helper")
			}
			if string(m.files[StatePath].data) != before || h.ran("docker", "restart") {
				t.Fatal("backup attempt changed active deployment")
			}
		})
	}
}
