// Package geodata reads the community geosite.dat and geoip.dat formats
// (v2fly/Xray "routercommon" protobuf messages) without a protobuf
// dependency: the schema is four small messages and the wire format is the
// stable protobuf encoding.
//
//	GeoSiteList { repeated GeoSite entry = 1 }
//	GeoSite     { string country_code = 1; repeated Domain domain = 2 }
//	Domain      { Type type = 1; string value = 2; repeated Attribute attribute = 3 }
//	            Type: 0 plain (keyword), 1 regex, 2 domain (suffix), 3 full
//	Attribute   { string key = 1; ... }
//	GeoIPList   { repeated GeoIP entry = 1 }
//	GeoIP       { string country_code = 1; repeated CIDR cidr = 2; bool reverse_match = 3 }
//	CIDR        { bytes ip = 1; uint32 prefix = 2 }
//
// Only the requested categories are materialized; everything else is skipped
// by length without allocation.
package geodata

import (
	"errors"
	"net/netip"
	"sort"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/egress"
)

// ErrMalformed reports an unreadable data file.
var ErrMalformed = errors.New("geodata: malformed data file")

// Bounds on one category's materialized size.
const (
	maxDomainsPerCategory  = 200_000
	maxPrefixesPerCategory = 100_000
)

type reader struct {
	b []byte
	i int
}

func (r *reader) done() bool { return r.i >= len(r.b) }

func (r *reader) varint() (uint64, error) {
	var v uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if r.i >= len(r.b) {
			return 0, ErrMalformed
		}
		c := r.b[r.i]
		r.i++
		v |= uint64(c&0x7f) << shift
		if c < 0x80 {
			return v, nil
		}
	}
	return 0, ErrMalformed
}

// field reads one key and returns (number, wiretype).
func (r *reader) field() (int, int, error) {
	k, err := r.varint()
	if err != nil {
		return 0, 0, err
	}
	return int(k >> 3), int(k & 7), nil
}

func (r *reader) bytes() ([]byte, error) {
	n, err := r.varint()
	if err != nil {
		return nil, err
	}
	if n > uint64(len(r.b)-r.i) {
		return nil, ErrMalformed
	}
	out := r.b[r.i : r.i+int(n)]
	r.i += int(n)
	return out, nil
}

func (r *reader) skip(wire int) error {
	switch wire {
	case 0:
		_, err := r.varint()
		return err
	case 1:
		if len(r.b)-r.i < 8 {
			return ErrMalformed
		}
		r.i += 8
	case 2:
		_, err := r.bytes()
		return err
	case 5:
		if len(r.b)-r.i < 4 {
			return ErrMalformed
		}
		r.i += 4
	default:
		return ErrMalformed
	}
	return nil
}

// entries iterates the top-level repeated field 1 and passes each entry's
// country code (lowercased) and raw message.
func entries(data []byte, fn func(code string, msg []byte) error) error {
	r := &reader{b: data}
	for !r.done() {
		num, wire, err := r.field()
		if err != nil {
			return err
		}
		if num != 1 || wire != 2 {
			if err := r.skip(wire); err != nil {
				return err
			}
			continue
		}
		msg, err := r.bytes()
		if err != nil {
			return err
		}
		code, err := countryCode(msg)
		if err != nil {
			return err
		}
		if err := fn(code, msg); err != nil {
			return err
		}
	}
	return nil
}

func countryCode(msg []byte) (string, error) {
	r := &reader{b: msg}
	for !r.done() {
		num, wire, err := r.field()
		if err != nil {
			return "", err
		}
		if num == 1 && wire == 2 {
			b, err := r.bytes()
			if err != nil {
				return "", err
			}
			return strings.ToLower(string(b)), nil
		}
		if err := r.skip(wire); err != nil {
			return "", err
		}
	}
	return "", nil
}

