package outbound

import (
	"strings"
	"testing"

	"github.com/metacubex/mihomo/transport/xhttp"
)

func TestX365OfficialUserAgentOnlyOnXHTTP(t *testing.T) {
	original := map[string]string{"X-Custom": "retained"}
	got := prepareX365XHTTPHeaders(original)
	if got["User-Agent"] != efanXHTTPUserAgent || got["X-Custom"] != "retained" {
		t.Fatalf("unexpected XHTTP headers: %v", got)
	}
	if _, present := original["User-Agent"]; present {
		t.Fatal("mutated input headers")
	}
	cfg := &xhttp.Config{Headers: got}
	ua := cfg.GetRequestHeader().Get("User-Agent")
	if ua != efanXHTTPUserAgent {
		t.Fatalf("XHTTP changed official UA to %q", ua)
	}
	if !strings.Contains(ua, "Chrome/120.0.0.0") {
		t.Fatalf("wrong version: %q", ua)
	}
}

func TestX365ExplicitUAStillWins(t *testing.T) {
	for _, key := range []string{"User-Agent", "user-agent", "USER-AGENT"} {
		got := prepareX365XHTTPHeaders(map[string]string{key: "custom"})
		if len(got) != 1 || got[key] != "custom" {
			t.Fatalf("custom UA was replaced: %v", got)
		}
	}
}
