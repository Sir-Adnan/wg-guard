package install

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRestoreRejectsNoncanonicalLayoutBeforePrepareOrStop(t *testing.T) {
	for _, field := range []string{"data_dir", "database_path", "master_key_file"} {
		t.Run(field, func(t *testing.T) {
			h := installedFixture(t, ModeNative)
			h.files[ConfigPath] = memFile{data: []byte(field + " = '/outside/restore'\n"), perm: 0600}
			before := len(h.commands)
			journalBefore := string(h.files[JournalPath].data)
			prepared := false
			err := Restore(context.Background(), h, RestoreOptions{Prepare: func(context.Context, *BackupIdentity) (func(context.Context) error, error) {
				prepared = true
				return func(context.Context) error { return nil }, nil
			}})
			if err == nil || prepared {
				t.Fatalf("noncanonical layout reached preparation: %v", err)
			}
			if len(h.commands) != before {
				t.Fatal("service mutation/probe before layout refusal")
			}
			if string(h.files[JournalPath].data) != journalBefore {
				t.Fatal("journal changed on layout refusal")
			}
		})
	}
}

func TestRestoreCoordinatesBothModes(t *testing.T) {
	for _, mode := range []Mode{ModeNative, ModeDocker} {
		t.Run(string(mode), func(t *testing.T) {
			h := installedFixture(t, mode)
			contractFixture(h)
			applied := false
			err := Restore(context.Background(), h, RestoreOptions{Prepare: func(ctx context.Context, b *BackupIdentity) (func(context.Context) error, error) {
				return func(context.Context) error {
					joined := ""
					for _, c := range h.commands {
						joined += strings.Join(c.argv, " ") + "\n"
					}
					if !strings.Contains(joined, "systemctl stop") && !strings.Contains(joined, " down") {
						t.Fatal("applied before stopping service")
					}
					applied = true
					return nil
				}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			if !applied {
				t.Fatal("restore not applied")
			}
			j, err := LoadJournal(h)
			if err != nil || j.Stage != "complete" {
				t.Fatal("restore not committed")
			}
		})
	}
}
func TestRestorePreviewCancellationDoesNotStopService(t *testing.T) {
	h := installedFixture(t, ModeNative)
	contractFixture(h)
	err := Restore(context.Background(), h, RestoreOptions{Prepare: func(context.Context, *BackupIdentity) (func(context.Context) error, error) {
		return nil, errors.New("review canceled")
	}})
	if err == nil {
		t.Fatal("cancellation ignored")
	}
	for _, cmd := range h.commands {
		if strings.Contains(strings.Join(cmd.argv, " "), "systemctl stop") {
			t.Fatal("service stopped before approval")
		}
	}
}

func TestCandidateRequiresCoordinatedRestoreWithoutChangingDataCompatibility(t *testing.T) {
	prior := CurrentContract()
	prior.CoordinatedRestore = false
	if err := CheckContract(prior); err == nil {
		t.Fatal("candidate without coordinated restore admitted")
	}
	if !dataCompatible(&Artifact{Contract: prior}, &Artifact{Contract: CurrentContract()}) {
		t.Fatal("capability admission changed retained data compatibility")
	}
}
