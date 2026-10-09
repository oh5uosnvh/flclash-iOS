package session

import (
	"encoding/binary"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/metacubex/mihomo/transport/anytls/padding"
)

// shanlianClientName is the identity Shanlian/LightXtreme servers require; it
// arrives as the private override of the profile-supplied client metadata.
const shanlianClientName = "sing-anytls/0.0.11"

func newTestSession(t *testing.T, clientMetadata, clientName string) string {
	t.Helper()
	conn, peer := net.Pipe()
	t.Cleanup(func() { peer.Close() })

	var paddingFactory atomic.Pointer[padding.PaddingFactory]
	paddingFactory.Store(padding.NewPaddingFactory(padding.DefaultPaddingScheme))

	s := NewClientSession(conn, &paddingFactory, clientMetadata, clientName)
	s.Run()
	t.Cleanup(func() { s.Close() })

	if len(s.buffer) < headerOverHeadSize {
		t.Fatalf("settings buffer length = %d, want at least %d", len(s.buffer), headerOverHeadSize)
	}
	if s.buffer[0] != cmdSettings {
		t.Fatalf("first command = %d, want settings command %d", s.buffer[0], cmdSettings)
	}
	length := int(binary.BigEndian.Uint16(s.buffer[5:7]))
	if len(s.buffer) < headerOverHeadSize+length {
		t.Fatalf("settings buffer length = %d, payload length = %d", len(s.buffer), length)
	}
	return string(s.buffer[headerOverHeadSize : headerOverHeadSize+length])
}

// Upstream stopped advertising any client identity by default (v1.19.30), so an
// ordinary node must keep sending an empty `client` field.
func TestClientSessionKeepsEmptyIdentityForStockNodes(t *testing.T) {
	settings := newTestSession(t, "", "")
	if !strings.Contains(settings, "client=") {
		t.Fatalf("settings = %q, missing client key", settings)
	}
	if strings.Contains(settings, "sing-anytls") {
		t.Fatalf("settings = %q, plain AnyTLS must not advertise the shanlian identity", settings)
	}
}

func TestClientSessionUsesConfiguredClientMetadata(t *testing.T) {
	settings := newTestSession(t, "my-client/1.0", "")
	if !strings.Contains(settings, "client=my-client/1.0") {
		t.Fatalf("settings = %q, missing configured client metadata", settings)
	}
}

func TestClientSessionShanlianOverrideWins(t *testing.T) {
	settings := newTestSession(t, "my-client/1.0", shanlianClientName)
	if !strings.Contains(settings, "client="+shanlianClientName) {
		t.Fatalf("settings = %q, missing shanlian client name", settings)
	}
	if strings.Contains(settings, "my-client/1.0") {
		t.Fatalf("settings = %q, shanlian override did not replace client metadata", settings)
	}
}