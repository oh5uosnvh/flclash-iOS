package shadowaead

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	C "github.com/metacubex/sing-shadowsocks2/cipher"
	M "github.com/metacubex/sing/common/metadata"
)

type memoryConn struct {
	bytes.Buffer
	writes int
}

func (c *memoryConn) Write(p []byte) (int, error)      { c.writes++; return c.Buffer.Write(p) }
func (c *memoryConn) Close() error                     { return nil }
func (c *memoryConn) LocalAddr() net.Addr              { return nil }
func (c *memoryConn) RemoteAddr() net.Addr             { return nil }
func (c *memoryConn) SetDeadline(time.Time) error      { return nil }
func (c *memoryConn) SetReadDeadline(time.Time) error  { return nil }
func (c *memoryConn) SetWriteDeadline(time.Time) error { return nil }

func TestViewTurboAuthPacket(t *testing.T) {
	packet := vtAuthPacket()
	if len(packet) != 60 {
		t.Fatalf("auth length=%d", len(packet))
	}
	packet[1] = ^packet[1]
	if string(packet[:len(vtToken)]) != vtToken || packet[len(vtToken)] != ':' {
		t.Fatalf("auth token mismatch: %q", packet[:len(vtToken)+1])
	}
}

func TestViewTurboPrefix(t *testing.T) {
	prefix := vtPrefix()
	if len(prefix) < 24 || len(prefix) > 64 {
		t.Fatalf("prefix length=%d", len(prefix))
	}
	if !bytes.HasSuffix(prefix, []byte("\r\n\r\n")) {
		t.Fatal("prefix missing terminator")
	}
	for _, value := range prefix[:len(prefix)-4] {
		if !((value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z')) {
			t.Fatalf("non-alpha prefix byte=%x", value)
		}
	}
}

func TestViewTurboDialConnKeepsSaltLazy(t *testing.T) {
	method, err := NewMethod("chacha20-ietf-poly1305", C.MethodOptions{Password: "password" + vtMagic})
	if err != nil {
		t.Fatal(err)
	}
	base := &memoryConn{}
	conn, err := method.DialConn(base, M.ParseSocksaddrHostPort("example.com", 443))
	if err != nil {
		t.Fatal(err)
	}
	if base.writes != 2 {
		t.Fatalf("handshake writes=%d, want prefix+auth only", base.writes)
	}
	early, ok := conn.(interface{ NeedHandshake() bool })
	if !ok || !early.NeedHandshake() {
		t.Fatal("salt was eagerly flushed")
	}
}

func TestViewTurboSkipHTTPHeader(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	go func() {
		_, _ = right.Write([]byte("HTTP/1.1 200 OK\r\nServer: Tengine\r\n\r\nSALT"))
	}()
	if err := vtSkipHTTPHeader(left); err != nil {
		t.Fatal(err)
	}
	leftover := make([]byte, 4)
	if _, err := io.ReadFull(left, leftover); err != nil {
		t.Fatal(err)
	}
	if string(leftover) != "SALT" {
		t.Fatalf("leftover=%q", leftover)
	}
}
