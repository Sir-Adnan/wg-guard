package domaintls

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"time"
)

var ErrMaterial = errors.New("certificate material is invalid, untrusted or unusable")

type CertificateInfo struct {
	Fingerprint string    `json:"fingerprint"`
	NotBefore   time.Time `json:"not_before"`
	NotAfter    time.Time `json:"not_after"`
	Issuer      string    `json:"issuer"`
	Automatic   bool      `json:"automatic"`
}

// CheckPair validates coherent material before activation. Trust roots may only
// be overridden by an isolated acceptance fixture, never by a web request.
func CheckPair(certPEM, keyPEM []byte, names []string, now time.Time, roots *x509.CertPool) (tls.Certificate, CertificateInfo, error) {
	return checkPair(certPEM, keyPEM, names, now, roots, time.Hour)
}

func checkPair(certPEM, keyPEM []byte, names []string, now time.Time, roots *x509.CertPool, minimumRemaining time.Duration) (tls.Certificate, CertificateInfo, error) {
	if len(certPEM) == 0 || len(keyPEM) == 0 || len(certPEM) > MaxMaterialBytes || len(keyPEM) > MaxMaterialBytes || len(names) < 1 || len(names) > 2 {
		return tls.Certificate{}, CertificateInfo{}, ErrMaterial
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil || len(pair.Certificate) == 0 || len(pair.Certificate) > 10 {
		return tls.Certificate{}, CertificateInfo{}, ErrMaterial
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return tls.Certificate{}, CertificateInfo{}, ErrMaterial
	}
	if now.Before(leaf.NotBefore) || !leaf.NotAfter.After(now.Add(minimumRemaining)) {
		return tls.Certificate{}, CertificateInfo{}, ErrMaterial
	}
	intermediates := x509.NewCertPool()
	for _, raw := range pair.Certificate[1:] {
		c, e := x509.ParseCertificate(raw)
		if e != nil {
			return tls.Certificate{}, CertificateInfo{}, ErrMaterial
		}
		intermediates.AddCert(c)
	}
	for _, name := range names {
		host, e := Hostname(name)
		if e != nil {
			return tls.Certificate{}, CertificateInfo{}, ErrMaterial
		}
		if _, e = leaf.Verify(x509.VerifyOptions{DNSName: host, CurrentTime: now, Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); e != nil {
			return tls.Certificate{}, CertificateInfo{}, ErrMaterial
		}
	}
	pair.Leaf = leaf
	hash := sha256.Sum256(pair.Certificate[0])
	info := CertificateInfo{Fingerprint: hex.EncodeToString(hash[:]), NotBefore: leaf.NotBefore, NotAfter: leaf.NotAfter, Issuer: leaf.Issuer.CommonName}
	return pair, info, nil
}
