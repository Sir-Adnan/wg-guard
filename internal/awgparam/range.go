// Package awgparam implements the scalar-or-range values used by the pinned
// AmneziaWG configuration contract.
package awgparam

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// U32Range is an inclusive scalar-or-range value with uint32 endpoints.
// Its zero value is the scalar zero. The type is comparable so it can be used
// directly in reconciliation and drift comparisons.
type U32Range struct {
	low  uint32
	high uint32
}

// U16Range is an inclusive scalar-or-range value with uint16 endpoints.
// Its zero value is the scalar zero. The type is comparable so it can be used
// directly in reconciliation and drift comparisons.
type U16Range struct {
	low  uint16
	high uint16
}

// NewU32Range constructs an inclusive uint32 range.
func NewU32Range(low, high uint32) (U32Range, error) {
	if high < low {
		return U32Range{}, errors.New("u32 range low exceeds high")
	}
	return U32Range{low: low, high: high}, nil
}

// NewU16Range constructs an inclusive uint16 range.
func NewU16Range(low, high uint16) (U16Range, error) {
	if high < low {
		return U16Range{}, errors.New("u16 range low exceeds high")
	}
	return U16Range{low: low, high: high}, nil
}

// ScalarU32 constructs a scalar uint32 range.
func ScalarU32(value uint32) U32Range {
	return U32Range{low: value, high: value}
}

// ScalarU16 constructs a scalar uint16 range.
func ScalarU16(value uint16) U16Range {
	return U16Range{low: value, high: value}
}

// ParseU32Range parses the canonical N or N-M syntax without accepting signs,
// whitespace, alternate bases, or endpoints outside uint32.
func ParseU32Range(text string) (U32Range, error) {
	low, high, err := parseBounds(text, 32, "u32")
	if err != nil {
		return U32Range{}, err
	}
	return U32Range{low: uint32(low), high: uint32(high)}, nil
}

// ParseU16Range parses the canonical N or N-M syntax without accepting signs,
// whitespace, alternate bases, or endpoints outside uint16.
func ParseU16Range(text string) (U16Range, error) {
	low, high, err := parseBounds(text, 16, "u16")
	if err != nil {
		return U16Range{}, err
	}
	return U16Range{low: uint16(low), high: uint16(high)}, nil
}

func parseBounds(text string, bitSize int, name string) (uint64, uint64, error) {
	if text == "" || strings.Count(text, "-") > 1 {
		return 0, 0, fmt.Errorf("invalid %s range syntax", name)
	}
	lowText, highText, ranged := strings.Cut(text, "-")
	if !decimalDigits(lowText) || (ranged && !decimalDigits(highText)) {
		return 0, 0, fmt.Errorf("invalid %s range syntax", name)
	}

	low, err := strconv.ParseUint(lowText, 10, bitSize)
	if err != nil {
		return 0, 0, fmt.Errorf("%s range bound exceeds %d bits", name, bitSize)
	}
	high := low
	if ranged {
		high, err = strconv.ParseUint(highText, 10, bitSize)
		if err != nil {
			return 0, 0, fmt.Errorf("%s range bound exceeds %d bits", name, bitSize)
		}
	}
	if high < low {
		return 0, 0, fmt.Errorf("%s range low exceeds high", name)
	}
	return low, high, nil
}

func decimalDigits(text string) bool {
	if text == "" {
		return false
	}
	for i := range len(text) {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}
	return true
}

// Low returns the inclusive lower endpoint.
func (r U32Range) Low() uint32 { return r.low }

// High returns the inclusive upper endpoint.
func (r U32Range) High() uint32 { return r.high }

// IsZero reports whether both endpoints are zero.
func (r U32Range) IsZero() bool { return r.low == 0 && r.high == 0 }

// Overlaps reports whether two closed intervals intersect.
func (r U32Range) Overlaps(other U32Range) bool {
	return r.low <= other.high && other.low <= r.high
}

// String returns canonical N or N-M syntax.
func (r U32Range) String() string {
	if r.low == r.high {
		return strconv.FormatUint(uint64(r.low), 10)
	}
	return strconv.FormatUint(uint64(r.low), 10) + "-" + strconv.FormatUint(uint64(r.high), 10)
}

// Low returns the inclusive lower endpoint.
func (r U16Range) Low() uint16 { return r.low }

// High returns the inclusive upper endpoint.
func (r U16Range) High() uint16 { return r.high }

// IsZero reports whether both endpoints are zero.
func (r U16Range) IsZero() bool { return r.low == 0 && r.high == 0 }

