package outbound

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"

	"github.com/metacubex/mihomo/component/ca"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/transport/oppa"

	M "github.com/metacubex/sing/common/metadata"
	tls "github.com/metacubex/tls"
)

// Oppa is NekoBoxYF's self-developed "oppa" protocol: a bare-Trojan-style
// tunnel over standard TLS (no uTLS, no multiplexing). The wire is simply
// [password][command][SOCKS address] followed by a raw stream (TCP) or the
// oppa UDP framing (UDP). Port of libcore/protocol/oppa, additive.
type Oppa struct {
	*Base
	option    *OppaOption
	tlsConfig *tls.Config
}

type OppaOption struct {
	BasicOption
	Name              string   `proxy:"name"`
	Server            string   `proxy:"server"`
	Port              int      `proxy:"port"`
	Password          string   `proxy:"password"`
	ServerName        string   `proxy:"sni,omitempty"`
	SkipCertVerify    bool     `proxy:"skip-cert-verify,omitempty"`
	ALPN              []string `proxy:"alpn,omitempty"`
	PinSHA256         string   `proxy:"pin-sha256,omitempty"`
	ClientFingerprint string   `proxy:"client-fingerprint,omitempty"`
	Fingerprint       string   `proxy:"fingerprint,omitempty"`
	Certificate       string   `proxy:"certificate,omitempty"`
	PrivateKey        string   `proxy:"private-key,omitempty"`
	// PreConnect mirrors the reference link parameter (0-64, default 8);
	// accepted for subscription compatibility, not used on the wire.
	PreConnect int  `proxy:"preconnect,omitempty"`
	UDP        bool `proxy:"udp,omitempty"`
}

func (o *Oppa) dialTLS(ctx context.Context) (net.Conn, error) {
	conn, err := o.dialer.DialContext(ctx, "tcp", o.addr)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %w", o.addr, err)
	}
	tlsConn := tls.Client(conn, o.tlsConfig)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("%s tls handshake error: %w", o.addr, err)
	}
	return tlsConn, nil
}

func (o *Oppa) DialContext(ctx context.Context, metadata *C.Metadata) (_ C.Conn, err error) {
	if metadata.NetWork == C.UDP {
		return nil, errors.New("oppa: UDP is delivered via ListenPacketContext")
	}

	destination := M.ParseSocksaddrHostPort(metadata.Host, metadata.DstPort)
	conn, err := o.dialTLS(ctx)
	if err != nil {
		return nil, err
	}
	header, err := oppa.BuildStreamHeader(o.option.Password, destination)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if _, err = conn.Write(header); err != nil {
		conn.Close()
		return nil, fmt.Errorf("oppa: write header error: %w", err)
	}
	return NewConn(conn, o), nil
}

func (o *Oppa) ListenPacketContext(ctx context.Context, metadata *C.Metadata) (_ C.PacketConn, err error) {
	if !o.option.UDP {
		return nil, errors.New("oppa: UDP disabled")
	}
	if err = o.ResolveUDP(ctx, metadata); err != nil {
		return nil, err
	}
	conn, err := o.dialTLS(ctx)
	if err != nil {
		return nil, err
	}
	header, err := oppa.BuildSessionHeader(o.option.Password, oppa.CommandUDP)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if _, err = conn.Write(header); err != nil {
		conn.Close()
		return nil, fmt.Errorf("oppa: write udp header error: %w", err)
	}
	return NewPacketConn(oppa.NewPacketConn(conn), o), nil
}

func (o *Oppa) ProxyInfo() C.ProxyInfo {
	info := o.Base.ProxyInfo()
	info.DialerProxy = o.option.DialerProxy
	return info
}

func NewOppa(option OppaOption) (*Oppa, error) {
	if len(option.Password) == 0 || len(option.Password) > oppa.MaxPasswordLength {
		return nil, fmt.Errorf("oppa: password length must be between 1 and %d bytes", oppa.MaxPasswordLength)
	}
	if option.Server == "" || option.Port < 1 || option.Port > 65535 {
		return nil, errors.New("oppa: missing valid server/port")
	}

	serverName := option.ServerName
	if serverName == "" {
		serverName = option.Server
	}

	tlsConfig := &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: option.SkipCertVerify,
		NextProtos:         option.ALPN,
		MinVersion:         tls.VersionTLS12,
	}

	var pin []byte
	if option.PinSHA256 != "" {
		var err error
		if pin, err = oppa.DecodePinHash(option.PinSHA256); err != nil {
			return nil, err
		}
	}

	switch {
	case pin != nil:
		// Certificate pinning: verify the chained hash only (implies skipping
		// the standard verification path, like the reference client).
		tlsConfig.InsecureSkipVerify = true
		tlsConfig.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if !bytes.Equal(pin, oppa.CertChainHash(rawCerts)) {
				return errors.New("oppa: peer certificate hash mismatch")
			}
			return nil
		}
	case !option.SkipCertVerify && isIPName(serverName):
		// RFC 6066: an IP address cannot be used as SNI. Subscription links
		// sometimes ship sni=<relay IP>; the resulting Let's Encrypt
		// certificates only carry DNS SANs and strict verification always
		// fails. Fall back to CA-chain-only verification (chain to a trusted
		// root + time validity), skipping hostname matching.
		tlsConfig.InsecureSkipVerify = true
		tlsConfig.VerifyConnection = func(state tls.ConnectionState) error {
			return verifyChainOnly(&state)
		}
	}

	if option.ClientFingerprint != "" {
		var err error
		tlsConfig, err = ca.GetTLSConfig(ca.Option{
			TLSConfig:   tlsConfig,
			Fingerprint: option.ClientFingerprint,
			Certificate: option.Certificate,
			PrivateKey:  option.PrivateKey,
		})
		if err != nil {
			return nil, err
		}
	}

	outbound := &Oppa{
		Base: NewBase(BaseOption{
			Name:         option.Name,
			Addr:         net.JoinHostPort(option.Server, strconv.Itoa(option.Port)),
			Type:         C.Oppa,
			ProviderName: option.ProviderName,
			UDP:          option.UDP,
			TFO:          option.TFO,
			MPTCP:        option.MPTCP,
			Interface:    option.Interface,
			RoutingMark:  option.RoutingMark,
			Prefer:       option.IPVersion,
		}),
		option:    &option,
		tlsConfig: tlsConfig,
	}
	outbound.dialer = option.NewDialer(outbound.DialOptions())
	return outbound, nil
}

func isIPName(name string) bool {
	_, err := netip.ParseAddr(name)
	return err == nil
}

func verifyChainOnly(state *tls.ConnectionState) error {
	if len(state.PeerCertificates) == 0 {
		return errors.New("oppa: peer presented no certificate")
	}
	verifyOptions := x509.VerifyOptions{
		Intermediates: x509.NewCertPool(),
		CurrentTime:   time.Now(),
	}
	for _, cert := range state.PeerCertificates[1:] {
		verifyOptions.Intermediates.AddCert(cert)
	}
	_, err := state.PeerCertificates[0].Verify(verifyOptions)
	return err
}
