package x365

import (
	"bytes"
	"encoding/hex"
	"io"
	"testing"

	M "github.com/metacubex/sing/common/metadata"
)

func TestRequestWireFormat(t *testing.T) {
	client, err := NewClient("376aac10-754a-4b62-9855-6683c9adafce")
	if err != nil {
		t.Fatal(err)
	}
	destination := M.ParseSocksaddrHostPort("example.com", 443)
	var output bytes.Buffer
	if err = WriteRequest(&output, client.key, CommandTCP, destination, []byte("PAYLOAD")); err != nil {
		t.Fatal(err)
	}
	packet := output.Bytes()
	if string(packet[:4]) != "X365" || packet[4] != Version || packet[5] != CommandTCP {
		t.Fatalf("prefix=%x", packet[:6])
	}
	wantUUID, _ := hex.DecodeString("376aac10754a4b6298556683c9adafce")
	if !bytes.Equal(packet[6:22], wantUUID) {
		t.Fatalf("uuid=%x", packet[6:22])
	}
	if packet[22] != 0x01 || packet[23] != 0xbb || packet[24] != 0x02 || packet[25] != 11 || string(packet[26:37]) != "example.com" {
		t.Fatalf("destination=%x", packet[22:])
	}
	if string(packet[len(packet)-7:]) != "PAYLOAD" {
		t.Fatalf("payload=%x", packet)
	}
}

func TestResponseValidation(t *testing.T) {
	if err := ReadResponse(bytes.NewReader([]byte("X365\x01"))); err != nil {
		t.Fatal(err)
	}
	if err := ReadResponse(bytes.NewReader([]byte("VLESS"))); err == nil {
		t.Fatal("invalid response accepted")
	}
	if err := ReadResponse(io.LimitReader(bytes.NewReader([]byte("X36")), 3)); err == nil {
		t.Fatal("short response accepted")
	}
}

func TestUDPFrontHeadroomIncludesLengthPrefix(t *testing.T) {
	destination := M.ParseSocksaddrHostPort("example.com", 53)
	packetConn := &PacketConn{destination: destination}
	if got, want := packetConn.FrontHeadroom(), requestLen(destination)+2; got != want {
		t.Fatalf("FrontHeadroom=%d want=%d", got, want)
	}
}
