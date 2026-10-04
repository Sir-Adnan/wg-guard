package settings

import (
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/secrets"
)

func TestSecretDefinitionsMatchNodeStorageInventory(t *testing.T) {
	expected := map[string]bool{}
	for _, key := range secrets.SecretSettingKeys() {
		expected[key] = true
	}
	for _, definition := range Defaults() {
		if !definition.Secret {
			continue
		}
		if !expected[definition.Key] {
			t.Fatal("secret catalog definition missing from node storage inventory", definition.Key)
		}
		delete(expected, definition.Key)
	}
	if len(expected) != 0 {
		t.Fatal("node storage inventory includes an unknown secret setting")
	}
}
