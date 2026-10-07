package x365

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/metacubex/mihomo/common/buf"
	N "github.com/metacubex/mihomo/common/net"

	vmess "github.com/metacubex/sing-vmess"
	"github.com/metacubex/sing/common"
	M "github.com/metacubex/sing/common/metadata"

	"github.com/gofrs/uuid/v5"
)

// x365 wire protocol (reverse-engineered from 饿饭/365VPN libgojni.so, function _ao/_ak):
//
// request header (client -> server):
//   [0:4]   magic "X365"        (0x58 0x33 0x36 0x35)
//   [4]     version 0x01
//   [5]     command             (1=TCP 2=UDP)
//   [6:22]  uuid 16 bytes
//   [22:24] port  big-endian
//   [24]    atyp                (0x01 IPv4 / 0x02 domain[len] / 0x03 IPv6)
//   [25:]   address
//   the port+atyp+addr trailer is exactly vmess.AddressSerializer (PortThenAddress).
//
// response header (server -> client): at least 5 bytes, first 4 must equal "X365".
// after the handshake both directions carry the raw relayed stream.

const (
	Version    byte = 0x01
	CommandTCP byte = 1
	CommandUDP byte = 2
)

var magic = [4]byte{'X', '3', '6', '5'}

// Client holds the parsed UUID key.
type Client struct {
	key [16]byte
}

func NewClient(userId string) (*Client, error) {
	user, err := uuid.FromString(userId)
	if err != nil {
		user = uuid.NewV5(uuid.Nil, userId)
	}
	return &Client{key: user}, nil
}

// StreamConn wraps a transport conn with the x365 TCP handshake.
func (c *Client) StreamConn(conn net.Conn, destination M.Socksaddr) net.Conn {
	return newConn(conn, c.key, CommandTCP, destination)
}

// PacketConn wraps a transport conn with the x365 UDP handshake.
func (c *Client) PacketConn(conn net.Conn, destination M.Socksaddr) *PacketConn {
	return &PacketConn{Conn: conn, key: c.key, destination: destination}
}

func requestLen(destination M.Socksaddr) int {
	// magic(4) + version(1) + command(1) + uuid(16) + addr trailer
	return 4 + 1 + 1 + 16 + vmess.AddressSerializer.AddrPortLen(destination)
}

func encodeRequest(buffer *buf.Buffer, key [16]byte, command byte, destination M.Socksaddr) error {
	common.Must(
		common.Error(buffer.Write(magic[:])),
		buffer.WriteByte(Version),
		buffer.WriteByte(command),
		common.Error(buffer.Write(key[:])),
	)
	return vmess.AddressSerializer.WriteAddrPort(buffer, destination)
}

// WriteRequest writes the x365 request header followed by an optional first payload.
func WriteRequest(writer io.Writer, key [16]byte, command byte, destination M.Socksaddr, payload []byte) error {
	buffer := buf.NewSize(requestLen(destination) + len(payload))
	defer buffer.Release()
	if err := encodeRequest(buffer, key, command, destination); err != nil {
		return err
	}
	common.Must1(buffer.Write(payload))
	return common.Error(writer.Write(buffer.Bytes()))
}

// ReadResponse reads and validates the 5-byte server response header.
func ReadResponse(reader io.Reader) error {
	var header [5]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return err
	}
	if header[0] != magic[0] || header[1] != magic[1] || header[2] != magic[2] || header[3] != magic[3] {
		return fmt.Errorf("x365: invalid response magic: %x", header[:4])
	}
	return nil
}

type Conn struct {
	N.ExtendedConn
	key            [16]byte
	command        byte
	destination    M.Socksaddr
	requestWritten bool
	responseRead   bool
}

func newConn(conn net.Conn, key [16]byte, command byte, destination M.Socksaddr) *Conn {
	return &Conn{
		ExtendedConn: N.NewExtendedConn(conn),
		key:          key,
		command:      command,
		destination:  destination,
	}
}

func (c *Conn) Read(b []byte) (int, error) {
	if !c.responseRead {
		if err := ReadResponse(c.ExtendedConn); err != nil {
			return 0, err
		}
		c.responseRead = true
	}
	return c.ExtendedConn.Read(b)
}

func (c *Conn) ReadBuffer(buffer *buf.Buffer) error {
	if !c.responseRead {
		if err := ReadResponse(c.ExtendedConn); err != nil {
			return err
		}
		c.responseRead = true
	}
	return c.ExtendedConn.ReadBuffer(buffer)
}

