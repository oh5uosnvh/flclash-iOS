package anytls

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/metacubex/mihomo/common/buf"
	"github.com/metacubex/mihomo/transport/anytls/padding"
	"github.com/metacubex/mihomo/transport/anytls/session"
	"github.com/metacubex/mihomo/transport/vmess"

	M "github.com/metacubex/sing/common/metadata"
	N "github.com/metacubex/sing/common/network"
)

type ClientConfig struct {
	Password                 string
	ClientMetadata           string
	IdleSessionCheckInterval time.Duration
	IdleSessionTimeout       time.Duration
	MinIdleSession           int
	DisableReuse             bool
	Server                   M.Socksaddr
	Dialer                   N.Dialer
	TLSConfig                *vmess.TLSConfig
}

type Client struct {
	passwordSha256 []byte
	tlsConfig      *vmess.TLSConfig
	dialer         N.Dialer
	server         M.Socksaddr
	clientName     string
	sessionClient  *session.Client
	padding        atomic.Pointer[padding.PaddingFactory]
}

const shanlianClientName = "sing-anytls/0.0.11"

// shanlianPasswordMarker is deliberately explicit. A plain 64-hex password
// remains a normal AnyTLS password; only a password ending in this marker uses
// the LightXtreme/Shanlian wire-compatible password treatment.
const shanlianPasswordMarker = "#SL"

// decodeShanlianPassword converts the marked 64-hex form into the exact 32
// bytes expected in the Shanlian AnyTLS preamble. The bytes are the account
// UUID, ticket and counter concatenated by the provider. No hash is applied.
func decodeShanlianPassword(password string) ([]byte, bool) {
	if !strings.HasSuffix(password, shanlianPasswordMarker) {
		return nil, false
	}

	hexPart := strings.TrimSuffix(password, shanlianPasswordMarker)
	if len(hexPart) != 64 {
		return nil, false
	}

	decoded, err := hex.DecodeString(hexPart)
	if err != nil || len(decoded) != 32 {
		return nil, false
	}
	return decoded, true
}

// IsShanlianPassword reports whether password is a valid explicitly marked
// Shanlian credential. It is used by the adapter to apply Shanlian-specific
// TLS defaults without changing ordinary AnyTLS nodes.
func IsShanlianPassword(password string) bool {
	_, ok := decodeShanlianPassword(password)
	return ok
}

func NewClient(ctx context.Context, config ClientConfig) *Client {
	var passwordSha256 []byte
	decoded, shanlian := decodeShanlianPassword(config.Password)
	if shanlian {
		// Shanlian mode: the decoded 32 bytes are already the protocol's
		// password field. In particular, do not SHA-256 them.
		passwordSha256 = decoded
	} else {
		// Stock AnyTLS compatibility: hash the supplied password exactly as
		// upstream does. This also makes malformed marked values harmless.
		pw := sha256.Sum256([]byte(config.Password))
		passwordSha256 = pw[:]
	}

	clientName := ""
	if shanlian {
		clientName = shanlianClientName
	}
	c := &Client{
		passwordSha256: passwordSha256,
		tlsConfig:      config.TLSConfig,
		dialer:         config.Dialer,
		server:         config.Server,
		clientName:     clientName,
	}
	// Initialize the padding state of this client
	padding.UpdatePaddingScheme(padding.DefaultPaddingScheme, &c.padding)
	c.sessionClient = session.NewClient(ctx, c.createOutboundTLSConnection, &c.padding, config.ClientMetadata, config.IdleSessionCheckInterval, config.IdleSessionTimeout, config.MinIdleSession, config.DisableReuse, clientName)
	return c
}

func (c *Client) CreateProxy(ctx context.Context, destination M.Socksaddr) (net.Conn, error) {
	conn, err := c.sessionClient.CreateStream(ctx)
	if err != nil {
		return nil, err
	}
	err = M.SocksaddrSerializer.WriteAddrPort(conn, destination)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func (c *Client) createOutboundTLSConnection(ctx context.Context) (net.Conn, error) {
	conn, err := c.dialer.DialContext(ctx, N.NetworkTCP, c.server)
	if err != nil {
		return nil, err
	}

	b := buf.NewPacket()
	defer b.Release()

	b.Write(c.passwordSha256)
	var paddingLen int
	if pad := c.padding.Load().GenerateRecordPayloadSizes(0); len(pad) > 0 {
		paddingLen = pad[0]
	}
	binary.BigEndian.PutUint16(b.Extend(2), uint16(paddingLen))
	if paddingLen > 0 {
		b.WriteZeroN(paddingLen)
	}

	tlsConn, err := vmess.StreamTLSConn(ctx, conn, c.tlsConfig)
	if err != nil {
		conn.Close()
		return nil, err
	}

	_, err = b.WriteTo(tlsConn)
	if err != nil {
		tlsConn.Close()
		return nil, err
	}
	return tlsConn, nil
}

func (h *Client) Close() error {
	return h.sessionClient.Close()
}
