package domaintls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testMaterial(t *testing.T, names []string) ([]byte, []byte, *x509.CertPool) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Synthetic acceptance root"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(48 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	rootDER, e := x509.CreateCertificate(rand.Reader, root, root, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	rootCert, _ := x509.ParseCertificate(rootDER)
	roots := x509.NewCertPool()
	roots.AddCert(rootCert)
	leafKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: names, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, e := x509.CreateCertificate(rand.Reader, leaf, rootCert, &leafKey.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	private, e := x509.MarshalPKCS8PrivateKey(leafKey)
	if e != nil {
		t.Fatal(e)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), roots
}

func TestCertificateChainNamesKeyAndValidity(t *testing.T) {
	cert, key, roots := testMaterial(t, []string{"panel.example.test", "sub.example.test"})
	if _, _, e := CheckPair(cert, key, []string{"panel.example.test", "sub.example.test"}, time.Now(), roots); e != nil {
		t.Fatal("valid SAN pair rejected", e)
	}
	_, other, _ := testMaterial(t, []string{"panel.example.test"})
	for _, kind := range []string{"wrong-key", "wrong-name", "untrusted", "expired", "oversized"} {
		c, k, n, at, r := cert, key, []string{"panel.example.test"}, time.Now(), roots
		switch kind {
		case "wrong-key":
			k = other
		case "wrong-name":
			n = []string{"other.example.test"}
		case "untrusted":
			r = x509.NewCertPool()
		case "expired":
			at = at.Add(48 * time.Hour)
		case "oversized":
			c = []byte(strings.Repeat("x", MaxMaterialBytes+1))
		}
		if _, _, e := CheckPair(c, k, n, at, r); e == nil {
			t.Fatal("unsafe material admitted", kind)
		}
	}
}

func TestSNISelectsApprovedPairAndRetirementInvalidatesCache(t *testing.T) {
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "active.json")
	p := testPolicy()
	cert, key, roots := testMaterial(t, []string{"panel.example.test", "sub.example.test"})
	for _, site := range p.Sites {
		c, k, _ := PairFiles(policyPath, site.CertificateID)
		if e := os.MkdirAll(filepath.Dir(c), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(c, cert, 0600); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(k, key, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write := func() {
		raw, _ := json.Marshal(p)
		if e := os.WriteFile(policyPath, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write()
	l := &Loader{PolicyFile: policyPath, Roots: roots}
	for _, name := range []string{"panel.example.test", "sub.example.test"} {
		if _, e := l.GetCertificate(&tls.ClientHelloInfo{ServerName: name}); e != nil {
			t.Fatal("approved SNI rejected", e)
		}
	}
	if _, e := l.GetCertificate(&tls.ClientHelloInfo{ServerName: "unapproved.example.test"}); e == nil {
		t.Fatal("unknown SNI returned certificate")
	}
	p.Sites = p.Sites[:1]
	p.Revision = strings.Repeat("d", 32)
	write()
	if _, e := l.GetCertificate(&tls.ClientHelloInfo{ServerName: "sub.example.test"}); e == nil {
		t.Fatal("retired SNI used cached certificate")
	}
	if e := os.WriteFile(policyPath, []byte("invalid"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := l.GetCertificate(&tls.ClientHelloInfo{ServerName: "panel.example.test"}); e == nil {
		t.Fatal("invalid policy reused old cache")
	}
}
