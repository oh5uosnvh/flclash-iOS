// Package oppa implements the "oppa" private protocol
// (libcore/protocol/oppa reference implementation). It is a bare-Trojan-style
// tunnel over standard Go TLS:
//
// TCP:  TLS -> [password][0x01][SOCKS address] -> raw bidirectional stream
// UDP:  TLS -> [password][0x02], then each datagram is framed as
//
//	[2-byte BE length][src address (always 0.0.0.0)][dst address][payload]
//
// There is no session multiplexing, no padding and no keepalive on the wire;
// every TCP connection and every UDP session performs its own TLS handshake.
package oppa

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	M "github.com/metacubex/sing/common/metadata"
)

const (
	// CommandTCP opens a raw stream relay for a single destination.
	CommandTCP byte = 0x01
	// CommandUDP switches the session into the datagram framing mode.
	CommandUDP byte = 0x02

	// MaxPasswordLength is the protocol limit for the cleartext password.
	MaxPasswordLength = 4096
)

// AppendAddress appends a SOCKS-style address (type byte + addr + 2-byte BE
// port) to buffer. Domain (0x03), IPv4 (0x01) and IPv6 (0x04) are supported,
// mirroring the reference implementation exactly.
func AppendAddress(buffer []byte, address M.Socksaddr) ([]byte, error) {
	if address.IsFqdn() {
		domain := []byte(address.Fqdn)
		if len(domain) == 0 || len(domain) > 255 {
			return nil, errors.New("oppa: invalid domain length")
		}
		buffer = append(buffer, 0x03, byte(len(domain)))
		buffer = append(buffer, domain...)
	} else if address.Addr.Is4() {
		buffer = append(buffer, 0x01)
		ip := address.Addr.As4()
		buffer = append(buffer, ip[:]...)
	} else if address.Addr.Is6() {
		buffer = append(buffer, 0x04)
		ip := address.Addr.As16()
		buffer = append(buffer, ip[:]...)
	} else {
		return nil, errors.New("oppa: invalid address")
	}
	return binary.BigEndian.AppendUint16(buffer, address.Port), nil
}

// ParseAddress reads a SOCKS-style address from reader.
func ParseAddress(reader io.Reader) (M.Socksaddr, error) {
	var kind [1]byte
	if _, err := io.ReadFull(reader, kind[:]); err != nil {
		return M.Socksaddr{}, err
	}
	switch kind[0] {
	case 0x01:
		var raw [6]byte
		if _, err := io.ReadFull(reader, raw[:]); err != nil {
			return M.Socksaddr{}, err
		}
		addr, ok := netip.AddrFromSlice(raw[:4])
		if !ok {
			return M.Socksaddr{}, errors.New("oppa: invalid IPv4 address")
		}
		return M.Socksaddr{Addr: addr, Port: binary.BigEndian.Uint16(raw[4:])}, nil
	case 0x03:
		var size [1]byte
		if _, err := io.ReadFull(reader, size[:]); err != nil {
			return M.Socksaddr{}, err
		}
		raw := make([]byte, int(size[0])+2)
		if _, err := io.ReadFull(reader, raw); err != nil {
			return M.Socksaddr{}, err
		}
		return M.Socksaddr{Fqdn: string(raw[:len(raw)-2]), Port: binary.BigEndian.Uint16(raw[len(raw)-2:])}, nil
	case 0x04:
		var raw [18]byte
		if _, err := io.ReadFull(reader, raw[:]); err != nil {
			return M.Socksaddr{}, err
		}
		addr, ok := netip.AddrFromSlice(raw[:16])
		if !ok {
			return M.Socksaddr{}, errors.New("oppa: invalid IPv6 address")
		}
		return M.Socksaddr{Addr: addr, Port: binary.BigEndian.Uint16(raw[16:])}, nil
	default:
		return M.Socksaddr{}, fmt.Errorf("oppa: unsupported address type %d", kind[0])
	}
}

// BuildSessionHeader returns the session prefix: password bytes followed by
// the command byte. Nothing else — the protocol carries no magic or version.
func BuildSessionHeader(password string, command byte) ([]byte, error) {
	passwordLength := len([]byte(password))
	if passwordLength == 0 || passwordLength > MaxPasswordLength {
		return nil, fmt.Errorf("oppa: password length must be between 1 and %d bytes", MaxPasswordLength)
	}
	return append(append([]byte(nil), []byte(password)...), command), nil
}

