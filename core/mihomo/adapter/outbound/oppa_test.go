package outbound

import (
	"testing"

	C "github.com/metacubex/mihomo/constant"
)

func TestOppaOptionValidation(t *testing.T) {
	if _, err := NewOppa(OppaOption{Name: "bad", Server: "127.0.0.1", Port: 443}); err == nil {
		t.Fatal("empty password accepted")
	}
	if _, err := NewOppa(OppaOption{Name: "bad", Server: "", Port: 443, Password: "pw"}); err == nil {
		t.Fatal("missing server accepted")
	}
	if _, err := NewOppa(OppaOption{Name: "bad", Server: "127.0.0.1", Port: 0, Password: "pw"}); err == nil {
		t.Fatal("invalid port accepted")
	}
}

func TestOppaBaseWiring(t *testing.T) {
	proxy, err := NewOppa(OppaOption{
		Name:     "oppa-test",
		Server:   "127.0.0.1",
		Port:     8443,
		Password: "secretpw",
		UDP:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if proxy.Addr() != "127.0.0.1:8443" {
		t.Fatalf("addr=%s", proxy.Addr())
	}
	if proxy.Type() != C.Oppa || !proxy.SupportUDP() {
		t.Fatalf("type=%v udp=%v", proxy.Type(), proxy.SupportUDP())
	}
}

func TestOppaRejectsInvalidPin(t *testing.T) {
	_, err := NewOppa(OppaOption{
		Name:      "pin",
		Server:    "127.0.0.1",
		Port:      8443,
		Password:  "pw",
		PinSHA256: "zzz-not-a-sha256-pin",
	})
	if err == nil {
		t.Fatal("invalid pin accepted")
	}
}

func TestOppaAcceptsPreConnectCompat(t *testing.T) {
	proxy, err := NewOppa(OppaOption{
		Name:       "compat",
		Server:     "example.com",
		Port:       443,
		Password:   "pw",
		ServerName: "example.com",
		PreConnect: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if proxy.Type() != C.Oppa {
		t.Fatalf("type=%v", proxy.Type())
	}
}
