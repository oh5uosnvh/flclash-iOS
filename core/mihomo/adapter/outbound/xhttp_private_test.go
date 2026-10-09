package outbound

import (
	"testing"

	C "github.com/metacubex/mihomo/constant"
)

const xhttpTestSession = "b15a77a56828e84af09ffb35d88f4acd24b18ff0d4780ef645ba2507c8362174e11249f36932af25954b49e368c9658520db9f4f5b2e14d704de46f11e14efd371981dad"
const xhttpTestPassword = "177fd09cb1d8211b9649f0216d97d7d3:3160d3d78f78b8c9c36bd2e42ce8bf1561d1f35f66bda8ccfd38383173c8951cba6e08ee3dfbd70ca919518e5e008a2e8fea"

func TestXHTTPOfficialNameGateway(t *testing.T) {
	proxy, err := NewXHTTP(XHTTPOption{
		Name:       "新加坡#N区#244-af45.iotstacker.com-20244",
		Server:     "101.116.156.114",
		Port:       40081,
		Password:   xhttpTestPassword,
		Cipher:     "aes-128-ctr",
		FakeNet:    XHTTPFakeNetOption{TCP: xhttpTestSession},
		UDP:        true,
		UDPOverTCP: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if proxy.Addr() != "af45.iotstacker.com:20244" {
		t.Fatalf("gateway=%s", proxy.Addr())
	}
	if proxy.Type() != C.XHTTP || !proxy.SupportUDP() || !proxy.SupportUOT() {
		t.Fatalf("type=%v udp=%v uot=%v", proxy.Type(), proxy.SupportUDP(), proxy.SupportUOT())
	}
}

func TestXHTTPExplicitGatewayPrecedence(t *testing.T) {
	proxy, err := NewXHTTP(XHTTPOption{
		Name:     "plain label",
		Server:   "192.0.2.10",
		Port:     443,
		Gateway:  "[2001:db8::1]:8443",
		Password: xhttpTestPassword,
		Sess:     xhttpTestSession,
	})
	if err != nil {
		t.Fatal(err)
	}
	if proxy.Addr() != "[2001:db8::1]:8443" {
		t.Fatalf("gateway=%s", proxy.Addr())
	}
}

func TestXHTTPRejectsUnsupportedCipher(t *testing.T) {
	_, err := NewXHTTP(XHTTPOption{
		Name:     "bad",
		Server:   "127.0.0.1",
		Port:     443,
		Cipher:   "aes-256-gcm",
		Password: xhttpTestPassword,
		Sess:     xhttpTestSession,
	})
	if err == nil {
		t.Fatal("unsupported cipher accepted")
	}
}
