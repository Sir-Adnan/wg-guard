package domain

import "encoding/json"

// OptInt64 and OptInt are the tri-state shape of every clearable limit field
// in the API contract: absent (leave the stored value / use the default),
// JSON null (clear — the limit becomes "unlimited"), or a value. This is what
// lets PATCH /users/{id} express "make unlimited" (null) without confusing it
// with "no change" (field absent) — the two are different operations.
//
// Services decode into these types; the zero value means "not present".
type OptInt64 struct {
	Set   bool // the field was present in the payload
	Null  bool // present AND null → clear the stored value
	Value int64
}

func (o *OptInt64) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		o.Set, o.Null = true, true
		return nil
	}
	var v int64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Set, o.Value = true, v
	return nil
}

// Resolve returns the stored value after applying the option: a set value
// replaces, null clears (nil), absent keeps current.
func (o OptInt64) Resolve(current *int64) *int64 {
	switch {
	case !o.Set:
		return current
	case o.Null:
		return nil
	default:
		v := o.Value
		return &v
	}
}

// OptInt is the int-sized twin of OptInt64.
type OptInt struct {
	Set   bool
	Null  bool
	Value int
}

func (o *OptInt) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		o.Set, o.Null = true, true
		return nil
	}
	var v int
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Set, o.Value = true, v
	return nil
}

func (o OptInt) Resolve(current *int) *int {
	switch {
	case !o.Set:
		return current
	case o.Null:
		return nil
	default:
		v := o.Value
		return &v
	}
}

// OptString adds the same tri-state to string fields that need an explicit
// clear (JSON null → "") beyond the empty string.
type OptString struct {
	Set   bool
	Null  bool
	Value string
}

func (o *OptString) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		o.Set, o.Null = true, true
		return nil
	}
	var v string
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Set, o.Value = true, v
	return nil
}
