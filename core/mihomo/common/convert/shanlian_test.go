package convert_test

import (
	"testing"

	"github.com/metacubex/mihomo/adapter"
	. "github.com/metacubex/mihomo/common/convert"

	"github.com/stretchr/testify/assert"
)

func TestConvertsV2RayAnyTLSShanlianLink(t *testing.T) {
	const password = "00112233445566778899aabbccddeeff102132435465768798a9bacbdcedfe0f#SL"
	uri := "anytls://" + password[:64] + "%23SL@209.248.43.20:6573/?insecure=1&sni=p3.byteimg.com&fp=chrome#shanlian"

	proxies, err := ConvertsV2Ray([]byte(uri))
	assert.NoError(t, err)
	if !assert.Len(t, proxies, 1) {
		return
	}

	proxy := proxies[0]
	assert.Equal(t, "anytls", proxy["type"])
	assert.Equal(t, password, proxy["password"])
	assert.Equal(t, "p3.byteimg.com", proxy["sni"])
	assert.Equal(t, "chrome", proxy["client-fingerprint"])
	assert.Equal(t, true, proxy["skip-cert-verify"])

	_, err = adapter.ParseProxy(proxy)
	assert.NoError(t, err)
}
