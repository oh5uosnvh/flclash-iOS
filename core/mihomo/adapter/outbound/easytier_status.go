//go:build !no_easytier

package outbound

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	apiinstance "github.com/easytier/easytier/easytier-go/proto/api/instance"
	apicommon "github.com/easytier/easytier/easytier-go/proto/common"

	"github.com/metacubex/mihomo/component/easytier"
	C "github.com/metacubex/mihomo/constant"
)

func GetEasyTierStatus(ctx context.Context, proxy C.ProxyAdapter, includeDetails, activate bool) (EasyTierStatus, error) {
	switch adapter := proxy.(type) {
	case *EasyTier:
		if activate {
			if adapter.ctx.Err() != nil {
				return EasyTierStatus{}, errEasyTierClosed
			}
			if err := adapter.ensureStarted(ctx); err != nil {
				return EasyTierStatus{}, err
			}
		}
		return adapter.status(ctx, includeDetails)
	case *autoCloseProxyAdapter:
		return GetEasyTierStatus(ctx, adapter.ProxyAdapter, includeDetails, activate)
	default:
		return EasyTierStatus{}, fmt.Errorf("proxy %q is not an EasyTier outbound", proxy.Name())
	}
}

func (e *EasyTier) setStatus(state string, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctx.Err() != nil {
		state, err = "stopped", nil
	}
	e.state, e.startErr = state, err
}

func (e *EasyTier) status(ctx context.Context, includeDetails bool) (EasyTierStatus, error) {
	e.mu.Lock()
	status := EasyTierStatus{State: e.state, Network: e.option.NetworkName}
	if status.State == "" {
		status.State = "uninitialized"
	}
	if e.startErr != nil && status.State != "stopped" {
		status.Error = e.startErr.Error()
	}
	instance := e.instance
	instanceID := e.instanceID
	e.mu.Unlock()
	if !includeDetails {
		return status, nil
	}
	details := &EasyTierNetworkDetails{InstanceID: instanceID, DNSZone: e.zone, Peers: []EasyTierNodeStatus{}}
	status.Details = details
	if status.State != "connected" || instance == nil {
		return status, nil
	}
	info, err := instance.ShowNodeInfo(ctx)
	if err != nil {
		return status, err
	}
	if info != nil {
		details.Local.Hostname = info.GetHostname()
		details.Local.PeerID = info.GetPeerId()
		details.Local.InstanceID = info.GetInstId()
		details.Local.Version = info.GetVersion()
		details.Local.ProxyCIDRs = info.GetProxyCidrs()
		details.Local.Listeners = info.GetListeners()
		details.Local.FeatureFlags = easyTierFeatureFlags(info.GetFeatureFlag())
		if ip, err := easytier.ParseNodeIPv4(info.GetIpv4Addr()); err == nil {
			details.Local.IPv4 = ip.String()
		}
	}
	routes, err := instance.ListRoute(ctx)
	if err != nil {
		return status, err
	}
	peers, err := instance.ListPeer(ctx)
	if err != nil {
		return status, err
	}
	latencies := easyTierPeerLatencies(peers)
	connections := make(map[uint32][]EasyTierConnectionStatus)
	for _, peer := range peers {
		if peer != nil {
			connections[peer.GetPeerId()] = easyTierConnections(peer)
		}
	}
	for _, route := range routes {
		if route == nil || route.GetPeerId() == details.Local.PeerID {
			continue
		}
		peer := easyTierRouteNode(route, connections[route.GetPeerId()], latencies[route.GetPeerId()], e.option.LatencyFirst != nil && *e.option.LatencyFirst)
		if peer.Hostname == "" && peer.IPv4 == "" && peer.PeerID == 0 {
			continue
		}
		if peer.IPv4 != "" && peer.IPv4 == details.Local.IPv4 {
			continue
		}
		details.Peers = append(details.Peers, peer)
	}
	sort.Slice(details.Peers, func(i, j int) bool {
		a, b := details.Peers[i], details.Peers[j]
		if a.Hostname != b.Hostname {
			return a.Hostname < b.Hostname
		}
		return a.IPv4 < b.IPv4
	})
	return status, nil
}

