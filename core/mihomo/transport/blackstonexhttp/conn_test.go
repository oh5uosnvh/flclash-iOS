package blackstonexhttp

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"io"
	"net"
	"strings"
	"testing"

	M "github.com/metacubex/sing/common/metadata"
)

const testSessionHex = "b15a77a56828e84af09ffb35d88f4acd24b18ff0d4780ef645ba2507c8362174e11249f36932af25954b49e368c9658520db9f4f5b2e14d704de46f11e14efd371981dad"
const testTokenHex = "60d3d78f78b8c9c36bd2e42ce8bf1561d1f35f66bda8ccfd38383173c8951cba6e08ee3dfbd70ca919518e5e008a2e8fea"

func TestKeyDerivationAndPasswordToken(t *testing.T) {
	key2 := "31" + testTokenHex // 0x31 == 49-byte token
	client, err := NewClient(DefaultKey1+":"+key2, testSessionHex, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(client.key[:]); got != "495bcfb90cbb1d7025d882a0be2fc305" {
		t.Fatalf("key=%s", got)
	}
	if got := hex.EncodeToString(client.magic); got != "8a7beebb5a40a02f5d54a8a15ce28274" {
		t.Fatalf("magic=%s", got)
	}
	if got := hex.EncodeToString(client.token); got != testTokenHex {
		t.Fatalf("token=%s", got)
	}
}

func TestRejectInvalidAuthenticationMaterial(t *testing.T) {
	cases := []struct{ password, session, auth string }{
		{DefaultKey1, "00", testTokenHex},
		{DefaultKey1, testSessionHex, "zz"},
		{DefaultKey1 + ":30" + testTokenHex, testSessionHex, ""},
		{DefaultKey1, testSessionHex, ""},
	}
	for i, testCase := range cases {
		if _, err := NewClient(testCase.password, testCase.session, testCase.auth); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}

func ctrForTest(t *testing.T, key, iv []byte) cipher.Stream {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	return cipher.NewCTR(block, iv)
}

func TestStreamRoundTripAndPrefixLayout(t *testing.T) {
	client, err := NewClient(DefaultKey1, testSessionHex, testTokenHex)
	if err != nil {
		t.Fatal(err)
	}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	destination := M.ParseSocksaddrHostPort("example.com", 443)
	wrapped := client.StreamConn(left, destination)
	requestPayload := []byte("GET / HTTP/1.1\r\n\r\n")
	responsePayload := []byte("HTTP/1.1 204 No Content\r\n\r\n")

	serverDone := make(chan error, 1)
	go func() {
		addressLength := M.SocksaddrSerializer.AddrPortLen(destination)
		headerLength := MagicOffset + len(client.magic) + 1 + len(client.token) + 1 + IVSize
		packet := make([]byte, headerLength+addressLength+len(requestPayload))
		if _, readErr := io.ReadFull(right, packet); readErr != nil {
			serverDone <- readErr
			return
		}
		if !bytes.Equal(packet[:SessionSize], client.session[:]) {
			serverDone <- io.ErrUnexpectedEOF
			return
		}
		if !bytes.Equal(packet[MagicOffset:MagicOffset+len(client.magic)], client.magic) {
			serverDone <- io.ErrUnexpectedEOF
			return
		}
		cursor := MagicOffset + len(client.magic)
		if int(packet[cursor]) != len(client.token) {
			serverDone <- io.ErrUnexpectedEOF
			return
		}
		cursor++
		if !bytes.Equal(packet[cursor:cursor+len(client.token)], client.token) {
			serverDone <- io.ErrUnexpectedEOF
			return
		}
		cursor += len(client.token)
		if packet[cursor] != IVSize {
			serverDone <- io.ErrUnexpectedEOF
			return
		}
		cursor++
		iv := packet[cursor : cursor+IVSize]
		cursor += IVSize
		plain := make([]byte, len(packet)-cursor)
		ctrForTest(t, client.key[:], iv).XORKeyStream(plain, packet[cursor:])
		var expected bytes.Buffer
		if writeErr := M.SocksaddrSerializer.WriteAddrPort(&expected, destination); writeErr != nil {
			serverDone <- writeErr
			return
		}
		expected.Write(requestPayload)
		if !bytes.Equal(plain, expected.Bytes()) {
			serverDone <- io.ErrUnexpectedEOF
			return
		}

		responseIV := bytes.Repeat([]byte{0x42}, IVSize)
		responseCiphertext := make([]byte, len(responsePayload))
		ctrForTest(t, client.key[:], responseIV).XORKeyStream(responseCiphertext, responsePayload)
		response := append([]byte("server-camouflage"), client.magic...)
		response = append(response, byte(len(client.token)))
		response = append(response, client.token...)
		response = append(response, byte(IVSize))
		response = append(response, responseIV...)
		response = append(response, responseCiphertext...)
		_, writeErr := right.Write(response)
		serverDone <- writeErr
	}()

	if n, err := wrapped.Write(requestPayload); err != nil || n != len(requestPayload) {
		t.Fatalf("Write n=%d err=%v", n, err)
	}
	response := make([]byte, len(responsePayload))
	if _, err = io.ReadFull(wrapped, response); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(response, responsePayload) {
		t.Fatalf("response=%q", response)
	}
	if err = <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestCipherConnIsNeverReplaceable(t *testing.T) {
	client, err := NewClient(
		DefaultKey1+":"+"02aabb",
		strings.Repeat("01", SessionSize),
		"aabb",
	)
	if err != nil {
		t.Fatal(err)
	}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	conn := client.StreamConn(left, M.ParseSocksaddrHostPort("example.com", 443)).(*Conn)
	if conn.ReaderReplaceable() || conn.WriterReplaceable() {
		t.Fatal("stateful AES-CTR connection must never expose its raw reader/writer")
	}
	if conn.NeedAdditionalReadDeadline() {
		t.Fatal("additional read deadline must stay disabled")
	}
}
