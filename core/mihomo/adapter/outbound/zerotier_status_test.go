//go:build !no_zerotier

package outbound

import "testing"

func TestZeroTierStatusIsUninitializedWithoutRuntime(t *testing.T) {
	status := (&ZeroTier{networkID: 0x8056c2e21c000001}).status(false)

	if status.Status != "" {
		t.Fatalf("uninitialized ZeroTier status mapped to %q", status.Status)
	}
	if status.NetworkID != "8056c2e21c000001" {
		t.Fatalf("unexpected network ID %q", status.NetworkID)
	}
}

func TestZeroTierPeerVersion(t *testing.T) {
	if version := zeroTierPeerVersion(1, 14, 2); version != "1.14.2" {
		t.Fatalf("unexpected ZeroTier peer version %q", version)
	}
	if version := zeroTierPeerVersion(-1, -1, -1); version != "" {
		t.Fatalf("unknown ZeroTier peer version mapped to %q", version)
	}
}
