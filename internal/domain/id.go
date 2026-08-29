// Package domain holds types shared across services: identifiers, lifecycle
// enums, and machine error codes (the codes exposed by the REST API error
// envelope are a V1 compatibility contract — see docs/architecture/api.md).
package domain

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"time"
)

// NewID returns a UUIDv7 string: 48-bit millisecond timestamp + random bits.
// Time-ordered IDs keep SQLite indexes (and cursor pagination) local. Format
// is standard RFC 9562; no external UUID dependency.
func NewID() string {
	var b [16]byte
	ms := time.Now().UnixMilli()
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	if _, err := rand.Read(b[6:]); err != nil {
		// crypto/rand failure is unrecoverable; panic rather than emit
		// colliding or predictable identifiers.
		panic("domain: crypto/rand unavailable: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x70 // version 7
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 9562 variant
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst)
}

// NewRandomToken returns nBytes of crypto/rand, base64url-encoded without
// padding. Used for session and API token material (never logged).
func NewRandomToken(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		panic("domain: crypto/rand unavailable: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
