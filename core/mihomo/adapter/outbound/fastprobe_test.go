package outbound

import (
	"context"
	"testing"
)

// TestX365FastProbeInterface verifies that *X365 satisfies the anonymous
// FastProbe interface used by (*Proxy).URLTest to short-circuit heavy probes.
func TestX365FastProbeInterface(t *testing.T) {
	var x any = any(&X365{})
	_, ok := x.(interface {
		FastProbe(ctx context.Context) error
	})
	if !ok {
		t.Fatal("*X365 does NOT satisfy FastProbe interface")
	}
	_, ok = any(&XHTTP{}).(interface {
		FastProbe(ctx context.Context) error
	})
	if !ok {
		t.Fatal("*XHTTP does NOT satisfy FastProbe interface")
	}
	t.Log("both satisfy FastProbe")
}
