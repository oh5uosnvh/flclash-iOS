package adapter

import (
	"testing"
)

func TestParseProxyOppa(t *testing.T) {
	proxy, err := ParseProxy(map[string]any{
		"name":             "oppa-yaml",
		"type":             "oppa",
		"server":           "example.com",
		"port":             443,
		"password":         "secret-pw",
		"sni":              "example.com",
		"udp":              true,
		"preconnect":       8,
		"skip-cert-verify": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if proxy.Type().String() != "Oppa" {
		t.Fatalf("type=%v", proxy.Type())
	}
	if !proxy.SupportUDP() {
		t.Fatal("udp not enabled")
	}
	if proxy.Addr() != "example.com:443" {
		t.Fatalf("addr=%s", proxy.Addr())
	}
}
