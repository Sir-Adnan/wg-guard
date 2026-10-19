package sitebridge

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

// errSniffed ends the TLS handshake once the client hello has been read.
var errSniffed = errors.New("sitebridge: sniffed")

// sniffTLS reads the client hello and returns its server name and every byte
// read, which are replayed to the site unchanged. The standard library's
// handshake parser reads the hello (records split across segments, any
// extension order); the handshake is stopped before the server would answer.
func sniffTLS(conn net.Conn) (string, []byte) {
	var head bytes.Buffer
	r := io.TeeReader(io.LimitReader(conn, maxSniff), &head)
	var name string
	srv := tls.Server(readOnly{r: r, Conn: conn}, &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			name = hello.ServerName
			return nil, errSniffed
		},
	})
	_ = srv.Handshake()
	return name, head.Bytes()
}

// sniffHTTP reads the request head and returns its Host and the bytes read.
func sniffHTTP(conn net.Conn) (string, []byte) {
	var head bytes.Buffer
	br := bufio.NewReaderSize(io.TeeReader(io.LimitReader(conn, maxSniff), &head), 4096)
	req, err := http.ReadRequest(br)
	if err != nil {
		return "", head.Bytes()
	}
	host := req.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host, head.Bytes()
}

// readOnly lets the TLS parser read the client while refusing every write:
// the bridge never answers the handshake itself.
type readOnly struct {
	r io.Reader
	net.Conn
}

func (c readOnly) Read(p []byte) (int, error)       { return c.r.Read(p) }
func (c readOnly) Write([]byte) (int, error)        { return 0, io.ErrClosedPipe }
func (c readOnly) SetDeadline(time.Time) error      { return nil }
func (c readOnly) SetWriteDeadline(time.Time) error { return nil }
func (c readOnly) Close() error                     { return nil }