// Categories lists the category names of a geosite.dat or geoip.dat file.
func Categories(data []byte) ([]string, error) {
	var out []string
	err := entries(data, func(code string, _ []byte) error {
		if code != "" {
			out = append(out, code)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// spec is one requested category with an optional attribute filter:
// "google", "google@cn" (only domains carrying @cn) or "google@!cn".
type spec struct {
	raw, code, attr string
	negate          bool
}

func parseSpec(s string) spec {
	sp := spec{raw: s, code: s}
	if code, attr, ok := strings.Cut(s, "@"); ok {
		sp.code = code
		if strings.HasPrefix(attr, "!") {
			sp.negate, attr = true, attr[1:]
		}
		sp.attr = attr
	}
	return sp
}

// Geosite materializes the requested categories as egress matchers, keyed by
// the requested spec. Missing categories are absent from the result.
func Geosite(data []byte, wanted []string) (map[string][]egress.Matcher, error) {
	byCode := map[string][]spec{}
	for _, w := range wanted {
		sp := parseSpec(strings.ToLower(w))
		byCode[sp.code] = append(byCode[sp.code], sp)
	}
	out := map[string][]egress.Matcher{}
	err := entries(data, func(code string, msg []byte) error {
		specs, ok := byCode[code]
		if !ok {
			return nil
		}
		return decodeSite(msg, specs, out)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func decodeSite(msg []byte, specs []spec, out map[string][]egress.Matcher) error {
	r := &reader{b: msg}
	for !r.done() {
		num, wire, err := r.field()
		if err != nil {
			return err
		}
		if num != 2 || wire != 2 {
			if err := r.skip(wire); err != nil {
				return err
			}
			continue
		}
		dmsg, err := r.bytes()
		if err != nil {
			return err
		}
		m, attrs, err := decodeDomain(dmsg)
		if err != nil {
			return err
		}
		if m.Value == "" {
			continue
		}
		for _, sp := range specs {
			if sp.attr != "" {
				has := attrs[sp.attr]
				if has == sp.negate {
					continue
				}
			}
			if len(out[sp.raw]) >= maxDomainsPerCategory {
				return ErrMalformed
			}
			out[sp.raw] = append(out[sp.raw], m)
		}
	}
	return nil
}

func decodeDomain(msg []byte) (egress.Matcher, map[string]bool, error) {
	r := &reader{b: msg}
	var typ uint64
	var value string
	var attrs map[string]bool
	for !r.done() {
		num, wire, err := r.field()
		if err != nil {
			return egress.Matcher{}, nil, err
		}
		switch {
		case num == 1 && wire == 0:
			if typ, err = r.varint(); err != nil {
				return egress.Matcher{}, nil, err
			}
		case num == 2 && wire == 2:
			b, err := r.bytes()
			if err != nil {
				return egress.Matcher{}, nil, err
			}
			value = string(b)
		case num == 3 && wire == 2:
			b, err := r.bytes()
			if err != nil {
				return egress.Matcher{}, nil, err
			}
			key, err := countryCode(b) // Attribute.key is field 1 as well
			if err != nil {
				return egress.Matcher{}, nil, err
			}
			if attrs == nil {
				attrs = map[string]bool{}
			}
			attrs[key] = true
		default:
			if err := r.skip(wire); err != nil {
				return egress.Matcher{}, nil, err
			}
		}
	}
	var m egress.Matcher
	switch typ {
	case 0:
		m = egress.Matcher{Type: egress.MatchKeyword, Value: strings.ToLower(value)}
	case 1:
		m = egress.Matcher{Type: egress.MatchRegexp, Value: value}
	case 2:
		v, ok := egress.NormalizeDomain(value)
		if !ok {
			return egress.Matcher{}, attrs, nil // tolerated: skip unusable names
		}
		m = egress.Matcher{Type: egress.MatchDomain, Value: v}
	case 3:
		v, ok := egress.NormalizeDomain(value)
		if !ok {
			return egress.Matcher{}, attrs, nil
		}
		m = egress.Matcher{Type: egress.MatchFull, Value: v}
	default:
		return egress.Matcher{}, attrs, nil
	}
	return m, attrs, nil
}

// GeoIP materializes the IPv4 prefixes of the requested categories. A
// reverse_match category and IPv6 prefixes are skipped (out of scope).
func GeoIP(data []byte, wanted []string) (map[string][]netip.Prefix, error) {
	want := map[string]bool{}
	for _, w := range wanted {
		want[strings.ToLower(w)] = true
	}
	out := map[string][]netip.Prefix{}
	err := entries(data, func(code string, msg []byte) error {
		if !want[code] {
			return nil
		}
		prefixes, err := decodeGeoIP(msg)
		if err != nil {
			return err
		}
		out[code] = append(out[code], prefixes...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func decodeGeoIP(msg []byte) ([]netip.Prefix, error) {
	r := &reader{b: msg}
	var out []netip.Prefix
	reverse := false
	for !r.done() {
		num, wire, err := r.field()
		if err != nil {
			return nil, err
		}
		switch {
		case num == 2 && wire == 2:
			b, err := r.bytes()
			if err != nil {
				return nil, err
			}
			p, ok, err := decodeCIDR(b)
			if err != nil {
				return nil, err
			}
			if ok {
				if len(out) >= maxPrefixesPerCategory {
					return nil, ErrMalformed
				}
				out = append(out, p)
			}
		case num == 3 && wire == 0:
			v, err := r.varint()
			if err != nil {
				return nil, err
			}
			reverse = v != 0
		default:
			if err := r.skip(wire); err != nil {
				return nil, err
			}
		}
	}
	if reverse {
		return nil, nil
	}
	return out, nil
}

func decodeCIDR(msg []byte) (netip.Prefix, bool, error) {
	r := &reader{b: msg}
	var ip []byte
	var bits uint64
	for !r.done() {
		num, wire, err := r.field()
		if err != nil {
			return netip.Prefix{}, false, err
		}
		switch {
		case num == 1 && wire == 2:
			if ip, err = r.bytes(); err != nil {
				return netip.Prefix{}, false, err
			}
		case num == 2 && wire == 0:
			if bits, err = r.varint(); err != nil {
				return netip.Prefix{}, false, err
			}
		default:
			if err := r.skip(wire); err != nil {
				return netip.Prefix{}, false, err
			}
		}
	}
	if len(ip) != 4 || bits > 32 {
		return netip.Prefix{}, false, nil // IPv6 or malformed entry: skipped
	}
	addr := netip.AddrFrom4([4]byte(ip))
	p := netip.PrefixFrom(addr, int(bits)).Masked()
	if p.Bits() == 0 {
		return netip.Prefix{}, false, nil
	}
	return p, true, nil
}
