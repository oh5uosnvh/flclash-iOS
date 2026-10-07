package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	N "github.com/metacubex/mihomo/common/net"
	C "github.com/metacubex/mihomo/constant"
	blackstonexhttp "github.com/metacubex/mihomo/transport/blackstonexhttp"

	M "github.com/metacubex/sing/common/metadata"
	"github.com/metacubex/sing/common/uot"

	"github.com/metacubex/mihomo/log"
)

// XHTTP is Blackstone VPN's private Clash proxy type "xhttp". The fields named
// tls/network/xhttp-opts in official configurations are accepted by the parser
// but intentionally ignored: packet captures prove that the gateway wire is
// bare TCP with the blackstonexhttp framing.
type XHTTP struct {
	*Base
	client *blackstonexhttp.Client
	option *XHTTPOption
}

type XHTTPFakeNetOption struct {
	TCP string `proxy:"tcp,omitempty"`
	UDP string `proxy:"udp,omitempty"`
}

type XHTTPOption struct {
	BasicOption
	Name     string `proxy:"name"`
	Server   string `proxy:"server"`
	Port     int    `proxy:"port"`
	Password string `proxy:"password,omitempty"`
	Cipher   string `proxy:"cipher,omitempty"`

	// Official decrypted YAML stores the authenticated gateway in the final
	// "-host-port" suffix of name while server/port identify a rotating landing.
	// Converted configs may instead set gateway or gateway-server/gateway-port.
	Gateway       string `proxy:"gateway,omitempty"`
	GatewayServer string `proxy:"gateway-server,omitempty"`
	GatewayPort   int    `proxy:"gateway-port,omitempty"`

	FakeNet    XHTTPFakeNetOption `proxy:"fake-net,omitempty"`
	FakeNetTCP string             `proxy:"fake-net-tcp,omitempty"`
	Sess       string             `proxy:"sess,omitempty"`
	Auth       string             `proxy:"auth,omitempty"`
	PaddingLen string             `proxy:"padding-len,omitempty"`

	UDP               bool `proxy:"udp,omitempty"`
	UDPOverTCP        bool `proxy:"udp-over-tcp,omitempty"`
	UDPOverTCPVersion int  `proxy:"udp-over-tcp-version,omitempty"`

	// Decoy/compatibility fields retained so native Blackstone Clash YAML can be
	// consumed without conversion. They are not used on the verified wire.
	Network           string       `proxy:"network,omitempty"`
	TLS               bool         `proxy:"tls,omitempty"`
	ServerName        string       `proxy:"servername,omitempty"`
	ClientFingerprint string       `proxy:"client-fingerprint,omitempty"`
	XHTTPOpts         XHTTPOptions `proxy:"xhttp-opts,omitempty"`
}

func splitHostPort(value string) (string, int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", 0, errors.New("empty address")
	}
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		// Common Clash configs omit brackets only for plain host/IPv4; accept the
		// final colon form while preserving bracketed IPv6 via SplitHostPort.
		at := strings.LastIndexByte(value, ':')
		if at <= 0 || at == len(value)-1 {
			return "", 0, err
		}
		host, portText = value[:at], value[at+1:]
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("invalid port %q", portText)
	}
	return strings.Trim(host, "[]"), port, nil
}

func gatewayFromOfficialName(name string) (string, int, bool) {
	lastDash := strings.LastIndexByte(name, '-')
	if lastDash <= 0 || lastDash == len(name)-1 {
		return "", 0, false
	}
	previousDash := strings.LastIndexByte(name[:lastDash], '-')
	if previousDash < 0 || previousDash+1 >= lastDash {
		return "", 0, false
	}
	host := name[previousDash+1 : lastDash]
	// Avoid treating an ordinary label ending "-foo-123" as an endpoint.
	if !strings.ContainsAny(host, ".:") && net.ParseIP(host) == nil {
		return "", 0, false
	}
	port, err := strconv.Atoi(name[lastDash+1:])
	if err != nil || port < 1 || port > 65535 {
		return "", 0, false
	}
	return host, port, true
}

func resolveXHTTPGateway(option XHTTPOption) (string, int, error) {
	if option.GatewayServer != "" || option.GatewayPort != 0 {
		if option.GatewayServer == "" || option.GatewayPort < 1 || option.GatewayPort > 65535 {
			return "", 0, errors.New("xhttp: gateway-server and valid gateway-port must be set together")
		}
		return option.GatewayServer, option.GatewayPort, nil
	}
	if option.Gateway != "" {
		host, port, err := splitHostPort(option.Gateway)
		if err != nil {
			return "", 0, fmt.Errorf("xhttp: invalid gateway: %w", err)
		}
		return host, port, nil
	}
	if host, port, ok := gatewayFromOfficialName(option.Name); ok {
		return host, port, nil
	}
	if option.Server == "" || option.Port < 1 || option.Port > 65535 {
		return "", 0, errors.New("xhttp: missing valid server/port")
	}
	return option.Server, option.Port, nil
}

