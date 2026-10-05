package install

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAcquisitionLogKeepsFailureWithoutSecretsOrUnboundedOutput(t *testing.T) {
	var buffer bytes.Buffer
	cause := errors.New(strings.Repeat("go: module progress ", 500) + "password=fixture-secret https://operator:fixture-password@proxy.example disk quota exceeded")
	if err := writeAcquisitionFailure(&buffer, cause, time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	text := buffer.String()
	if len(text) > 2200 || !strings.Contains(text, "disk quota exceeded") || strings.Contains(text, "fixture-secret") || strings.Contains(text, "fixture-password") {
		t.Fatal("acquisition log failed bounded diagnostic/redaction")
	}
	if !strings.Contains(text, "acquisition failed") {
		t.Fatal("acquisition source missing")
	}
}
