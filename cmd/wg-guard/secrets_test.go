package main

import (
	"strings"
	"testing"
)

func TestSecretsRotateRejectsMissingConfigValueBeforeOpen(t *testing.T) {
	for _, args := range [][]string{
		{"rotate", "--config"},
		{"rotate", "-config"},
		{"rotate", "--config", "--yes"},
		{"rotate", "--config", ""},
	} {
		err := runSecrets(args)
		if err == nil || !(strings.Contains(err.Error(), "requires a path") || strings.Contains(err.Error(), "flag needs an argument")) {
			t.Fatalf("runSecrets(%q) = %v, want missing path error", args, err)
		}
	}
}
