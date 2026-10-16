package clientconf

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"fmt"
)

// AmneziaVPNKey encodes a client config as an AmneziaVPN "vpn://" key.
//
// Format (source-reviewed against amnezia-vpn/amnezia-client 30ff345,
// client/core/controllers/selfhosted/importController.cpp, 2026-10-16): the
// app strips "vpn://", base64url-decodes without padding, applies Qt's
// qUncompress (a 4-byte big-endian uncompressed length followed by a zlib
// stream) and then detects the text format. A payload containing [Interface]
// and [Peer] is imported through the app's native WireGuard/AmneziaWG parser,
// so the config is carried verbatim instead of re-encoding it into the app's
// internal JSON schema, which changes between releases. Android registers the
// vpn:// scheme, so the link opens the app directly.
func AmneziaVPNKey(conf string) (string, error) {
	if len(conf) == 0 || len(conf) > 1<<20 {
		return "", fmt.Errorf("clientconf: config size out of range")
	}
	var buf bytes.Buffer
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(conf)))
	buf.Write(size[:])
	zw, err := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	if err != nil {
		return "", err
	}
	if _, err := zw.Write([]byte(conf)); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	return "vpn://" + base64.RawURLEncoding.EncodeToString(buf.Bytes()), nil
}