func easyTierPeerLatencies(peers []*apiinstance.PeerInfo) map[uint32]int64 {
	latencies := make(map[uint32]int64)
	for _, peer := range peers {
		if peer == nil {
			continue
		}
		var best uint64
		for _, conn := range peer.GetConns() {
			if conn == nil || conn.GetIsClosed() {
				continue
			}
			latency := conn.GetStats().GetLatencyUs()
			if latency > 0 && (best == 0 || latency < best) {
				best = latency
			}
		}
		if best > 0 {
			milliseconds := best / 1000
			if milliseconds == 0 {
				milliseconds = 1
			}
			latencies[peer.GetPeerId()] = int64(milliseconds)
		}
	}
	return latencies
}

func easyTierConnections(peer *apiinstance.PeerInfo) []EasyTierConnectionStatus {
	result := []EasyTierConnectionStatus{}
	for _, conn := range peer.GetConns() {
		if conn == nil || conn.GetIsClosed() {
			continue
		}
		tunnel := conn.GetTunnel()
		item := EasyTierConnectionStatus{
			ID: conn.GetConnId(), Protocol: tunnel.GetTunnelType(),
			LocalEndpoint: tunnel.GetLocalAddr().GetUrl(), RemoteEndpoint: tunnel.GetRemoteAddr().GetUrl(),
		}
		if stats := conn.GetStats(); stats != nil {
			if loss := conn.GetLossRate(); loss >= 0 && loss <= 1 {
				item.LossRate = &loss
			}
			item.RxBytes = &stats.RxBytes
			item.TxBytes = &stats.TxBytes
			item.RxPackets = &stats.RxPackets
			item.TxPackets = &stats.TxPackets
			if us := stats.GetLatencyUs(); us > 0 {
				item.LatencyMS = int64(us / 1000)
				if item.LatencyMS == 0 {
					item.LatencyMS = 1
				}
			}
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func easyTierFeatureFlags(flag *apicommon.PeerFeatureFlag) map[string]bool {
	if flag == nil {
		return nil
	}
	encoded, err := json.Marshal(flag)
	if err != nil {
		return nil
	}
	var flags map[string]bool
	if err := json.Unmarshal(encoded, &flags); err != nil || len(flags) == 0 {
		return nil
	}
	return flags
}

func easyTierRouteNode(route *apiinstance.Route, connections []EasyTierConnectionStatus, latency int64, latencyFirst bool) EasyTierNodeStatus {
	peer := EasyTierNodeStatus{
		Hostname: route.GetHostname(), LatencyMS: latency,
		PeerID: route.GetPeerId(), InstanceID: route.GetInstId(), Version: route.GetVersion(),
		NextHop: route.GetNextHopPeerId(), Cost: route.GetCost(), ProxyCIDRs: route.GetProxyCidrs(),
		FeatureFlags: easyTierFeatureFlags(route.GetFeatureFlag()),
		Connections:  connections,
	}
	if route.GetPathLatency() > 0 {
		value := route.GetPathLatency()
		peer.PathLatencyMS = &value
	}
	if route.PathLatencyLatencyFirst != nil && route.GetPathLatencyLatencyFirst() >= 0 {
		value := route.GetPathLatencyLatencyFirst()
		peer.LatencyFirstPathLatencyMS = &value
	}
	if latencyFirst {
		if route.NextHopPeerIdLatencyFirst != nil {
			peer.NextHop = route.GetNextHopPeerIdLatencyFirst()
		}
		if route.CostLatencyFirst != nil {
			peer.Cost = route.GetCostLatencyFirst()
		}
	}
	if peer.NextHop != 0 {
		peer.ConnectionType = "relayed"
		if peer.NextHop == peer.PeerID {
			peer.ConnectionType = "direct"
		}
	}
	if peer.ConnectionType == "relayed" {
		peer.LatencyMS = 0
	}
	if inet := route.GetIpv4Addr(); inet != nil && inet.GetAddress() != nil {
		peer.IPv4 = easytier.IPv4FromUint32(inet.GetAddress().GetAddr()).String()
	}
	return peer
}