// Overlaps reports whether two closed intervals intersect.
func (r U16Range) Overlaps(other U16Range) bool {
	return r.low <= other.high && other.low <= r.high
}

// String returns canonical N or N-M syntax.
func (r U16Range) String() string {
	if r.low == r.high {
		return strconv.FormatUint(uint64(r.low), 10)
	}
	return strconv.FormatUint(uint64(r.low), 10) + "-" + strconv.FormatUint(uint64(r.high), 10)
}

// MarshalJSON encodes a scalar as a JSON number and a true range as a string.
func (r U32Range) MarshalJSON() ([]byte, error) { return marshalJSON(r.String(), r.low == r.high) }

// UnmarshalJSON accepts a JSON integer or a quoted canonical scalar/range.
func (r *U32Range) UnmarshalJSON(data []byte) error {
	if r == nil {
		return errors.New("cannot unmarshal u32 range into nil receiver")
	}
	text, err := jsonRangeText(data, "u32")
	if err != nil {
		return err
	}
	parsed, err := ParseU32Range(text)
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

// MarshalJSON encodes a scalar as a JSON number and a true range as a string.
func (r U16Range) MarshalJSON() ([]byte, error) { return marshalJSON(r.String(), r.low == r.high) }

// UnmarshalJSON accepts a JSON integer or a quoted canonical scalar/range.
func (r *U16Range) UnmarshalJSON(data []byte) error {
	if r == nil {
		return errors.New("cannot unmarshal u16 range into nil receiver")
	}
	text, err := jsonRangeText(data, "u16")
	if err != nil {
		return err
	}
	parsed, err := ParseU16Range(text)
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

func marshalJSON(text string, scalar bool) ([]byte, error) {
	if scalar {
		return []byte(text), nil
	}
	return json.Marshal(text)
}

func jsonRangeText(data []byte, name string) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("invalid %s range JSON", name)
	}
	if data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return "", fmt.Errorf("invalid %s range JSON", name)
		}
		return text, nil
	}
	if !decimalDigits(string(data)) {
		return "", fmt.Errorf("invalid %s range JSON", name)
	}
	return string(data), nil
}

// Scan implements sql.Scanner. It accepts legacy SQLite integers and the
// canonical text representation. NULL and empty text map to the zero value.
func (r *U32Range) Scan(src any) error {
	if r == nil {
		return errors.New("cannot scan u32 range into nil receiver")
	}
	parsed, err := scanU32(src)
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

func scanU32(src any) (U32Range, error) {
	switch value := src.(type) {
	case nil:
		return U32Range{}, nil
	case int64:
		if value < 0 || uint64(value) > uint64(^uint32(0)) {
			return U32Range{}, errors.New("legacy u32 range integer is out of bounds")
		}
		return ScalarU32(uint32(value)), nil
	case string:
		if value == "" {
			return U32Range{}, nil
		}
		return ParseU32Range(value)
	case []byte:
		if len(value) == 0 {
			return U32Range{}, nil
		}
		return ParseU32Range(string(value))
	default:
		return U32Range{}, fmt.Errorf("cannot scan u32 range from %T", src)
	}
}

// Value implements driver.Valuer using canonical text. The zero value is
// stored as empty text so it matches the canonical unset database default.
func (r U32Range) Value() (driver.Value, error) {
	if r.IsZero() {
		return "", nil
	}
	return r.String(), nil
}

// Scan implements sql.Scanner. It accepts legacy SQLite integers and the
// canonical text representation. NULL and empty text map to the zero value.
func (r *U16Range) Scan(src any) error {
	if r == nil {
		return errors.New("cannot scan u16 range into nil receiver")
	}
	parsed, err := scanU16(src)
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

func scanU16(src any) (U16Range, error) {
	switch value := src.(type) {
	case nil:
		return U16Range{}, nil
	case int64:
		if value < 0 || value > int64(^uint16(0)) {
			return U16Range{}, errors.New("legacy u16 range integer is out of bounds")
		}
		return ScalarU16(uint16(value)), nil
	case string:
		if value == "" {
			return U16Range{}, nil
		}
		return ParseU16Range(value)
	case []byte:
		if len(value) == 0 {
			return U16Range{}, nil
		}
		return ParseU16Range(string(value))
	default:
		return U16Range{}, fmt.Errorf("cannot scan u16 range from %T", src)
	}
}

// Value implements driver.Valuer using canonical text. The zero value is
// stored as empty text so it matches the canonical unset database default.
func (r U16Range) Value() (driver.Value, error) {
	if r.IsZero() {
		return "", nil
	}
	return r.String(), nil
}
