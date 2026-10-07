package oppa

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"net"
	"testing"

	M "github.com/metacubex/sing/common/metadata"
)

func TestStreamHeader(t *testing.T) {
	header, err := BuildStreamHeader("secret", M.ParseSocksaddr("example.com:443"))
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte("secret"), 0x01, 0x03, 11)
	want = append(want, []byte("example.com")...)
	want = binary.BigEndian.AppendUint16(want, 443)
	if !bytes.Equal(header, want) {
		t.Fatalf("header = %x, want %x", header, want)
	}

	reader := bytes.NewReader(header[len("secret")+1:]) // skip password + command byte
	addr, err := ParseAddress(reader)
	if err != nil {
		t.Fatal(err)
	}
	if addr.Fqdn != "example.com" || addr.Port != 443 {
		t.Fatalf("parsed = %v", addr)
	}
}

func TestAddressIPv4IPv6RoundTrip(t *testing.T) {
	for _, target := range []string{"192.0.2.1:80", "[2001:db8::1]:8080", "a.example:53"} {
		addr := M.ParseSocksaddr(target)
		buf, err := AppendAddress(nil, addr)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ParseAddress(bytes.NewReader(buf))
		if err != nil {
			t.Fatal(err)
		}
		if parsed.String() != addr.String() {
			t.Fatalf("round trip %s -> %s", addr, parsed)
		}
	}
}

func TestSessionHeaderValidation(t *testing.T) {
	if _, err := BuildSessionHeader("", CommandTCP); err == nil {
		t.Fatal("empty password accepted")
	}
	long := make([]byte, MaxPasswordLength+1)
	if _, err := BuildSessionHeader(string(long), CommandTCP); err == nil {
		t.Fatal("oversized password accepted")
	}
	header, err := BuildSessionHeader("pw", CommandUDP)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(header, []byte{'p', 'w', 0x02}) {
		t.Fatalf("udp header = %x", header)
	}
}

func TestUDPFrameRoundTrip(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	payload := []byte("hello udp")
	destination := &net.UDPAddr{IP: net.IPv4(8, 8, 4, 4), Port: 53}

	go func() {
		c := NewPacketConn(client)
		if _, err := c.WriteTo(payload, destination); err != nil {
			t.Errorf("write: %v", err)
		}
	}()

	s := NewPacketConn(server)
	buf := make([]byte, 1500)
	n, from, err := s.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != string(payload) {
		t.Fatalf("payload = %q", buf[:n])
	}
	if from.String() != destination.String() {
		t.Fatalf("from = %v", from)
	}
}

func TestDecodePinHash(t *testing.T) {
	// sha256("test") in three encodings must all decode to the same bytes.
	const hexPin = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	raw := fromHex(hexPin)
	b64Raw := base64.RawURLEncoding.EncodeToString(raw)
	colonated := "9f:86:d0:81:88:4c:7d:65:9a:2f:ea:a0:c5:5a:d0:15:a3:bf:4f:1b:2b:0b:82:2c:d1:5d:6c:15:b0:f0:0a:08"
	for _, pin := range []string{hexPin, b64Raw, colonated} {
		decoded, err := DecodePinHash(pin)
		if err != nil {
			t.Fatalf("decode %q: %v", pin, err)
		}
		if !bytes.Equal(decoded, raw) {
			t.Fatalf("pin %q decoded to different bytes", pin)
		}
	}
	if _, err := DecodePinHash("!!not-a-pin!!"); err == nil {
		t.Fatal("invalid pin accepted")
	}
}

func TestCertChainHash(t *testing.T) {
	single := CertChainHash([][]byte{[]byte("cert0")})
	if len(single) != 32 {
		t.Fatalf("len = %d", len(single))
	}
	if !bytes.Equal(single, CertChainHash([][]byte{[]byte("cert0")})) {
		t.Fatal("not deterministic")
	}
	chain := CertChainHash([][]byte{[]byte("a"), []byte("b")})
	if bytes.Equal(chain, single) {
		t.Fatal("chain ignores extra certs")
	}
}

func fromHex(s string) []byte {
	out, _ := hex.DecodeString(s)
	return out
}
