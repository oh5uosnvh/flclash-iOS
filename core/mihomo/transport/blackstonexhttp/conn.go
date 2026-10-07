package blackstonexhttp

// Package blackstonexhttp implements the private wire protocol used by
// Blackstone VPN's Clash proxy type "xhttp". Despite its name and decoy
// tls/xhttp fields, the verified wire is bare TCP with a camouflage/auth
// prefix followed by an AES-128-CTR stream.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"

	"github.com/metacubex/mihomo/log"
	M "github.com/metacubex/sing/common/metadata"
)

const (
	IVSize            = 16
	KeySize           = 16
	SessionSize       = 68
	RandomPrefixSize  = 60
	MagicOffset       = SessionSize + RandomPrefixSize
	maxResponseHeader = 64 * 1024
	// DefaultKey1 is the 2026-09 server-rotated default key1 (Neko libcore
	// parity). Nodes normally carry their own key1 inside password.
	DefaultKey1 = "11cb12c8cf84e0568896a7f6bd61ab53"
)

// Since the 2026-09 server-side key1 rotation the wire MAGIC is PER-KEY1:
// MD5(key1ASCII + magicSuffix) — it is no longer a constant. The fixed bytes
// below are the legacy (August generation, key1 177fd09c…) fingerprint and are
// only used as the fallback for an empty key1. Hardcoding them for any other
// key1 makes the gateway reject the handshake ("response magic not found").
var legacyMagic = []byte{
	0x62, 0xd7, 0x70, 0x75, 0x10, 0x60, 0x53, 0xa2,
	0xee, 0x4f, 0xaf, 0x95, 0x89, 0xf9, 0xa2, 0x06,
}

// magicSuffix is the domain-separation string the official kernel appends to
// key1 before MD5 (recovered from libgojni.so PickCipher).
const magicSuffix = "do not hack this protocol please"

// deriveMagic reproduces the official PickCipher magic derivation.
func deriveMagic(key1 string) []byte {
	if key1 == "" {
		return append([]byte(nil), legacyMagic...)
	}
	sum := md5.Sum([]byte(key1 + magicSuffix))
	return sum[:]
}

type Client struct {
	key     [KeySize]byte
	session [SessionSize]byte
	token   []byte
	magic   []byte // per-key1 derived wire magic (see deriveMagic)
}

func kdfMD5(secret []byte, keyLen int) []byte {
	var out, previous []byte
	for len(out) < keyLen {
		h := md5.New()
		_, _ = h.Write(previous)
		_, _ = h.Write(secret)
		previous = h.Sum(nil)
		out = append(out, previous...)
	}
	return out[:keyLen]
}

func decodeHexField(name, value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("xhttp: missing %s", name)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("xhttp: invalid %s hex: %w", name, err)
	}
	return decoded, nil
}

// ParsePassword returns the ASCII key1 and, when present, the token embedded in
// password's key2. key2 is [one-byte token length][token].
func ParsePassword(password string) (string, []byte, error) {
	key1 := DefaultKey1
	var token []byte
	if password == "" {
		return key1, nil, nil
	}
	parts := strings.SplitN(password, ":", 2)
	if parts[0] != "" {
		key1 = parts[0]
	}
	if len(parts) == 2 && parts[1] != "" {
		key2, err := decodeHexField("password key2", parts[1])
		if err != nil {
			return "", nil, err
		}
		if len(key2) >= 2 && int(key2[0]) == len(key2)-1 {
			token = append([]byte(nil), key2[1:]...)
		} else {
			return "", nil, fmt.Errorf("xhttp: key2 length prefix=%d, payload=%d", key2[0], len(key2)-1)
		}
	}
	if key1 == "" {
		return "", nil, errors.New("xhttp: empty key1")
	}
	return key1, token, nil
}

