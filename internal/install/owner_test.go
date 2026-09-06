package install

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestDefaultOwnerHookRunsAfterSettingsBeforeListener(t *testing.T) {
	for _, mode := range []Mode{ModeNative, ModeDocker} {
		h := newMemHost()
		p := Defaults()
		p.Mode = mode
		p.PanelPort = healthServer(t, http.StatusOK)
		h.output[BinPath+" owner-bootstrap --config "+p.BootConfigPath()+" --check"] = "absent\n"
		h.files["/private-password"] = memFile{data: []byte("synthetic-password-123\n"), perm: 0600}
		_, err := Install(context.Background(), h, InstallOptions{Plan: p, Yes: true, Owner: OwnerOptions{Username: "root", PasswordFile: "/private-password"}})
		if err != nil {
			t.Fatal(err)
		}
		seed, owner, start := -1, -1, -1
		for i, c := range h.commands {
			v := strings.Join(c.argv, " ")
			if strings.Contains(v, "settings set node.endpoint") {
				seed = i
			}
			if strings.Contains(v, "owner-bootstrap") && strings.Contains(v, "--stdin") {
				owner = i
			}
			if strings.Contains(v, " up -d") || strings.Contains(v, "enable --now") {
				start = i
			}
		}
		if seed < 0 || owner <= seed || start <= owner {
			t.Fatalf("wrong order seed=%d owner=%d start=%d", seed, owner, start)
		}
		for _, f := range h.files {
			if bytes.Contains(f.data, []byte("synthetic-password-123")) && !bytes.Equal(f.data, []byte("synthetic-password-123\n")) {
				t.Fatal("secret persisted into lifecycle data")
			}
		}
	}
}

func TestLocalOwnerTransportAndReuse(t *testing.T) {
	h := newMemHost()
	p := Defaults()
	var out bytes.Buffer
	h.output[BinPath+" owner-bootstrap --config "+p.BootConfigPath()+" --check"] = "absent\n"
	err := BootstrapLocalOwner(context.Background(), h, p, OwnerOptions{Username: "root", Stdin: strings.NewReader("synthetic-password-123\nsynthetic-password-123\n"), Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, c := range h.commands {
		if strings.Contains(strings.Join(c.argv, " "), "synthetic-password") {
			t.Fatal("secret in argv")
		}
		if len(c.stdin) > 0 {
			found = true
			if !bytes.Contains(c.stdin, []byte("synthetic-password-123")) {
				t.Fatal("missing stdin password")
			}
		}
	}
	if !found || strings.Contains(out.String(), "synthetic-password") {
		t.Fatal("secret transport failed")
	}
	h.commands = nil
	h.output[BinPath+" owner-bootstrap --config "+p.BootConfigPath()+" --check"] = "present\n"
	if err := BootstrapLocalOwner(context.Background(), h, p, OwnerOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	if len(h.commands) != 1 {
		t.Fatal("existing owner changed")
	}
}
func TestLocalOwnerMissingOrMismatchedPasswordRefuses(t *testing.T) {
	for _, o := range []OwnerOptions{{Yes: true}, {Username: "root", Stdin: strings.NewReader("synthetic-password-123\ndifferent-password\n")}} {
		h := newMemHost()
		p := Defaults()
		h.output[BinPath+" owner-bootstrap --config "+p.BootConfigPath()+" --check"] = "absent\n"
		if err := BootstrapLocalOwner(context.Background(), h, p, o); err == nil {
			t.Fatal("owner missing password accepted")
		}
		for _, c := range h.commands {
			if len(c.stdin) > 0 {
				t.Fatal("created after refusal")
			}
		}
	}
}