// BuildStreamHeader returns the full TCP session header:
// [password][0x01][destination address].
func BuildStreamHeader(password string, destination M.Socksaddr) ([]byte, error) {
	header, err := BuildSessionHeader(password, CommandTCP)
	if err != nil {
		return nil, err
	}
	return AppendAddress(header, destination)
}

// PacketConn adapts a TLS stream (already authenticated by the UDP session
// header) to net.PacketConn using the oppa UDP framing. The reference client
// pins the source address to 0.0.0.0 on every outgoing frame.
type PacketConn struct {
	net.Conn
	readMu  sync.Mutex
	writeMu sync.Mutex
}

func NewPacketConn(conn net.Conn) *PacketConn {
	return &PacketConn{Conn: conn}
}

func (c *PacketConn) ReadFrom(buffer []byte) (int, net.Addr, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	var sizeBytes [2]byte
	if _, err := io.ReadFull(c.Conn, sizeBytes[:]); err != nil {
		return 0, nil, err
	}
	size := int(binary.BigEndian.Uint16(sizeBytes[:]))
	if size < 4 || size > 65535 {
		return 0, nil, fmt.Errorf("oppa: invalid UDP frame length %d", size)
	}
	frame := make([]byte, size)
	if _, err := io.ReadFull(c.Conn, frame); err != nil {
		return 0, nil, err
	}
	reader := bytes.NewReader(frame)
	if _, err := ParseAddress(reader); err != nil {
		return 0, nil, err
	}
	destination, err := ParseAddress(reader)
	if err != nil {
		return 0, nil, err
	}
	if reader.Len() > len(buffer) {
		return 0, nil, io.ErrShortBuffer
	}
	n, _ := reader.Read(buffer)
	return n, destination.UDPAddr(), nil
}

func (c *PacketConn) WriteTo(payload []byte, address net.Addr) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	destination := M.SocksaddrFromNet(address).Unwrap()
	body, err := AppendAddress(nil, M.Socksaddr{Addr: netip.IPv4Unspecified()})
	if err != nil {
		return 0, err
	}
	body, err = AppendAddress(body, destination)
	if err != nil {
		return 0, err
	}
	body = append(body, payload...)
	if len(body) > 65535 {
		return 0, errors.New("oppa: UDP payload too large")
	}
	frame := binary.BigEndian.AppendUint16(nil, uint16(len(body)))
	frame = append(frame, body...)
	if _, err = c.Conn.Write(frame); err != nil {
		return 0, err
	}
	return len(payload), nil
}

func (c *PacketConn) LocalAddr() net.Addr           { return c.Conn.LocalAddr() }
func (c *PacketConn) SetDeadline(t time.Time) error { return c.Conn.SetDeadline(t) }
func (c *PacketConn) SetReadDeadline(t time.Time) error {
	return c.Conn.SetReadDeadline(t)
}
func (c *PacketConn) SetWriteDeadline(t time.Time) error {
	return c.Conn.SetWriteDeadline(t)
}

// DecodePinHash accepts a certificate pin as colon-stripped hex or
// raw/standard/padded URL-safe base64 (SHA-256, 32 bytes).
func DecodePinHash(raw string) ([]byte, error) {
	clean := bytes.ReplaceAll([]byte(raw), []byte(":"), nil)
	if decoded, err := hex.DecodeString(string(clean)); err == nil && len(decoded) == sha256.Size {
		return decoded, nil
	}
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.StdEncoding} {
		if decoded, err := encoding.DecodeString(raw); err == nil && len(decoded) == sha256.Size {
			return decoded, nil
		}
	}
	return nil, errors.New("oppa: invalid SHA-256 pin")
}

// CertChainHash computes the chained certificate pin used by the reference
// implementation: chain = sha256(cert0); chain = sha256(chain + sha256(certN))...
func CertChainHash(rawCerts [][]byte) []byte {
	var chain []byte
	for _, cert := range rawCerts {
		digest := sha256.Sum256(cert)
		if chain == nil {
			chain = digest[:]
		} else {
			next := sha256.Sum256(append(chain, digest[:]...))
			chain = next[:]
		}
	}
	return chain
}
