package domaintls

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"
)

// Loader has one current immutable policy and at most two certificates cached.
// Policy is rechecked per request/handshake, including existing connections.
// Atomic directory-mounted policy replacement retires cached names immediately.
type Loader struct {
	PolicyFile         string
	Roots              *x509.CertPool
	Now                func() time.Time
	BuiltinHosts       []string
	BuiltinCertificate func(*tls.ClientHelloInfo) (*tls.Certificate, error)
	mu                 sync.Mutex
	revision           string
	certs              map[string]*tls.Certificate
}

func ReadPolicy(raw []byte) (Policy, error) {
	if len(raw) == 0 || len(raw) > MaxPolicyBytes {
		return Policy{}, ErrPolicy
	}
	var p Policy
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&p) != nil || decoder.Decode(new(any)) != io.EOF || p.Validate() != nil {
		return Policy{}, ErrPolicy
	}
	return p, nil
}

func readRegular(name string, limit int64) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, ErrPolicy
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, ErrPolicy
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		clear(data)
		return nil, ErrPolicy
	}
	return data, nil
}

func (l *Loader) Snapshot() (Policy, error) {
	data, err := readRegular(l.PolicyFile, MaxPolicyBytes)
	if err != nil {
		return Policy{}, ErrPolicy
	}
	return ReadPolicy(data)
}

func PairFiles(policyFile, id string) (string, string, error) {
	if !filepath.IsAbs(policyFile) && !path.IsAbs(policyFile) || !IDPattern.MatchString(id) {
		return "", "", ErrPolicy
	}
	if path.IsAbs(policyFile) && filepath.VolumeName(policyFile) == "" {
		dir := path.Join(path.Dir(policyFile), "certificates", id)
		return path.Join(dir, "fullchain.pem"), path.Join(dir, "privkey.pem"), nil
	}
	dir := filepath.Join(filepath.Dir(policyFile), "certificates", id)
	return filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem"), nil
}

func (l *Loader) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	p, err := l.Snapshot()
	if err != nil {
		return nil, ErrPolicy
	}
	name := hello.ServerName
	if name == "" {
		panel, _ := ParseOrigin(p.PanelOrigin())
		if net.ParseIP(panel.Host) == nil && (hello.Conn == nil || !peerIsLoopback(hello.Conn.RemoteAddr().String())) {
			return nil, ErrPolicy
		}
		name = panel.Host
	}
	for _, site := range p.Sites {
		o, _ := ParseOrigin(site.Origin)
		if !sameHostname(name, o.Host) || site.Method == External {
			continue
		}
		if site.Method == Builtin {
			approved := false
			for _, host := range l.BuiltinHosts {
				approved = approved || host == o.Host
			}
			if !approved || l.BuiltinCertificate == nil {
				return nil, ErrPolicy
			}
			return l.BuiltinCertificate(hello)
		}
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.revision != p.Revision {
			l.certs = make(map[string]*tls.Certificate, 2)
			l.revision = p.Revision
		}
		if pair := l.certs[site.CertificateID]; pair != nil {
			if !pair.Leaf.NotAfter.After(l.now()) {
				return nil, ErrMaterial
			}
			return pair, nil
		}
		certFile, keyFile, e := PairFiles(l.PolicyFile, site.CertificateID)
		if e != nil {
			return nil, e
		}
		certPEM, e := readRegular(certFile, MaxMaterialBytes)
		if e != nil {
			return nil, ErrMaterial
		}
		defer clear(certPEM)
		keyPEM, e := readRegular(keyFile, MaxMaterialBytes)
		if e != nil {
			return nil, ErrMaterial
		}
		defer clear(keyPEM)
		names := []string{o.Host}
		for _, other := range p.Sites {
			if other.Role != site.Role && other.CertificateID == site.CertificateID {
				otherOrigin, _ := ParseOrigin(other.Origin)
				names = append(names, otherOrigin.Host)
			}
		}
		pair, _, e := checkPair(certPEM, keyPEM, names, l.now(), l.Roots, 0)
		if e != nil {
			return nil, e
		}
		l.certs[site.CertificateID] = &pair
		return &pair, nil
	}
	return nil, ErrPolicy
}

// TLS session resumption can skip GetCertificate. This callback runs for every
// ClientHello, so retired names and expired material cannot bypass admission
// through a cached session ticket. Returning nil preserves the listener config.
func (l *Loader) GetConfigForClient(hello *tls.ClientHelloInfo) (*tls.Config, error) {
	_, err := l.GetCertificate(hello)
	return nil, err
}

func (l *Loader) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}