func (x *XHTTP) dialStream(ctx context.Context, destination M.Socksaddr) (net.Conn, error) {
	base, err := x.dialer.DialContext(ctx, "tcp", x.addr)
	if err != nil {
		log.Debugln("[xhttp] dial gateway %s failed (dst=%v): %v", x.addr, destination, err)
		return nil, fmt.Errorf("%s connect error: %w", x.addr, err)
	}
	return x.client.StreamConn(base, destination), nil
}

func (x *XHTTP) DialContext(ctx context.Context, metadata *C.Metadata) (_ C.Conn, err error) {
	if metadata.NetWork == C.UDP {
		if !x.option.UDPOverTCP {
			return nil, errors.New("xhttp: UDP requires udp-over-tcp")
		}
		version := uint8(x.option.UDPOverTCPVersion)
		if version == 0 {
			version = uot.Version
		}
		stream, dialErr := x.dialStream(ctx, uot.RequestDestination(version))
		if dialErr != nil {
			return nil, dialErr
		}
		destination := M.ParseSocksaddrHostPort(metadata.String(), metadata.DstPort)
		request := uot.Request{Destination: destination}
		if version == uot.LegacyVersion {
			return NewConn(uot.NewConn(stream, request), x), nil
		}
		return NewConn(uot.NewLazyConn(stream, request), x), nil
	}

	destination := M.ParseSocksaddrHostPort(metadata.String(), metadata.DstPort)
	stream, err := x.dialStream(ctx, destination)
	if err != nil {
		return nil, err
	}
	return NewConn(stream, x), nil
}

func (x *XHTTP) ListenPacketContext(ctx context.Context, metadata *C.Metadata) (_ C.PacketConn, err error) {
	if !x.option.UDPOverTCP {
		return nil, errors.New("xhttp: UDP requires udp-over-tcp")
	}
	if err = x.ResolveUDP(ctx, metadata); err != nil {
		return nil, err
	}
	version := uint8(x.option.UDPOverTCPVersion)
	if version == 0 {
		version = uot.Version
	}
	stream, err := x.dialStream(ctx, uot.RequestDestination(version))
	if err != nil {
		return nil, err
	}
	destination := M.SocksaddrFromNet(metadata.UDPAddr())
	request := uot.Request{Destination: destination}
	if version == uot.LegacyVersion {
		return NewPacketConn(N.NewThreadSafePacketConn(uot.NewConn(stream, request)), x), nil
	}
	return NewPacketConn(N.NewThreadSafePacketConn(uot.NewLazyConn(stream, request)), x), nil
}

func (x *XHTTP) SupportUOT() bool { return x.option.UDPOverTCP }

func (x *XHTTP) ProxyInfo() C.ProxyInfo {
	info := x.Base.ProxyInfo()
	info.DialerProxy = x.option.DialerProxy
	return info
}

func NewXHTTP(option XHTTPOption) (*XHTTP, error) {
	if option.Cipher != "" && option.Cipher != "aes-128-ctr" {
		return nil, fmt.Errorf("xhttp: unsupported cipher %q", option.Cipher)
	}
	gatewayHost, gatewayPort, err := resolveXHTTPGateway(option)
	if err != nil {
		return nil, err
	}
	session := option.Sess
	if session == "" {
		session = option.FakeNetTCP
	}
	if session == "" {
		session = option.FakeNet.TCP
	}
	client, err := blackstonexhttp.NewClient(option.Password, session, option.Auth)
	if err != nil {
		return nil, err
	}
	outbound := &XHTTP{
		Base: NewBase(BaseOption{
			Name:         option.Name,
			Addr:         net.JoinHostPort(gatewayHost, strconv.Itoa(gatewayPort)),
			Type:         C.XHTTP,
			ProviderName: option.ProviderName,
			UDP:          option.UDP && option.UDPOverTCP,
			TFO:          option.TFO,
			MPTCP:        option.MPTCP,
			Interface:    option.Interface,
			RoutingMark:  option.RoutingMark,
			Prefer:       option.IPVersion,
		}),
		client: client,
		option: &option,
	}
	outbound.dialer = option.NewDialer(outbound.DialOptions())
	return outbound, nil
}
