package outbound

import "testing"

const testShanlianOutboundPassword = "00112233445566778899aabbccddeeff102132435465768798a9bacbdcedfe0f#SL"

func TestShanlianAnyTLSDefaultsChromeFingerprint(t *testing.T) {
	if got := effectiveAnyTLSClientFingerprint(testShanlianOutboundPassword, ""); got != "chrome" {
		t.Fatalf("Shanlian client fingerprint = %q, want chrome", got)
	}
}

func TestOrdinaryAnyTLSDoesNotReceiveShanlianFingerprintDefault(t *testing.T) {
	ordinary := "00112233445566778899aabbccddeeff"
	if got := effectiveAnyTLSClientFingerprint(ordinary, ""); got != "" {
		t.Fatalf("ordinary AnyTLS client fingerprint = %q, want empty", got)
	}
}

func TestExplicitAnyTLSFingerprintTakesPrecedence(t *testing.T) {
	if got := effectiveAnyTLSClientFingerprint(testShanlianOutboundPassword, "firefox"); got != "firefox" {
		t.Fatalf("explicit client fingerprint = %q, want firefox", got)
	}
}
