package device

import (
	"fmt"

	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// GenerateKeys is the shared provisioning boundary for device credentials.
// Only sealed material and its public identity leave it; it performs no storage,
// rendering or host operation. API/web parsing and principal checks stay outside.
func GenerateKeys(ring *secrets.KeyRing, withPSK bool) (*KeyMaterial, error) {
	if ring == nil {
		return nil, fmt.Errorf("device key ring is unavailable")
	}
	pair, err := tunnel.GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	private := []byte(pair.Private)
	sealed, err := ring.Encrypt(private)
	clear(private)
	if err != nil {
		return nil, fmt.Errorf("encrypt private key: %w", err)
	}
	keys := &KeyMaterial{PublicKey: pair.Public, PrivateKeyEnc: sealed}
	if withPSK {
		psk, err := tunnel.GeneratePresharedKey()
		if err != nil {
			return nil, err
		}
		plain := []byte(psk)
		sealed, err := ring.Encrypt(plain)
		clear(plain)
		if err != nil {
			return nil, fmt.Errorf("encrypt preshared key: %w", err)
		}
		keys.PresharedEnc = sealed
	}
	return keys, nil
}