func (c *Conn) Write(b []byte) (int, error) {
	if !c.requestWritten {
		if err := WriteRequest(c.ExtendedConn, c.key, c.command, c.destination, b); err != nil {
			return 0, err
		}
		c.requestWritten = true
		return len(b), nil
	}
	return c.ExtendedConn.Write(b)
}

func (c *Conn) WriteBuffer(buffer *buf.Buffer) error {
	if !c.requestWritten {
		header := buf.With(buffer.ExtendHeader(requestLen(c.destination)))
		if err := encodeRequest(header, c.key, c.command, c.destination); err != nil {
			return err
		}
		c.requestWritten = true
	}
	return c.ExtendedConn.WriteBuffer(buffer)
}

func (c *Conn) FrontHeadroom() int {
	if c.requestWritten {
		return 0
	}
	return requestLen(c.destination)
}

func (c *Conn) ReaderReplaceable() bool { return c.responseRead }
func (c *Conn) WriterReplaceable() bool { return c.requestWritten }
func (c *Conn) NeedHandshake() bool     { return !c.requestWritten }
func (c *Conn) Upstream() any           { return c.ExtendedConn }

// PacketConn carries UDP payloads, each framed with a 2-byte big-endian length prefix.
type PacketConn struct {
	net.Conn
	access         sync.Mutex
	key            [16]byte
	destination    M.Socksaddr
	requestWritten bool
	responseRead   bool
}

func (c *PacketConn) Read(b []byte) (int, error) {
	if !c.responseRead {
		if err := ReadResponse(c.Conn); err != nil {
			return 0, err
		}
		c.responseRead = true
	}
	var length uint16
	if err := binary.Read(c.Conn, binary.BigEndian, &length); err != nil {
		return 0, err
	}
	if len(b) < int(length) {
		return 0, io.ErrShortBuffer
	}
	return io.ReadFull(c.Conn, b[:length])
}

func (c *PacketConn) writeRequest(payload []byte) error {
	buffer := buf.NewSize(requestLen(c.destination) + 2 + len(payload))
	defer buffer.Release()
	if err := encodeRequest(buffer, c.key, CommandUDP, c.destination); err != nil {
		return err
	}
	if len(payload) > 0 {
		common.Must(
			binary.Write(buffer, binary.BigEndian, uint16(len(payload))),
			common.Error(buffer.Write(payload)),
		)
	}
	return common.Error(c.Conn.Write(buffer.Bytes()))
}

func (c *PacketConn) Write(b []byte) (int, error) {
	if !c.requestWritten {
		c.access.Lock()
		if !c.requestWritten {
			err := c.writeRequest(b)
			c.requestWritten = true
			c.access.Unlock()
			if err != nil {
				return 0, err
			}
			return len(b), nil
		}
		c.access.Unlock()
	}
	if err := binary.Write(c.Conn, binary.BigEndian, uint16(len(b))); err != nil {
		return 0, err
	}
	return c.Conn.Write(b)
}

func (c *PacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	defer buffer.Release()
	dataLen := buffer.Len()
	binary.BigEndian.PutUint16(buffer.ExtendHeader(2), uint16(dataLen))
	if !c.requestWritten {
		c.access.Lock()
		if !c.requestWritten {
			header := buf.With(buffer.ExtendHeader(requestLen(c.destination)))
			err := encodeRequest(header, c.key, CommandUDP, c.destination)
			c.requestWritten = true
			c.access.Unlock()
			if err != nil {
				return err
			}
			return common.Error(c.Conn.Write(buffer.Bytes()))
		}
		c.access.Unlock()
	}
	return common.Error(c.Conn.Write(buffer.Bytes()))
}

func (c *PacketConn) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	if !c.responseRead {
		if err := ReadResponse(c.Conn); err != nil {
			return M.Socksaddr{}, err
		}
		c.responseRead = true
	}
	var length uint16
	if err := binary.Read(c.Conn, binary.BigEndian, &length); err != nil {
		return M.Socksaddr{}, err
	}
	_, err := buffer.ReadFullFrom(c.Conn, int(length))
	if err != nil {
		return M.Socksaddr{}, err
	}
	return c.destination, nil
}

func (c *PacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, err := c.Read(p)
	if err != nil {
		return 0, nil, err
	}
	if c.destination.IsFqdn() {
		return n, c.destination, nil
	}
	return n, c.destination.UDPAddr(), nil
}

func (c *PacketConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	return c.Write(p)
}

func (c *PacketConn) FrontHeadroom() int               { return requestLen(c.destination) + 2 }
func (c *PacketConn) NeedAdditionalReadDeadline() bool { return true }
func (c *PacketConn) Upstream() any                    { return c.Conn }
