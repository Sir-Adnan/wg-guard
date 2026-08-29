package domain

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewIDIsUUIDv7(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewID()
		if seen[id] {
			t.Fatalf("duplicate id %s", id)
		}
		seen[id] = true
		if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
			t.Fatalf("malformed id %q", id)
		}
		if id[14] != '7' {
			t.Fatalf("version nibble not 7: %q", id)
		}
		if v := id[19]; v != '8' && v != '9' && v != 'a' && v != 'b' {
			t.Fatalf("variant nibble not RFC 9562: %q", id)
		}
	}
}

func TestNewIDIsTimeOrdered(t *testing.T) {
	// UUIDv7 orders by millisecond timestamp; random bits within the same
	// millisecond are unordered by design. Assert non-decreasing timestamps.
	tsOf := func(id string) int64 {
		raw, err := hex.DecodeString(strings.ReplaceAll(id[:13], "-", ""))
		if err != nil {
			t.Fatalf("decode %q: %v", id, err)
		}
		return int64(raw[0])<<40 | int64(raw[1])<<32 | int64(raw[2])<<24 |
			int64(raw[3])<<16 | int64(raw[4])<<8 | int64(raw[5])
	}
	prev := NewID()
	time.Sleep(5 * time.Millisecond)
	for i := 0; i < 10; i++ {
		next := NewID()
		if tsOf(next) < tsOf(prev) {
			t.Fatalf("timestamps regressed: %d < %d", tsOf(next), tsOf(prev))
		}
		prev = next
	}
}

func TestNewRandomTokenLength(t *testing.T) {
	tok := NewRandomToken(32)
	if len(tok) != 43 { // 32 bytes -> 43 base64url chars, unpadded
		t.Fatalf("unexpected token length %d", len(tok))
	}
	if strings.ContainsAny(tok, "+/=") {
		t.Fatalf("token contains padded/standard base64 chars: %q", tok)
	}
}

func TestErrorCodes(t *testing.T) {
	err := E(CodeUserNotFound, "user %s", "u1")
	if CodeOf(err) != CodeUserNotFound {
		t.Fatalf("CodeOf = %s", CodeOf(err))
	}
	wrapped := errors.Join(err) // identity through errors.As, not Join
	_ = wrapped
	if CodeOf(errors.New("foreign")) != CodeInternal {
		t.Fatal("foreign errors must map to INTERNAL_ERROR")
	}
	inner := E(CodeUsernameExists, "taken")
	outer := errors.Join(errors.New("ctx"), inner)
	if CodeOf(outer) != CodeUsernameExists {
		t.Fatal("errors.As must find wrapped *Error")
	}
}
