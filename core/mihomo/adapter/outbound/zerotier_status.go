//go:build !no_zerotier

package outbound

import (
	"cmp"
	"fmt"
	"time"

	C "github.com/metacubex/mihomo/constant"
	"golang.org/x/exp/slices"
)

func GetZeroTierStatus(proxy C.ProxyAdapter, includeDetails bool, activate bool) (ZeroTierStatus, error) {
	switch adapter := proxy.(type) {
	case *ZeroTier:
		if activate {
			if err := adapter.start(); err != nil {
				return ZeroTierStatus{}, err
			}
		}
		return adapter.status(includeDetails), nil
	case *autoCloseProxyAdapter:
		return GetZeroTierStatus(adapter.ProxyAdapter, includeDetails, activate)
	default:
		return ZeroTierStatus{}, fmt.Errorf("proxy %q is not a ZeroTier outbound", proxy.Name())
	}
}

func (z *ZeroTier) status(includeDetails bool) ZeroTierStatus {
	z.operationMu.RLock()
	defer z.operationMu.RUnlock()

	status := ZeroTierStatus{
		NetworkID: fmt.Sprintf("%016x", z.networkID),
	}
	z.stateMu.RLock()
	runtime := z.runtime
	config := z.config
	status.AuthURL = z.authURL
	if z.networkErr != nil {
		status.Error = z.networkErr.Error()
	}
	z.stateMu.RUnlock()
	if runtime == nil {
		return status
	}

	status.Node = runtime.nodeAddress.String()
	status.Online = runtime.node.Online()
	if network, ok := runtime.node.Network(z.networkID); ok {
		status.Status = network.Status.String()
		if config.NetworkID == 0 {
			config = network.Config
		}
	} else {
		status.Status = "requesting-configuration"
	}
	status.Network = config.Name
	if !includeDetails {
		return status
	}
	status.MTU = z.effectiveMTU(config)
	for _, address := range config.Assigned {
		status.Addresses = append(status.Addresses, address.String())
	}
	for _, route := range config.Routes {
		value := route.Target.String()
		if route.Via.IsValid() {
			value += " via " + route.Via.String()
		}
		status.Routes = append(status.Routes, value)
	}
	for _, server := range config.DNSServers {
		status.DNS = append(status.DNS, server.String())
	}
	for _, peer := range runtime.node.Peers() {
		peerStatus := ZeroTierPeerStatus{
			Address: peer.Address.String(),
			Role:    peer.Role.String(),
			Version: zeroTierPeerVersion(peer.VersionMajor, peer.VersionMinor, peer.VersionRevision),
		}
		var latency time.Duration
		for _, path := range peer.Paths {
			if path.Endpoint.IsValid() {
				peerStatus.Endpoints = append(peerStatus.Endpoints, path.Endpoint.String())
			}
			if path.Active || path.Preferred {
				peerStatus.Direct = true
				if latency == 0 || path.Latency < latency {
					latency = path.Latency
				}
			}
		}
		peerStatus.LatencyMS = latency.Milliseconds()
		status.Peers = append(status.Peers, peerStatus)
	}
	slices.SortFunc(status.Peers, func(a, b ZeroTierPeerStatus) int {
		if a.Direct != b.Direct {
			if a.Direct {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Address, b.Address)
	})
	return status
}

func zeroTierPeerVersion(major, minor, revision int) string {
	if major < 0 || minor < 0 || revision < 0 {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, revision)
}
