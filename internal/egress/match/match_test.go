package match

import (
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/egress"
)

func mustParse(t *testing.T, text string) []egress.Matcher {
	t.Helper()
	m, err := egress.ParseMatchers(text)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDomainsMatchSemantics(t *testing.T) {
	d := NewDomains(mustParse(t, "example.com\nfull:exact.org\nkeyword:openai\nregexp:^api[0-9]+\\.test$\n1.2.3.0/24"))
	cases := map[string]bool{
		"example.com":         true,
		"a.b.example.com":     true,
		"notexample.com":      false,
		"exact.org":           true,
		"www.exact.org":       false,
		"cdn.openai-x.net":    true,
		"api42.test":          true,
		"api.test":            false,
		"com":                 false,
		"":                    false,
		"example.com.evil.io": false,
	}
	for name, want := range cases {
		if got := d.Match(name); got != want {
			t.Errorf("Match(%q) = %v, want %v", name, got, want)
		}
	}
	if d.Len() != 4 {
		t.Fatalf("CIDR lines must not compile into domain matchers: %d", d.Len())
	}
}

func TestRulesetFirstMatchWinsAndNormalizes(t *testing.T) {
	r := &Ruleset{Entries: []Entry{
		{RuleID: "first", SetName: "a", Domains: NewDomains(mustParse(t, "google.com"))},
		{RuleID: "second", SetName: "b", Domains: NewDomains(mustParse(t, "gemini.google.com\nspotify.com"))},
	}}
	if e, ok := r.Lookup("Gemini.Google.com."); !ok || e.RuleID != "first" {
		t.Fatalf("first match must win: %+v %v", e, ok)
	}
	if e, ok := r.Lookup("open.spotify.com"); !ok || e.SetName != "b" {
		t.Fatalf("second rule: %+v %v", e, ok)
	}
	if _, ok := r.Lookup("example.net"); ok {
		t.Fatal("unexpected match")
	}
	if _, ok := r.Lookup("bad..name"); ok {
		t.Fatal("invalid names never match")
	}
	var empty *Ruleset
	if _, ok := empty.Lookup("google.com"); ok || !empty.Empty() {
		t.Fatal("nil ruleset matches nothing")
	}
}