// NewClient validates all server-issued authentication material. authHex takes
// precedence over the token embedded in password.
func NewClient(password, sessionHex, authHex string) (*Client, error) {
	key1, passwordToken, err := ParsePassword(password)
	if err != nil {
		return nil, err
	}
	session, err := decodeHexField("session/fake-net.tcp", sessionHex)
	if err != nil {
		return nil, err
	}
	if len(session) != SessionSize {
		return nil, fmt.Errorf("xhttp: session must be %d bytes, got %d", SessionSize, len(session))
	}
	token := passwordToken
	if strings.TrimSpace(authHex) != "" {
		token, err = decodeHexField("auth", authHex)
		if err != nil {
			return nil, err
		}
	}
	if len(token) == 0 || len(token) > 255 {
		return nil, fmt.Errorf("xhttp: auth token length must be 1..255, got %d", len(token))
	}
	client := &Client{
		token: append([]byte(nil), token...),
		magic: deriveMagic(key1),
	}
	copy(client.session[:], session)
	copy(client.key[:], kdfMD5([]byte(key1), KeySize))
	return client, nil
}

func (c *Client) StreamConn(conn net.Conn, destination M.Socksaddr) net.Conn {
	return &Conn{Conn: conn, client: c, destination: destination}
}

type Conn struct {
	net.Conn
	client      *Client
	destination M.Socksaddr

	writeMu      sync.Mutex
	writeStarted bool
	writeStream  cipher.Stream

	readMu      sync.Mutex
	readStarted bool
	readStream  cipher.Stream
	readBuffer  []byte
	pending     []byte
}

func newCTR(key, iv []byte) (cipher.Stream, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(iv) != aes.BlockSize {
		return nil, fmt.Errorf("xhttp: invalid IV length %d", len(iv))
	}
	return cipher.NewCTR(block, iv), nil
}

func randomBytes(n int) ([]byte, error) {
	value := make([]byte, n)
	_, err := io.ReadFull(rand.Reader, value)
	return value, err
}

