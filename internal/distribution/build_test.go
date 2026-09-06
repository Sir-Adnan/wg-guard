package distribution

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

type sourceRunner struct {
	t     *testing.T
	calls int
}

func TestRealCompilerProducesStampedLinuxCandidate(t *testing.T) {
	var packed bytes.Buffer
	gz := gzip.NewWriter(&packed)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{
		"go.mod":                      "module github.com/Sir-Adnan/wg-guard\n\ngo 1.25.0\n",
		"cmd/wg-guard/main.go":        "package main\nimport (\"fmt\";\"github.com/Sir-Adnan/wg-guard/internal/version\")\nfunc main(){fmt.Print(version.Commit,version.Version)}\n",
		"internal/version/version.go": "package version\nvar Commit=\"none\"\nvar Version=\"dev\"\n",
	} {
		tw.WriteHeader(&tar.Header{Name: "wg-guard-" + fixtureSHA + "/" + name, Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(body))})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/commits/") {
			fmt.Fprintf(w, `{"sha":"%s"}`, fixtureSHA)
			return
		}
		w.Write(packed.Bytes())
	})
	b, err := c.Acquire(context.Background(), Selection{Channel: "commit", Ref: fixtureSHA}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	info, err := buildinfo.ReadFile(b.BinaryPath)
	if err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{}
	for _, s := range info.Settings {
		settings[s.Key] = s.Value
	}
	if settings["GOOS"] != "linux" || settings["GOARCH"] != "amd64" || settings["CGO_ENABLED"] != "0" {
		t.Fatalf("incorrect target settings %+v", settings)
	}
	linked, err := os.ReadFile(b.BinaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(linked, []byte(fixtureSHA)) || !bytes.Contains(linked, []byte("0.0.0-dev.0123456789ab")) {
		t.Fatal("missing link identity")
	}
}

func (r *sourceRunner) RunConfigured(ctx context.Context, argv []string, dir string, env []string) (subprocess.Result, error) {
	if len(argv) > 1 && argv[1] == "version" {
		return subprocess.Result{Stdout: []byte("go version go1.99.0 linux/amd64")}, nil
	}
	r.calls++
	if !strings.Contains(strings.Join(argv, " "), "-trimpath -buildvcs=false") || argv[len(argv)-1] != "./cmd/wg-guard" {
		r.t.Errorf("unsafe build argv %q", argv)
	}
	if !strings.Contains(strings.Join(env, "\n"), "CGO_ENABLED=0") || !strings.Contains(strings.Join(env, "\n"), "GOSUMDB=sum.golang.org") {
		r.t.Error("missing safe environment")
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		r.t.Error("source not extracted")
	}
	for i, v := range argv {
		if v == "-o" {
			return subprocess.Result{}, os.WriteFile(argv[i+1], []byte("built candidate"), 0600)
		}
	}
	return subprocess.Result{}, fmt.Errorf("missing output")
}
func archiveFixture(t *testing.T, name string, kind byte) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	body := []byte("module github.com/Sir-Adnan/wg-guard\n\ngo 1.25.0\n")
	size := int64(len(body))
	if kind != tar.TypeReg {
		size = 0
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: kind, Mode: 0644, Size: size, Linkname: "../../escape"}); err != nil {
		t.Fatal(err)
	}
	if size > 0 {
		tw.Write(body)
	}
	tw.Close()
	gz.Close()
	return b.Bytes()
}
func TestAcquireSourceRejectsUnsafeArchivesBeforeBuild(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind byte
		fail bool
	}{
		{"wg-guard-" + fixtureSHA + "/go.mod", tar.TypeReg, false},
		{"../escape", tar.TypeReg, true}, {"/absolute", tar.TypeReg, true}, {"root/../../escape", tar.TypeReg, true},
		{"wg-guard-" + fixtureSHA + "/go.mod", tar.TypeSymlink, true}, {"wg-guard-" + fixtureSHA + "/go.mod", tar.TypeLink, true}, {"wg-guard-" + fixtureSHA + "/go.mod", tar.TypeFifo, true},
		{"root/drive:C", tar.TypeReg, true},
	} {
		t.Run(tc.name+fmt.Sprint(tc.kind), func(t *testing.T) {
			content := archiveFixture(t, tc.name, tc.kind)
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/commits/") {
					fmt.Fprintf(w, `{"sha":"%s"}`, fixtureSHA)
					return
				}
				if r.URL.Path != "/Sir-Adnan/wg-guard/tar.gz/"+fixtureSHA {
					t.Errorf("mutable source path %s", r.URL.Path)
				}
				w.Write(content)
			})
			runner := &sourceRunner{t: t}
			c.options.Runner = runner
			dir := t.TempDir()
			b, err := c.Acquire(context.Background(), Selection{Channel: "commit", Ref: "main"}, dir)
			if (err != nil) != tc.fail {
				t.Fatalf("source acquisition error=%v want fail %v", err, tc.fail)
			}
			if tc.fail {
				if tc.kind != tar.TypeReg && !strings.Contains(err.Error(), "unsupported archive member") {
					t.Fatalf("special member did not reach type validation: %v", err)
				}
				if runner.calls != 0 {
					t.Fatal("unsafe source built")
				}
				entries, _ := os.ReadDir(dir)
				if len(entries) != 0 {
					t.Fatal("failure left artifact")
				}
				return
			}
			if b.Commit != fixtureSHA || b.Ref != fixtureSHA || b.SHA256 == "" {
				t.Fatalf("missing immutable identity %+v", b)
			}
		})
	}
}

func TestToolchainChecksumBeforeExecution(t *testing.T) {
	data := archiveFixture(t, "go/bin/go", tar.TypeReg)
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, corrupt := range []bool{false, true} {
		t.Run(fmt.Sprint(corrupt), func(t *testing.T) {
			var c *Client
			c = fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/dl/" {
					fmt.Fprintf(w, `[{"version":"go1.99.0","stable":true,"files":[{"filename":"go1.99.0.linux-amd64.tar.gz","os":"linux","arch":"amd64","kind":"archive","sha256":"%s","size":%d}]}]`, sum, len(data))
					return
				}
				if corrupt {
					w.Write([]byte("corrupt"))
					return
				}
				w.Write(data)
			})
			c.options.GoBase = c.options.APIBase
			path, err := c.downloadCompiler(context.Background(), "go1.25.0", t.TempDir())
			if (err != nil) != corrupt {
				t.Fatalf("compiler error %v want failure %v", err, corrupt)
			}
			if err == nil {
				if _, err := os.Stat(path); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
