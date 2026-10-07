package anytls

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

const testShanlianPassword = "00112233445566778899aabbccddeeff102132435465768798a9bacbdcedfe0f#SL"

func TestDecodeShanlianPassword(t *testing.T) {
	want, err := hex.DecodeString(strings.TrimSuffix(testShanlianPassword, shanlianPasswordMarker))
	if err != nil {
		t.Fatal(err)
	}

	got, ok := decodeShanlianPassword(testShanlianPassword)
	if !ok {
		t.Fatal("valid marked password was rejected")
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("decoded password = %x, want %x", got, want)
	}

	invalid := []string{
		strings.TrimSuffix(testShanlianPassword, shanlianPasswordMarker),
		strings.Repeat("0", 64),
		strings.Repeat("z", 64) + shanlianPasswordMarker,
		strings.Repeat("0", 63) + shanlianPasswordMarker,
		testShanlianPassword + " ",
	}
	for _, password := range invalid {
		if decoded, ok := decodeShanlianPassword(password); ok || decoded != nil {
			t.Errorf("invalid password accepted: %q", password)
		}
	}
}

func TestNewClientPasswordRouting(t *testing.T) {
	decoded, _ := hex.DecodeString(strings.TrimSuffix(testShanlianPassword, shanlianPasswordMarker))

	shanlian := NewClient(context.Background(), ClientConfig{
		Password:     testShanlianPassword,
		DisableReuse: true,
	})
	defer shanlian.Close()
	if !bytes.Equal(shanlian.passwordSha256, decoded) {
		t.Fatalf("Shanlian password field = %x, want %x", shanlian.passwordSha256, decoded)
	}

	ordinaryPassword := strings.TrimSuffix(testShanlianPassword, shanlianPasswordMarker)
	ordinary := NewClient(context.Background(), ClientConfig{
		Password:     ordinaryPassword,
		DisableReuse: true,
	})
	defer ordinary.Close()
	wantHash := sha256.Sum256([]byte(ordinaryPassword))
	if !bytes.Equal(ordinary.passwordSha256, wantHash[:]) {
		t.Fatalf("ordinary password field = %x, want SHA-256 %x", ordinary.passwordSha256, wantHash[:])
	}
}

func TestNewClientShanlianMetadata(t *testing.T) {
	// Shanlian credentials must flow the compatibility client name into the
	// session layer via ClientConfig.ClientMetadata compatibility shim.
	if !IsShanlianPassword(testShanlianPassword) {
		t.Fatal("marker not detected")
	}
	if IsShanlianPassword(strings.TrimSuffix(testShanlianPassword, shanlianPasswordMarker)) {
		t.Fatal("unmarked password detected as Shanlian")
	}
}
