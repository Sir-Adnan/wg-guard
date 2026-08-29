package tunnel

import (
	"encoding/base64"
	"testing"
)

func TestKeyGeneration(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePrivateKey(kp.Private); err != nil {
		t.Fatalf("private key invalid: %v", err)
	}
	if err := ValidatePublicKey(kp.Public); err != nil {
		t.Fatalf("public key invalid: %v", err)
	}
	derived, err := PublicKeyFromPrivate(kp.Private)
	if err != nil {
		t.Fatal(err)
	}
	if derived != kp.Public {
		t.Fatalf("derived public key %s != %s", derived, kp.Public)
	}
	kp2, _ := GenerateKeyPair()
	if kp.Private == kp2.Private {
		t.Fatal("keys not random")
	}
	psk, err := GeneratePresharedKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePrivateKey(psk); err != nil {
		t.Fatalf("psk shape invalid: %v", err)
	}
}

func TestKeyValidation(t *testing.T) {
	if err := ValidatePublicKey("not-base64!!"); err == nil {
		t.Fatal("non-base64 accepted")
	}
	if err := ValidatePublicKey("AAAA"); err == nil {
		t.Fatal("short key accepted")
	}
	if err := ValidatePrivateKey(base64.StdEncoding.EncodeToString(make([]byte, 31))); err == nil {
		t.Fatal("wrong-length key accepted")
	}
}
