// Package match evaluates domain-name matchers for the egress resolver. A
// Ruleset is compiled once per routing apply and then read concurrently by
// resolver goroutines without locks (it is immutable after Compile).
package match

import (
	"regexp"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/egress"
)

// Domains is one rule's compiled domain matchers.
type Domains struct {
	full     map[string]struct{}
	suffix   map[string]struct{}
	keywords []string
	regexps  []*regexp.Regexp
}

// NewDomains compiles domain-type matchers. Non-domain matchers and invalid
// expressions (already rejected at save time) are ignored.
func NewDomains(matchers []egress.Matcher) *Domains {
	d := &Domains{full: map[string]struct{}{}, suffix: map[string]struct{}{}}
	for _, m := range matchers {
		switch m.Type {
		case egress.MatchFull:
			d.full[m.Value] = struct{}{}
		case egress.MatchDomain:
			d.suffix[m.Value] = struct{}{}
		case egress.MatchKeyword:
			d.keywords = append(d.keywords, m.Value)
		case egress.MatchRegexp:
			if re, err := regexp.Compile(m.Value); err == nil {
				d.regexps = append(d.regexps, re)
			}
		}
	}
	return d
}

// Len is the number of compiled matchers.
func (d *Domains) Len() int {
	if d == nil {
		return 0
	}
	return len(d.full) + len(d.suffix) + len(d.keywords) + len(d.regexps)
}

// Match reports whether name (normalized: lowercase, no trailing dot) matches.
func (d *Domains) Match(name string) bool {
	if d == nil || name == "" {
		return false
	}
	if _, ok := d.full[name]; ok {
		return true
	}
	// Walk suffixes label by label: a.b.example.com, b.example.com, example.com, com.
	for s := name; ; {
		if _, ok := d.suffix[s]; ok {
			return true
		}
		i := strings.IndexByte(s, '.')
		if i < 0 {
			break
		}
		s = s[i+1:]
	}
	for _, k := range d.keywords {
		if strings.Contains(name, k) {
			return true
		}
	}
	for _, re := range d.regexps {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// Entry binds one rule's domain matchers to the nftables set its resolved
// addresses join.
type Entry struct {
	RuleID  string
	SetName string
	Domains *Domains
}

// Ruleset is the ordered first-match list the resolver consults.
type Ruleset struct {
	Entries []Entry
}

// Lookup returns the first entry matching name.
func (r *Ruleset) Lookup(name string) (Entry, bool) {
	if r == nil {
		return Entry{}, false
	}
	name, ok := egress.NormalizeDomain(name)
	if !ok {
		return Entry{}, false
	}
	for _, e := range r.Entries {
		if e.Domains.Match(name) {
			return e, true
		}
	}
	return Entry{}, false
}

// Empty reports whether no rule matches names.
func (r *Ruleset) Empty() bool { return r == nil || len(r.Entries) == 0 }
