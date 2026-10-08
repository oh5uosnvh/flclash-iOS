package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/metacubex/mihomo/common/convert"
	N "github.com/metacubex/mihomo/common/net"
	"github.com/metacubex/mihomo/component/ca"
	"github.com/metacubex/mihomo/component/ech"
	tlsC "github.com/metacubex/mihomo/component/tls"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/transport/gun"
	tuicCommon "github.com/metacubex/mihomo/transport/tuic/common"
	"github.com/metacubex/mihomo/transport/vmess"
	"github.com/metacubex/mihomo/transport/x365"
	"github.com/metacubex/mihomo/transport/xhttp"

	mihomoHttp "github.com/metacubex/http"
	"github.com/metacubex/quic-go"
	M "github.com/metacubex/sing/common/metadata"
	"github.com/metacubex/tls"
)

// The official 1.0.63 client uses Chrome/120 at the HTTP layer; the
// server rejects mihomo's newer randomized UA with 403 before X365 is read.
const efanXHTTPUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

func prepareX365XHTTPHeaders(headers map[string]string) map[string]string {
	prepared := make(map[string]string, len(headers)+1)
	hasUA := false
	for key, value := range headers {
		prepared[key] = value
		if strings.EqualFold(key, "User-Agent") {
			hasUA = true
		}
	}
	if !hasUA {
		prepared["User-Agent"] = efanXHTTPUserAgent
	}
	return prepared
}

// X365 is a private VLESS-derived protocol used by 饿饭/365VPN. Its wire header
// is "X365"+ver+cmd+uuid+addr and it is carried over the same transports as
// VLESS (typically xhttp stream-one + REALITY + uTLS). We reuse VLESS's transport
// plumbing verbatim and only swap the inner handshake client.
type X365 struct {
	*Base
	client *x365.Client
	option *X365Option

	// for gun mux
	gunClient *gun.Client
	// for xhttp
	xhttpClient *xhttp.Client

	realityConfig *tlsC.RealityConfig
	echConfig     *ech.Config

	// lazy transport init: gun/xhttp clients are built on first real dial
	// (see ensureLazy) so a 100+-node subscription does not pay their cost
	// while the NE is near its memory budget.
	initOnce sync.Once
	initErr  error
}

type X365Option struct {
	BasicOption
	Name              string            `proxy:"name"`
	Server            string            `proxy:"server"`
	Port              int               `proxy:"port"`
	UUID              string            `proxy:"uuid"`
	TLS               bool              `proxy:"tls,omitempty"`
	ALPN              []string          `proxy:"alpn,omitempty"`
	UDP               bool              `proxy:"udp,omitempty"`
	Network           string            `proxy:"network,omitempty"`
	ECHOpts           ECHOptions        `proxy:"ech-opts,omitempty"`
	RealityOpts       RealityOptions    `proxy:"reality-opts,omitempty"`
	HTTPOpts          HTTPOptions       `proxy:"http-opts,omitempty"`
	HTTP2Opts         HTTP2Options      `proxy:"h2-opts,omitempty"`
	GrpcOpts          GrpcOptions       `proxy:"grpc-opts,omitempty"`
	WSOpts            WSOptions         `proxy:"ws-opts,omitempty"`
	XHTTPOpts         XHTTPOptions      `proxy:"xhttp-opts,omitempty"`
	WSHeaders         map[string]string `proxy:"ws-headers,omitempty"`
	SkipCertVerify    bool              `proxy:"skip-cert-verify,omitempty"`
	Fingerprint       string            `proxy:"fingerprint,omitempty"`
	Certificate       string            `proxy:"certificate,omitempty"`
	PrivateKey        string            `proxy:"private-key,omitempty"`
	ServerName        string            `proxy:"servername,omitempty"`
	ClientFingerprint string            `proxy:"client-fingerprint,omitempty"`
}