func writeFull(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		n, err := writer.Write(value)
		if n > 0 {
			value = value[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func (c *Conn) prefix(iv []byte) ([]byte, error) {
	randomPrefix, err := randomBytes(RandomPrefixSize)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, MagicOffset+len(c.client.magic)+1+len(c.client.token)+1+IVSize)
	out = append(out, c.client.session[:]...)
	out = append(out, randomPrefix...)
	out = append(out, c.client.magic...)
	out = append(out, byte(len(c.client.token)))
	out = append(out, c.client.token...)
	out = append(out, byte(IVSize))
	out = append(out, iv...)
	return out, nil
}

func (c *Conn) initialWrite(payload []byte) error {
	iv, err := randomBytes(IVSize)
	if err != nil {
		return err
	}
	stream, err := newCTR(c.client.key[:], iv)
	if err != nil {
		return err
	}
	prefix, err := c.prefix(iv)
	if err != nil {
		return err
	}
	var plain bytes.Buffer
	if err = M.SocksaddrSerializer.WriteAddrPort(&plain, c.destination); err != nil {
		return err
	}
	_, _ = plain.Write(payload)
	ciphertext := make([]byte, plain.Len())
	stream.XORKeyStream(ciphertext, plain.Bytes())
	packet := append(prefix, ciphertext...)
	if err = writeFull(c.Conn, packet); err != nil {
		log.Warnln("[xhttp] handshake write error dst=%v: %v", c.destination, err)
		return err
	}
	c.writeStream = stream
	c.writeStarted = true
	log.Debugln("[xhttp] handshake sent dst=%v prefix=%dB payload=%dB", c.destination, len(prefix), plain.Len())
	return nil
}

func (c *Conn) Write(payload []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if !c.writeStarted {
		if err := c.initialWrite(payload); err != nil {
			return 0, err
		}
		return len(payload), nil
	}
	ciphertext := make([]byte, len(payload))
	c.writeStream.XORKeyStream(ciphertext, payload)
	if err := writeFull(c.Conn, ciphertext); err != nil {
		return 0, err
	}
	return len(payload), nil
}

func (c *Conn) parseResponseHeader() ([]byte, bool, error) {
	magicAt := bytes.Index(c.readBuffer, c.client.magic)
	if magicAt < 0 {
		if len(c.readBuffer) > maxResponseHeader {
			log.Warnln("[xhttp] response magic not found in %dB (dst=%v), first bytes: % x", len(c.readBuffer), c.destination, c.readBuffer[:min(32, len(c.readBuffer))])
			return nil, false, errors.New("xhttp: response magic not found within 64KiB")
		}
		return nil, false, nil
	}
	cursor := magicAt + len(c.client.magic)
	if cursor >= len(c.readBuffer) {
		return nil, false, nil
	}
	tokenLength := int(c.readBuffer[cursor])
	cursor++
	if cursor+tokenLength >= len(c.readBuffer) {
		return nil, false, nil
	}
	cursor += tokenLength
	ivLength := int(c.readBuffer[cursor])
	cursor++
	if ivLength != IVSize {
		return nil, false, fmt.Errorf("xhttp: response IV length=%d, want %d", ivLength, IVSize)
	}
	if cursor+ivLength > len(c.readBuffer) {
		return nil, false, nil
	}
	stream, err := newCTR(c.client.key[:], c.readBuffer[cursor:cursor+ivLength])
	if err != nil {
		return nil, false, err
	}
	cursor += ivLength
	leftover := append([]byte(nil), c.readBuffer[cursor:]...)
	c.readBuffer = nil
	c.readStream = stream
	c.readStarted = true
	log.Debugln("[xhttp] response header parsed dst=%v magicAt=%d tokLen=%d leftover=%dB", c.destination, magicAt, tokenLength, len(leftover))
	return leftover, true, nil
}

func (c *Conn) Read(payload []byte) (int, error) {
	if len(payload) == 0 {
		return 0, nil
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if len(c.pending) > 0 {
		n := copy(payload, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}
	for !c.readStarted {
		temporary := make([]byte, 32*1024)
		n, err := c.Conn.Read(temporary)
		if n > 0 {
			c.readBuffer = append(c.readBuffer, temporary[:n]...)
			leftover, ready, parseErr := c.parseResponseHeader()
			if parseErr != nil {
				log.Warnln("[xhttp] header parse error dst=%v: %v", c.destination, parseErr)
				return 0, parseErr
			}
			if ready {
				if len(leftover) > 0 {
					plain := make([]byte, len(leftover))
					c.readStream.XORKeyStream(plain, leftover)
					copied := copy(payload, plain)
					c.pending = append(c.pending, plain[copied:]...)
					return copied, nil
				}
				break
			}
		}
		if err != nil {
			log.Debugln("[xhttp] read (header phase) dst=%v n=%d err=%v", c.destination, n, err)
			return 0, err
		}
	}
	n, err := c.Conn.Read(payload)
	if n > 0 {
		c.readStream.XORKeyStream(payload[:n], payload[:n])
	}
	if err != nil && !errors.Is(err, io.EOF) {
		log.Debugln("[xhttp] read (stream phase) dst=%v n=%d err=%v", c.destination, n, err)
	}
	return n, err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (c *Conn) NeedHandshake() bool { return !c.writeStarted }

// AES-CTR is stateful for the entire connection. The reader/writer must NEVER
// be unwrapped to the underlying TCP connection after the framing handshake;
// doing so bypasses encryption/decryption for later TLS/MTProto flights.
func (c *Conn) ReaderReplaceable() bool          { return false }
func (c *Conn) WriterReplaceable() bool          { return false }
func (c *Conn) Upstream() any                    { return c.Conn }
func (c *Conn) FrontHeadroom() int               { return 0 }
func (c *Conn) NeedAdditionalReadDeadline() bool { return false }

func init() {
	log.Debugln("[xhttp] blackstonexhttp transport loaded (diag build, noAdditionalReadDeadline)")
}
