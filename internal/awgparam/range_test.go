package awgparam

import (
	"encoding/json"
	"math"
	"testing"
)

func TestParseU32Range(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		low       uint32
		high      uint32
		canonical string
		wantErr   bool
	}{
		{name: "zero", input: "0", canonical: "0"},
		{name: "scalar", input: "5", low: 5, high: 5, canonical: "5"},
		{name: "range", input: "5-9", low: 5, high: 9, canonical: "5-9"},
		{name: "equal endpoints", input: "5-5", low: 5, high: 5, canonical: "5"},
		{name: "maximum", input: "4294967295", low: math.MaxUint32, high: math.MaxUint32, canonical: "4294967295"},
		{name: "full domain", input: "0-4294967295", high: math.MaxUint32, canonical: "0-4294967295"},
		{name: "empty", input: "", wantErr: true},
		{name: "leading whitespace", input: " 5", wantErr: true},
		{name: "trailing whitespace", input: "5 ", wantErr: true},
		{name: "plus sign", input: "+5", wantErr: true},
		{name: "negative", input: "-5", wantErr: true},
		{name: "inverted", input: "9-5", wantErr: true},
		{name: "missing high", input: "5-", wantErr: true},
		{name: "extra separator", input: "5-9-10", wantErr: true},
		{name: "decimal", input: "5.0", wantErr: true},
		{name: "overflow", input: "4294967296", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseU32Range(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseU32Range(%q) succeeded: %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseU32Range(%q): %v", tt.input, err)
			}
			if got.Low() != tt.low || got.High() != tt.high || got.String() != tt.canonical {
				t.Fatalf("ParseU32Range(%q) = [%d,%d] %q, want [%d,%d] %q",
					tt.input, got.Low(), got.High(), got.String(), tt.low, tt.high, tt.canonical)
			}
		})
	}
}

func TestParseU16Range(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		low     uint16
		high    uint16
		wantErr bool
	}{
		{input: "0"},
		{input: "5-9", low: 5, high: 9},
		{input: "65535", low: math.MaxUint16, high: math.MaxUint16},
		{input: "0-65535", high: math.MaxUint16},
		{input: "", wantErr: true},
		{input: "5-4", wantErr: true},
		{input: "65536", wantErr: true},
		{input: "1-65536", wantErr: true},
		{input: " 5", wantErr: true},
		{input: "+5", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseU16Range(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseU16Range(%q) succeeded: %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseU16Range(%q): %v", tt.input, err)
			}
			if got.Low() != tt.low || got.High() != tt.high {
				t.Fatalf("ParseU16Range(%q) = [%d,%d], want [%d,%d]", tt.input, got.Low(), got.High(), tt.low, tt.high)
			}
		})
	}
}

func TestFormatAndConstructRanges(t *testing.T) {
	t.Parallel()

	u32, err := NewU32Range(12, 12)
	if err != nil || u32.String() != "12" || u32.IsZero() {
		t.Fatalf("scalar u32 = %v, %v", u32, err)
	}
	u32, err = NewU32Range(12, 34)
	if err != nil || u32.String() != "12-34" {
		t.Fatalf("ranged u32 = %v, %v", u32, err)
	}
	if _, err := NewU32Range(34, 12); err == nil {
		t.Fatal("inverted u32 range accepted")
	}
	if !ScalarU32(0).IsZero() || ScalarU32(1).IsZero() {
		t.Fatal("u32 zero semantics are incorrect")
	}

	u16, err := NewU16Range(7, 7)
	if err != nil || u16.String() != "7" || u16.IsZero() {
		t.Fatalf("scalar u16 = %v, %v", u16, err)
	}
	u16, err = NewU16Range(7, 11)
	if err != nil || u16.String() != "7-11" {
		t.Fatalf("ranged u16 = %v, %v", u16, err)
	}
	if _, err := NewU16Range(11, 7); err == nil {
		t.Fatal("inverted u16 range accepted")
	}
	if !ScalarU16(0).IsZero() || ScalarU16(1).IsZero() {
		t.Fatal("u16 zero semantics are incorrect")
	}

	// Compile-time behavior: the value types remain comparable for drift checks and map keys.
	seen := map[U32Range]bool{u32: true}
	if !seen[u32] {
		t.Fatal("U32Range is not usable as a comparable value")
	}
}

func TestOverlap(t *testing.T) {
	t.Parallel()

	a, _ := NewU32Range(5, 9)
	b, _ := NewU32Range(9, 12)
	c, _ := NewU32Range(10, 12)
	if !a.Overlaps(b) || !b.Overlaps(a) {
		t.Fatal("inclusive endpoint overlap was not detected")
	}
	if a.Overlaps(c) || c.Overlaps(a) {
		t.Fatal("disjoint ranges overlap")
	}

	x, _ := NewU16Range(100, 200)
	y, _ := NewU16Range(150, 250)
	z, _ := NewU16Range(201, 250)
	if !x.Overlaps(y) || x.Overlaps(z) {
		t.Fatal("u16 overlap semantics are incorrect")
	}
}