func (v *X365) StreamConnContext(ctx context.Context, c net.Conn, metadata *C.Metadata) (_ net.Conn, err error) {
	switch v.option.Network {
	case "ws":
		host, port, _ := net.SplitHostPort(v.addr)
		wsOpts := &vmess.WebsocketConfig{
			Host:                     host,
			Port:                     port,
			Path:                     v.option.WSOpts.Path,
			MaxEarlyData:             v.option.WSOpts.MaxEarlyData,
			EarlyDataHeaderName:      v.option.WSOpts.EarlyDataHeaderName,
			V2rayHttpUpgrade:         v.option.WSOpts.V2rayHttpUpgrade,
			V2rayHttpUpgradeFastOpen: v.option.WSOpts.V2rayHttpUpgradeFastOpen,
			ClientFingerprint:        v.option.ClientFingerprint,
			ECHConfig:                v.echConfig,
			Headers:                  mihomoHttp.Header{},
		}

		if len(v.option.WSOpts.Headers) != 0 {
			for key, value := range v.option.WSOpts.Headers {
				wsOpts.Headers.Add(key, value)
			}
		}
		if v.option.TLS {
			wsOpts.TLS = true
			wsOpts.TLSConfig, err = ca.GetTLSConfig(ca.Option{
				TLSConfig: &tls.Config{
					ServerName:         host,
					InsecureSkipVerify: v.option.SkipCertVerify,
					NextProtos:         []string{"http/1.1"},
				},
				Fingerprint: v.option.Fingerprint,
				Certificate: v.option.Certificate,
				PrivateKey:  v.option.PrivateKey,
			})
			if err != nil {
				return nil, err
			}

			if v.option.ServerName != "" {
				wsOpts.TLSConfig.ServerName = v.option.ServerName
			} else if host := wsOpts.Headers.Get("Host"); host != "" {
				wsOpts.TLSConfig.ServerName = host
			}
		} else {
			if host := wsOpts.Headers.Get("Host"); host == "" {
				wsOpts.Headers.Set("Host", convert.RandHost())
				convert.SetUserAgent(wsOpts.Headers)
			}
		}
		c, err = vmess.StreamWebsocketConn(ctx, c, wsOpts)
	case "http":
		c, err = v.streamTLSConn(ctx, c, false)
		if err != nil {
			return nil, err
		}

		host, _, _ := net.SplitHostPort(v.addr)
		httpOpts := &vmess.HTTPConfig{
			Host:    host,
			Method:  v.option.HTTPOpts.Method,
			Path:    v.option.HTTPOpts.Path,
			Headers: v.option.HTTPOpts.Headers,
		}

		c = vmess.StreamHTTPConn(c, httpOpts)
	case "h2":
		c, err = v.streamTLSConn(ctx, c, true)
		if err != nil {
			return nil, err
		}

		h2Opts := &vmess.H2Config{
			Hosts: v.option.HTTP2Opts.Host,
			Path:  v.option.HTTP2Opts.Path,
		}

		c, err = vmess.StreamH2Conn(ctx, c, h2Opts)
	case "grpc":
		break // already handle in dialContext
	case "xhttp":
		break // already handle in dialContext
	default:
		// default tcp network
		c, err = v.streamTLSConn(ctx, c, false)
	}

	if err != nil {
		return nil, err
	}

	return v.streamConnContext(ctx, c, metadata)
}

func (v *X365) streamConnContext(ctx context.Context, c net.Conn, metadata *C.Metadata) (conn net.Conn, err error) {
	if ctx.Done() != nil {
		done := N.SetupContextForConn(ctx, c)
		defer done(&err)
	}
	destination := M.ParseSocksaddrHostPort(metadata.String(), metadata.DstPort)
	conn = v.client.StreamConn(c, destination)
	return
}

