//go:build !with_gvisor || no_tailscale

package outbound

import (
	"context"
	"fmt"
	"time"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/tailscale/ipn/ipnstate"
)

type Tailscale struct {
	*Base
}

type TailscaleOption struct {
	BasicOption
	Name       string `proxy:"name"`
	Hostname   string `proxy:"hostname,omitempty"`
	AuthKey    string `proxy:"auth-key,omitempty"`
	ControlURL string `proxy:"control-url,omitempty"`
	StateDir   string `proxy:"state-dir,omitempty"`
	Ephemeral  bool   `proxy:"ephemeral,omitempty"`
	UDP        bool   `proxy:"udp,omitempty"`

	AcceptRoutes           *bool  `proxy:"accept-routes,omitempty"`
	ExitNode               string `proxy:"exit-node,omitempty"`
	ExitNodeAllowLANAccess *bool  `proxy:"exit-node-allow-lan-access,omitempty"`
}

func NewTailscale(option TailscaleOption) (*Tailscale, error) {
	return nil, fmt.Errorf("tailscale support is disabled by \"no_tailscale\" build tag or not include \"with_gvisor\" build tag")
}

func GetTailscaleStatus(ctx context.Context, proxy C.ProxyAdapter, includeDetails bool, activate bool) (*ipnstate.Status, error) {
	return nil, fmt.Errorf("tailscale support is disabled by \"no_tailscale\" build tag or not include \"with_gvisor\" build tag")
}

func TailscaleAuthKeyConfigured(proxy C.ProxyAdapter) bool {
	return false
}

func PingTailscaleNode(ctx context.Context, proxy C.ProxyAdapter, ip string) (time.Duration, error) {
	return 0, fmt.Errorf("tailscale support is disabled by \"no_tailscale\" build tag or not include \"with_gvisor\" build tag")
}

func LogoutTailscale(ctx context.Context, proxy C.ProxyAdapter) error {
	return fmt.Errorf("tailscale support is disabled by \"no_tailscale\" build tag or not include \"with_gvisor\" build tag")
}
