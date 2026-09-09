package install

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type credentialOrderWriter struct {
	t *testing.T
	h *memHost
	b bytes.Buffer
}

func (w *credentialOrderWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("Password: ")) {
		journal, err := LoadJournal(w.h)
		if err != nil || journal == nil || journal.Stage != "complete" {
			w.t.Errorf("credentials displayed before lifecycle completion: journal=%+v err=%v", journal, err)
		}
	}
	return w.b.Write(p)
}

func (w *credentialOrderWriter) String() string { return w.b.String() }

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

func TestLocalOwnerDefaultsToAdminAndRetriesInvalidManualPassword(t *testing.T) {
	h := newMemHost()
	p := Defaults()
	h.output[BinPath+" owner-bootstrap --config "+p.BootConfigPath()+" --check"] = "absent\n"
	var out bytes.Buffer
	result := OwnerResult{}
	input := strings.Join([]string{
		"",                       // default username: admin
		"short",                  // rejected locally
		"synthetic-password-123", // valid, but confirmation mismatches
		"different-password-123",
		"synthetic-password-123", // retry
		"synthetic-password-123",
	}, "\n") + "\n"
	if err := BootstrapLocalOwner(context.Background(), h, p, OwnerOptions{Stdin: strings.NewReader(input), Stdout: &out, Result: &result}); err != nil {
		t.Fatal(err)
	}
	if result.Username != "admin" || result.GeneratedPassword != "" || !result.Created {
		t.Fatalf("owner result = %+v", result)
	}
	var payload OwnerInput
	for _, command := range h.commands {
		if len(command.stdin) > 0 {
			if err := json.Unmarshal(command.stdin, &payload); err != nil {
				t.Fatal(err)
			}
		}
	}
	if payload.Username != "admin" || payload.Password != "synthetic-password-123" {
		t.Fatalf("bootstrap payload username=%q password accepted=%v", payload.Username, payload.Password == "synthetic-password-123")
	}
	text := out.String()
	if !strings.Contains(text, "at least 10") || !strings.Contains(text, "do not match") {
		t.Fatalf("retry guidance missing:\n%s", text)
	}
	if strings.Contains(text, payload.Password) {
		t.Fatal("manual password echoed to terminal output")
	}
}

func TestLocalOwnerGeneratesPasswordWhenInteractiveInputIsBlank(t *testing.T) {
	h := newMemHost()
	p := Defaults()
	h.output[BinPath+" owner-bootstrap --config "+p.BootConfigPath()+" --check"] = "absent\n"
	var out bytes.Buffer
	result := OwnerResult{}
	if err := BootstrapLocalOwner(context.Background(), h, p, OwnerOptions{Stdin: strings.NewReader("\n\n"), Stdout: &out, Result: &result}); err != nil {
		t.Fatal(err)
	}
	if result.Username != "admin" || len(result.GeneratedPassword) != 24 || !result.Created {
		t.Fatalf("generated owner result = username=%q password-length=%d created=%v", result.Username, len(result.GeneratedPassword), result.Created)
	}
	var payload OwnerInput
	for _, command := range h.commands {
		if len(command.stdin) > 0 {
			if err := json.Unmarshal(command.stdin, &payload); err != nil {
				t.Fatal(err)
			}
		}
	}
	if payload.Username != result.Username || payload.Password != result.GeneratedPassword {
		t.Fatal("generated credentials did not reach the real bootstrap boundary")
	}
	if strings.Contains(out.String(), result.GeneratedPassword) {
		t.Fatal("generated password was displayed before installation completed")
	}
}

func TestSuccessfulInstallDisplaysGeneratedCredentialsOnlyAfterHealth(t *testing.T) {
	h := newMemHost()
	p := Defaults()
	p.PanelPort = healthServer(t, http.StatusOK)
	h.output[BinPath+" owner-bootstrap --config "+p.BootConfigPath()+" --check"] = "absent\n"
	out := &credentialOrderWriter{t: t, h: h}
	// Advanced private setup (the synthetic health port makes it explicit),
	// install review, admin username and blank password. A generated password
	// does not need a confirmation prompt.
	_, err := Install(context.Background(), h, InstallOptions{
		Plan: p, Version: "test", Stdin: strings.NewReader(strings.Repeat("\n", 9)), Stdout: out,
	})
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	var generated string
	for _, command := range h.commands {
		if len(command.stdin) == 0 {
			continue
		}
		var payload OwnerInput
		if json.Unmarshal(command.stdin, &payload) == nil && payload.Username == "admin" {
			generated = payload.Password
		}
	}
	if len(generated) != 24 {
		t.Fatalf("generated password length = %d", len(generated))
	}
	text := out.String()
	if !strings.Contains(text, "Save administrator credentials") || !strings.Contains(text, "Username: admin") || !strings.Contains(text, "Password: "+generated) {
		t.Fatalf("final credential card missing:\n%s", text)
	}
	if strings.Index(text, "Healthy") > strings.Index(text, "Password: "+generated) {
		t.Fatal("generated password displayed before successful health verification")
	}
}