func (v *X365) streamTLSConn(ctx context.Context, conn net.Conn, isH2 bool) (net.Conn, error) {
	if v.option.TLS {
		host, _, _ := net.SplitHostPort(v.addr)

		tlsOpts := vmess.TLSConfig{
			Host:              host,
			SkipCertVerify:    v.option.SkipCertVerify,
			FingerPrint:       v.option.Fingerprint,
			Certificate:       v.option.Certificate,
			PrivateKey:        v.option.PrivateKey,
			ClientFingerprint: v.option.ClientFingerprint,
			ECH:               v.echConfig,
			Reality:           v.realityConfig,
			NextProtos:        v.option.ALPN,
		}

		if isH2 {
			tlsOpts.NextProtos = []string{"h2"}
		}

		if v.option.ServerName != "" {
			tlsOpts.Host = v.option.ServerName
		}

		return vmess.StreamTLSConn(ctx, conn, &tlsOpts)
	}

	return conn, nil
}

func (v *X365) dialContext(ctx context.Context) (c net.Conn, err error) {
	if err = v.ensureLazy(); err != nil {
		return nil, err
	}
	switch v.option.Network {
	case "grpc": // gun transport
		return v.gunClient.Dial()
	case "xhttp":
		return v.xhttpClient.Dial(ctx)
	default:
	}
	return v.dialer.DialContext(ctx, "tcp", v.addr)
}

// DialContext implements C.ProxyAdapter
func (v *X365) DialContext(ctx context.Context, metadata *C.Metadata) (_ C.Conn, err error) {
	c, err := v.dialContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %s", v.addr, err.Error())
	}
	defer func(c net.Conn) {
		safeConnClose(c, err)
	}(c)

	c, err = v.StreamConnContext(ctx, c, metadata)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %s", v.addr, err.Error())
	}
	return NewConn(c, v), err
}

// ListenPacketContext implements C.ProxyAdapter
func (v *X365) ListenPacketContext(ctx context.Context, metadata *C.Metadata) (_ C.PacketConn, err error) {
	if err = v.ResolveUDP(ctx, metadata); err != nil {
		return nil, err
	}

	c, err := v.dialContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %s", v.addr, err.Error())
	}
	defer func(c net.Conn) {
		safeConnClose(c, err)
	}(c)

	if v.option.Network == "grpc" || v.option.Network == "xhttp" {
		// already handled in dialContext
	} else {
		c, err = v.streamTLSConn(ctx, c, false)
		if err != nil {
			return nil, fmt.Errorf("%s connect error: %s", v.addr, err.Error())
		}
	}

	destination := M.ParseSocksaddrHostPort(metadata.String(), metadata.DstPort)
	pc := v.client.PacketConn(c, destination)
	return NewPacketConn(N.NewThreadSafePacketConn(pc), v), nil
}

// SupportUOT implements C.ProxyAdapter
func (v *X365) SupportUOT() bool {
	return true
}

// ProxyInfo implements C.ProxyAdapter
func (v *X365) ProxyInfo() C.ProxyInfo {
	info := v.Base.ProxyInfo()
	info.DialerProxy = v.option.DialerProxy
	return info
}