func TestJSONU32Range(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		input     string
		canonical string
	}{
		{input: `0`, canonical: "0"},
		{input: `5`, canonical: "5"},
		{input: `"5"`, canonical: "5"},
		{input: `"5-9"`, canonical: "5-9"},
		{input: `4294967295`, canonical: "4294967295"},
	} {
		var got U32Range
		if err := json.Unmarshal([]byte(tt.input), &got); err != nil {
			t.Fatalf("Unmarshal(%s): %v", tt.input, err)
		}
		if got.String() != tt.canonical {
			t.Fatalf("Unmarshal(%s) = %q, want %q", tt.input, got.String(), tt.canonical)
		}
		encoded, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		want := tt.canonical
		if got.Low() != got.High() {
			want = `"` + want + `"`
		}
		if string(encoded) != want {
			t.Fatalf("Marshal(%q) = %s, want %s", got.String(), encoded, want)
		}
	}

	for _, input := range []string{`null`, `-1`, `1.5`, `1e2`, `""`, `" 5"`, `"4294967296"`, `{}`, `[]`, `true`} {
		var got U32Range
		if err := json.Unmarshal([]byte(input), &got); err == nil {
			t.Fatalf("Unmarshal(%s) succeeded: %v", input, got)
		}
	}
}

func TestJSONU16Range(t *testing.T) {
	t.Parallel()

	for _, input := range []string{`0`, `25`, `"25"`, `"25-35"`, `65535`} {
		var got U16Range
		if err := json.Unmarshal([]byte(input), &got); err != nil {
			t.Fatalf("Unmarshal(%s): %v", input, err)
		}
	}
	for _, input := range []string{`null`, `-1`, `1.5`, `1e2`, `65536`, `"1-65536"`, `false`} {
		var got U16Range
		if err := json.Unmarshal([]byte(input), &got); err == nil {
			t.Fatalf("Unmarshal(%s) succeeded: %v", input, got)
		}
	}

	ranged, _ := NewU16Range(25, 35)
	encoded, err := json.Marshal(ranged)
	if err != nil || string(encoded) != `"25-35"` {
		t.Fatalf("Marshal ranged u16 = %s, %v", encoded, err)
	}
	encoded, err = json.Marshal(ScalarU16(25))
	if err != nil || string(encoded) != "25" {
		t.Fatalf("Marshal scalar u16 = %s, %v", encoded, err)
	}
}

func TestScanAndValueU32Range(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     any
		canonical string
	}{
		{name: "legacy integer", input: int64(5), canonical: "5"},
		{name: "text scalar", input: "7", canonical: "7"},
		{name: "text range", input: "7-11", canonical: "7-11"},
		{name: "bytes", input: []byte("12-34"), canonical: "12-34"},
		{name: "empty", input: "", canonical: "0"},
		{name: "nil", input: nil, canonical: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScalarU32(99)
			if err := (&got).Scan(tt.input); err != nil {
				t.Fatalf("Scan: %v", err)
			}
			if got.String() != tt.canonical {
				t.Fatalf("Scan(%v) = %q, want %q", tt.input, got.String(), tt.canonical)
			}
		})
	}

	for _, input := range []any{int64(-1), uint64(math.MaxUint32) + 1, float64(5), "4294967296", struct{}{}} {
		var got U32Range
		if err := (&got).Scan(input); err == nil {
			t.Fatalf("Scan(%T(%v)) succeeded", input, input)
		}
	}

	ranged, _ := NewU32Range(7, 11)
	value, err := ranged.Value()
	if err != nil || value != "7-11" {
		t.Fatalf("Value(range) = %#v, %v", value, err)
	}
	value, err = (U32Range{}).Value()
	if err != nil || value != "" {
		t.Fatalf("Value(zero) = %#v, %v", value, err)
	}
}

func TestScanAndValueU16Range(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		input     any
		canonical string
	}{
		{input: int64(25), canonical: "25"},
		{input: "25-35", canonical: "25-35"},
		{input: []byte("65535"), canonical: "65535"},
		{input: "", canonical: "0"},
		{input: nil, canonical: "0"},
	} {
		var got U16Range
		if err := (&got).Scan(tt.input); err != nil {
			t.Fatalf("Scan(%v): %v", tt.input, err)
		}
		if got.String() != tt.canonical {
			t.Fatalf("Scan(%v) = %q, want %q", tt.input, got.String(), tt.canonical)
		}
	}
	for _, input := range []any{int64(-1), int64(65536), float64(5), "1-65536", []byte("bad")} {
		var got U16Range
		if err := (&got).Scan(input); err == nil {
			t.Fatalf("Scan(%T(%v)) succeeded", input, input)
		}
	}

	ranged, _ := NewU16Range(25, 35)
	value, err := ranged.Value()
	if err != nil || value != "25-35" {
		t.Fatalf("Value(range) = %#v, %v", value, err)
	}
	value, err = (U16Range{}).Value()
	if err != nil || value != "" {
		t.Fatalf("Value(zero) = %#v, %v", value, err)
	}
}
