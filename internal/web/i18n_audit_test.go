package web

import (
	"io/fs"
	"path"
	"strings"
	"testing"
	"text/template"
	"text/template/parse"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	webassets "github.com/Sir-Adnan/wg-guard/web"
)

// TestTemplatesUseKnownI18nKeys walks every embedded template and fails when a
// constant string passed to the .T helper is not a catalog key in BOTH locales.
// i18n.T echoes an unknown key verbatim, so a leaked key would render raw text
// like "bulk.create" in the UI — this keeps that class of bug out for good.
func TestTemplatesUseKnownI18nKeys(t *testing.T) {
	files, err := fs.Glob(webassets.FS, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no templates found")
	}
	for _, f := range files {
		raw, err := fs.ReadFile(webassets.FS, f)
		if err != nil {
			t.Fatal(err)
		}
		// Parse through text/template (same custom funcs initTemplates
		// registers) so the builtins are present and the trees are walkable.
		tt := template.New(path.Base(f)).Funcs(template.FuncMap{
			"list":    func(items ...string) []string { return items },
			"dict":    func(kvs ...any) map[string]any { return nil },
			"i64u":    func(v any) int64 { return 0 },
			"timePtr": func(t time.Time) *time.Time { return nil },
		})
		if _, err := tt.Parse(string(raw)); err != nil {
			t.Fatalf("%s: parse: %v", f, err)
		}
		for _, tr := range tt.Templates() {
			if tr.Tree == nil {
				continue
			}
			walkTemplateNode(t, f, tr.Name(), tr.Tree.Root)
		}
	}
}

// walkTemplateNode recurses the parse tree looking for commands whose callable
// is a .T / variable T method call with a constant first argument.
func walkTemplateNode(t *testing.T, file, tmpl string, n parse.Node) {
	t.Helper()
	switch node := n.(type) {
	case *parse.ListNode:
		if node == nil {
			return
		}
		for _, child := range node.Nodes {
			walkTemplateNode(t, file, tmpl, child)
		}
	case *parse.IfNode:
		walkPipe(t, file, tmpl, node.Pipe)
		walkTemplateNode(t, file, tmpl, node.List)
		walkTemplateNode(t, file, tmpl, node.ElseList)
	case *parse.WithNode:
		walkPipe(t, file, tmpl, node.Pipe)
		walkTemplateNode(t, file, tmpl, node.List)
		walkTemplateNode(t, file, tmpl, node.ElseList)
	case *parse.RangeNode:
		walkPipe(t, file, tmpl, node.Pipe)
		walkTemplateNode(t, file, tmpl, node.List)
		walkTemplateNode(t, file, tmpl, node.ElseList)
	case *parse.ActionNode:
		walkPipe(t, file, tmpl, node.Pipe)
	}
}

func walkPipe(t *testing.T, file, tmpl string, pipe *parse.PipeNode) {
	t.Helper()
	if pipe == nil {
		return
	}
	for _, cmd := range pipe.Cmds {
		if len(cmd.Args) < 2 {
			continue
		}
		method, ok := tMethodName(cmd.Args[0])
		if !ok || method != "T" {
			continue
		}
		str, ok := cmd.Args[1].(*parse.StringNode)
		if !ok {
			continue // dynamic key (printf, range variable) — can't audit statically
		}
		if strings.ContainsAny(str.Text, "%!") {
			continue // format string bug would show in rendering tests
		}
		if i18n.T(i18n.En, str.Text) == str.Text || i18n.T(i18n.Fa, str.Text) == str.Text {
			t.Errorf("%s [%s]: template key %q is missing from a catalog (raw-key leak)",
				file, tmpl, str.Text)
		}
	}
}

// tMethodName reports whether the arg is a .T or $x.T call, returning "T".
func tMethodName(arg parse.Node) (string, bool) {
	switch a := arg.(type) {
	case *parse.FieldNode:
		if len(a.Ident) == 1 && a.Ident[0] == "T" {
			return "T", true
		}
	case *parse.VariableNode:
		if len(a.Ident) == 2 && a.Ident[1] == "T" {
			return "T", true
		}
	}
	return "", false
}

// TestStatusLabelsCoverLifecycle pins the dynamic St() keys: every domain user
// status must resolve in both locales, or the users list renders raw keys.
func TestStatusLabelsCoverLifecycle(t *testing.T) {
	statuses := []domain.UserStatus{
		domain.UserActive, domain.UserDisabled, domain.UserSuspended,
		domain.UserExpired, domain.UserTrafficExceeded, domain.UserWaitingFirstConnection,
	}
	for _, st := range statuses {
		key := "status." + string(st)
		if i18n.T(i18n.En, key) == key || i18n.T(i18n.Fa, key) == key {
			t.Errorf("status label %q missing from a catalog", key)
		}
	}
}