// Close implements C.ProxyAdapter
func (v *X365) Close() error {
	var errs []error
	if v.gunClient != nil {
		if err := v.gunClient.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if v.xhttpClient != nil {
		if err := v.xhttpClient.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// FastProbe implements lightweight health-check probing for (*Proxy).URLTest:
// bare TCP dial to the node address. The full-session path (REALITY handshake
// + xhttp session + x365 handshake) is far too expensive to run per node per
// health-check round inside the iOS NetworkExtension memory budget.
func (v *X365) FastProbe(ctx context.Context) error {
	conn, err := v.dialer.DialContext(ctx, "tcp", v.addr)
	if err != nil {
		return err
	}
	return conn.Close()
}

func NewX365(option X365Option) (*X365, error) {
	client, err := x365.NewClient(option.UUID)
	if err != nil {
		return nil, err
	}

	v := &X365{
		Base: NewBase(BaseOption{
			Name:         option.Name,
			Addr:         net.JoinHostPort(option.Server, strconv.Itoa(option.Port)),
			Type:         C.X365,
			ProviderName: option.ProviderName,
			UDP:          option.UDP,
			TFO:          option.TFO,
			MPTCP:        option.MPTCP,
			Interface:    option.Interface,
			RoutingMark:  option.RoutingMark,
			Prefer:       option.IPVersion,
		}),
		client: client,
		option: &option,
	}
	v.dialer = option.NewDialer(v.DialOptions())

	v.realityConfig, err = v.option.RealityOpts.Parse()
	if err != nil {
		return nil, err
	}

	v.echConfig, err = v.option.ECHOpts.Parse()
	if err != nil {
		return nil, err
	}

	switch option.Network {
	case "h2":
		if len(option.HTTP2Opts.Host) == 0 {
			option.HTTP2Opts.Host = append(option.HTTP2Opts.Host, "www.example.com")
		}
	}

	return v, nil
}

// ensureLazy builds the per-network heavy transports (gun/xhttp clients and
// their pools) on first real dial instead of at construction time. A
// 100+-node subscription constructs every outbound while the NE is already
// close to its jetsam budget; deferring these keeps the load-phase peak off
// the line. The wire protocol is untouched - only the construction timing
// moves, mirroring the working reference build's lazyInit.
func (v *X365) ensureLazy() error {
	v.initOnce.Do(func() {
		option := *v.option
		switch option.Network {
		case "grpc":
			dialFn := func(ctx context.Context, network, addr string) (net.Conn, error) {
				c, err := v.dialer.DialContext(ctx, "tcp", v.addr)
				if err != nil {
					return nil, fmt.Errorf("%s connect error: %s", v.addr, err.Error())
				}
				return c, nil
			}

			gunConfig := &gun.Config{
				ServiceName:  option.GrpcOpts.GrpcServiceName,
				UserAgent:    option.GrpcOpts.GrpcUserAgent,
				Host:         option.ServerName,
				PingInterval: option.GrpcOpts.PingInterval,
			}
			if option.ServerName == "" {
				gunConfig.Host = v.addr
			}
			var tlsConfig *vmess.TLSConfig
			if option.TLS {
				tlsConfig = &vmess.TLSConfig{
					Host:              option.ServerName,
					SkipCertVerify:    option.SkipCertVerify,
					FingerPrint:       option.Fingerprint,
					Certificate:       option.Certificate,
					PrivateKey:        option.PrivateKey,
					ClientFingerprint: option.ClientFingerprint,
					NextProtos:        []string{"h2"},
					ECH:               v.echConfig,
					Reality:           v.realityConfig,
				}
				if option.ServerName == "" {
					host, _, _ := net.SplitHostPort(v.addr)
					tlsConfig.Host = host
				}
			}

			v.gunClient = gun.NewClient(
				func() *gun.Transport {
					return gun.NewTransport(dialFn, tlsConfig, gunConfig)
				},
				option.GrpcOpts.MaxConnections,
				option.GrpcOpts.MinStreams,
				option.GrpcOpts.MaxStreams,
			)
		case "xhttp":
			requestHost := v.option.XHTTPOpts.Host
			if requestHost == "" {
				if v.option.ServerName != "" {
					requestHost = v.option.ServerName
				} else {
					requestHost = v.option.Server
				}
			}

			var hKeepAlivePeriod time.Duration

			var reuseCfg *xhttp.ReuseConfig
			if option.XHTTPOpts.ReuseSettings != nil {
				reuseCfg = &xhttp.ReuseConfig{
					MaxConcurrency:   option.XHTTPOpts.ReuseSettings.MaxConcurrency,
					MaxConnections:   option.XHTTPOpts.ReuseSettings.MaxConnections,
					CMaxReuseTimes:   option.XHTTPOpts.ReuseSettings.CMaxReuseTimes,
					HMaxRequestTimes: option.XHTTPOpts.ReuseSettings.HMaxRequestTimes,
					HMaxReusableSecs: option.XHTTPOpts.ReuseSettings.HMaxReusableSecs,
				}
				hKeepAlivePeriod = time.Duration(option.XHTTPOpts.ReuseSettings.HKeepAlivePeriod) * time.Second
			}

			cfg := &xhttp.Config{
				Host:                 requestHost,
				Path:                 v.option.XHTTPOpts.Path,
				Mode:                 v.option.XHTTPOpts.Mode,
				Headers:              prepareX365XHTTPHeaders(v.option.XHTTPOpts.Headers),
				NoGRPCHeader:         v.option.XHTTPOpts.NoGRPCHeader,
				XPaddingBytes:        v.option.XHTTPOpts.XPaddingBytes,
				XPaddingObfsMode:     v.option.XHTTPOpts.XPaddingObfsMode,
				XPaddingKey:          v.option.XHTTPOpts.XPaddingKey,
				XPaddingHeader:       v.option.XHTTPOpts.XPaddingHeader,
				XPaddingPlacement:    v.option.XHTTPOpts.XPaddingPlacement,
				XPaddingMethod:       v.option.XHTTPOpts.XPaddingMethod,
				UplinkHTTPMethod:     v.option.XHTTPOpts.UplinkHTTPMethod,
				SessionPlacement:     v.option.XHTTPOpts.SessionPlacement,
				SessionKey:           v.option.XHTTPOpts.SessionKey,
				SeqPlacement:         v.option.XHTTPOpts.SeqPlacement,
				SeqKey:               v.option.XHTTPOpts.SeqKey,
				UplinkDataPlacement:  v.option.XHTTPOpts.UplinkDataPlacement,
				UplinkDataKey:        v.option.XHTTPOpts.UplinkDataKey,
				UplinkChunkSize:      v.option.XHTTPOpts.UplinkChunkSize,
				ScMaxEachPostBytes:   v.option.XHTTPOpts.ScMaxEachPostBytes,
				ScMinPostsIntervalMs: v.option.XHTTPOpts.ScMinPostsIntervalMs,
				ReuseConfig:          reuseCfg,
			}

			makeTransport := func() mihomoHttp.RoundTripper {
				return xhttp.NewTransport(
					func(ctx context.Context) (net.Conn, error) {
						return v.dialer.DialContext(ctx, "tcp", v.addr)
					},
					func(ctx context.Context, raw net.Conn, isH2 bool) (net.Conn, error) {
						return v.streamTLSConn(ctx, raw, isH2)
					},
					func(ctx context.Context, cfg *quic.Config) (*quic.Conn, error) {
						host, _, _ := net.SplitHostPort(v.addr)
						tlsOpts := &vmess.TLSConfig{
							Host:              host,
							SkipCertVerify:    v.option.SkipCertVerify,
							FingerPrint:       v.option.Fingerprint,
							Certificate:       v.option.Certificate,
							PrivateKey:        v.option.PrivateKey,
							ClientFingerprint: v.option.ClientFingerprint,
							ECH:               v.echConfig,
							Reality:           v.realityConfig,
							NextProtos:        []string{"h3"},
						}
						if v.option.ServerName != "" {
							tlsOpts.Host = v.option.ServerName
						}
						if !v.option.TLS {
							return nil, errors.New("xhttp HTTP/3 requires TLS")
						}
						if v.realityConfig != nil {
							return nil, errors.New("xhttp HTTP/3 does not support reality")
						}
						tlsConfig, err := tlsOpts.ToStdConfig()
						if err != nil {
							return nil, err
						}

						err = v.echConfig.ClientHandle(ctx, tlsConfig)
						if err != nil {
							return nil, err
						}
						_, quicConn, err := tuicCommon.DialQuic(ctx, v.addr, v.DialOptions(), v.dialer, tlsConfig, cfg, tuicCommon.DialQuicOption{Early: true})
						if err != nil {
							return nil, err
						}
						return quicConn, nil
					},
					v.option.ALPN,
					hKeepAlivePeriod,
				)
			}

			v.xhttpClient, v.initErr = xhttp.NewClient(cfg, makeTransport, nil, v.realityConfig != nil)
		}
	})
	return v.initErr
}
