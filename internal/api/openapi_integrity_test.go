package api

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The published contract stays machine-usable: every reference resolves,
// every operation has a unique operationId, authenticated operations list
// their auth and rate-limit failures, idempotent routes declare the key, and
// every machine error code is documented.
func TestOpenAPIDocumentIntegrity(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(openapiJSON, &doc); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok && !resolves(doc, ref) {
				t.Errorf("unresolved $ref %s", ref)
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(doc)

	paths := doc["paths"].(map[string]any)
	ids := map[string]string{}
	for path, item := range paths {
		for method, raw := range item.(map[string]any) {
			op, ok := raw.(map[string]any)
			if !ok || !strings.Contains("get post put patch delete", method) {
				continue
			}
			id, _ := op["operationId"].(string)
			if id == "" {
				t.Errorf("%s %s has no operationId", method, path)
			} else if other, dup := ids[id]; dup {
				t.Errorf("operationId %s used by %s and %s %s", id, other, method, path)
			}
			ids[id] = method + " " + path
		}
	}

	e := newEnv(t)
	for _, r := range e.srv.routes {
		op := paths[r.Path].(map[string]any)[strings.ToLower(r.Method)].(map[string]any)
		responses := op["responses"].(map[string]any)
		if r.Scope != "" {
			for _, code := range []string{"401", "403", "429"} {
				if _, ok := responses[code]; !ok {
					t.Errorf("%s %s does not document %s", r.Method, r.Path, code)
				}
			}
		}
		if r.Idempotent {
			declared := false
			for _, p := range asSlice(op["parameters"]) {
				ref, _ := p.(map[string]any)["$ref"].(string)
				declared = declared || strings.HasSuffix(ref, "/IdempotencyKey") || strings.HasSuffix(ref, "/IntegrationKey")
			}
			if !declared {
				t.Errorf("idempotent %s %s does not declare Idempotency-Key", r.Method, r.Path)
			}
		}
	}

	source, err := os.ReadFile("../domain/errors.go")
	if err != nil {
		t.Fatal(err)
	}
	errorDoc := doc["components"].(map[string]any)["schemas"].(map[string]any)["Error"].(map[string]any)
	codeDoc := errorDoc["properties"].(map[string]any)["error"].(map[string]any)["properties"].(map[string]any)["code"].(map[string]any)["description"].(string)
	for _, m := range regexp.MustCompile(`Code\w+\s+=\s+"([A-Z_]+)"`).FindAllStringSubmatch(string(source), -1) {
		if !strings.Contains(codeDoc, m[1]) {
			t.Errorf("error code %s is not documented in Error.code", m[1])
		}
	}
}

func resolves(doc map[string]any, ref string) bool {
	if !strings.HasPrefix(ref, "#/") {
		return false
	}
	var node any = doc
	for _, part := range strings.Split(ref[2:], "/") {
		part = strings.NewReplacer("~1", "/", "~0", "~").Replace(part)
		m, ok := node.(map[string]any)
		if !ok {
			return false
		}
		if node, ok = m[part]; !ok {
			return false
		}
	}
	return true
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}
