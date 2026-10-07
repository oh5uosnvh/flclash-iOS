//go:build !no_easytier

package outbound

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	apiinstance "github.com/easytier/easytier/easytier-go/proto/api/instance"
	apicommon "github.com/easytier/easytier/easytier-go/proto/common"
	C "github.com/metacubex/mihomo/constant"
)

func dormantEasyTier(t *testing.T) *EasyTier {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	e := &EasyTier{
		Base: NewBase(BaseOption{Name: "status-test"}),
		ctx:  ctx, cancel: cancel, option: EasyTierOption{NetworkName: "mesh"},
		stateDir: filepath.Join(t.TempDir(), "state"),
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func waitEasyTierStatus(t *testing.T, ctx context.Context, e *EasyTier, state string) EasyTierStatus {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err := e.status(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		if status.State == state {
			return status
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for %s: %+v, %v", state, status, ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestEasyTierPassiveStatusDoesNotStart(t *testing.T) {
	e := dormantEasyTier(t)
	status, err := GetEasyTierStatus(context.Background(), e, true, false)
	if err != nil || status.State != "uninitialized" || status.Network != "mesh" || status.Details == nil {
		t.Fatalf("unexpected dormant status: %+v, %v", status, err)
	}
	if _, err := os.Stat(e.stateDir); !os.IsNotExist(err) {
		t.Fatalf("passive query created state: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	status, err = GetEasyTierStatus(context.Background(), e, false, false)
	if err != nil || status.State != "stopped" {
		t.Fatalf("closed status: %+v, %v", status, err)
	}
	if _, err := GetEasyTierStatus(context.Background(), e, false, true); err == nil {
		t.Fatal("activation after close succeeded")
	}
}

func TestEasyTierCloseRacesWithStatusAndActivation(t *testing.T) {
	e := dormantEasyTier(t)
	if err := os.WriteFile(e.stateDir, nil, 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = GetEasyTierStatus(context.Background(), e, true, true)
			_, _ = e.status(context.Background(), true)
		}()
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	status, err := e.status(context.Background(), false)
	if err != nil || status.State != "stopped" {
		t.Fatalf("final status: %+v, %v", status, err)
	}
}

func TestEasyTierLocalInstanceStatus(t *testing.T) {
	home := C.Path.HomeDir()
	C.SetHomeDir(t.TempDir())
	t.Cleanup(func() { C.SetHomeDir(home) })
	disabledP2P := true
	e, err := NewEasyTier(EasyTierOption{
		Name: "status-test", NetworkName: "local-status-test", NetworkSecret: "test-only-secret",
		Hostname: "local-test", IPv4: "10.144.0.1", StateDir: filepath.Join(C.Path.HomeDir(), "state"),
		Listeners: []string{"tcp://127.0.0.1:0"}, DisableP2P: &disabledP2P,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := os.WriteFile(e.stateDir, nil, 0600); err != nil {
		t.Fatal(err)
	}
	activationCtx, cancelActivation := context.WithCancel(ctx)
	defer cancelActivation()
	activationResult := make(chan error, 1)
	go func() {
		_, err := GetEasyTierStatus(activationCtx, e, false, true)
		activationResult <- err
	}()
	failed := waitEasyTierStatus(t, ctx, e, "error")
	if failed.Error == "" {
		t.Fatal("failed startup did not report an error")
	}
	cancelActivation()
	if err := <-activationResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("activation returned %v, want context cancellation", err)
	}
	if err := os.Remove(e.stateDir); err != nil {
		t.Fatal(err)
	}
	summary, err := GetEasyTierStatus(ctx, e, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if summary.State != "connected" || summary.Error != "" || summary.Details != nil {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	details, err := GetEasyTierStatus(ctx, e, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if details.State != "connected" || details.Details.InstanceID == "" || details.Details.Local.Hostname != "local-test" {
		t.Fatalf("unexpected details: %+v", details)
	}
	if len(details.Details.Peers) != 0 {
		t.Fatalf("unexpected peers: %+v", details.Details.Peers)
	}
	instance, err := e.currentInstance()
	if err != nil {
		t.Fatal(err)
	}
	if instance.ID() != details.Details.InstanceID {
		t.Fatal("activation replaced instance")
	}
	if err := instance.Close(ctx); err != nil {
		t.Fatal(err)
	}
	interrupted := waitEasyTierStatus(t, ctx, e, "error")
	if interrupted.Error == "" {
		t.Fatal("stopped instance did not report an error")
	}
	restarted := waitEasyTierStatus(t, ctx, e, "connected")
	if restarted.Error != "" {
		t.Fatalf("restart retained instance error: %+v", restarted)
	}
	replacement, err := e.currentInstance()
	if err != nil {
		t.Fatal(err)
	}
	if replacement == instance || replacement.ID() != instance.ID() {
		t.Fatal("restart did not replace the instance with its persisted identity")
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = GetEasyTierStatus(ctx, e, true, false) }()
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	stopped, err := GetEasyTierStatus(ctx, e, true, false)
	if err != nil || stopped.State != "stopped" {
		t.Fatalf("unexpected closed status: %+v, %v", stopped, err)
	}

}

func TestEasyTierPeerLatencies(t *testing.T) {
	peers := []*apiinstance.PeerInfo{
		nil,
		{PeerId: 1, Conns: []*apiinstance.PeerConnInfo{
			nil, {},
			{IsClosed: true, Stats: &apiinstance.PeerConnStats{LatencyUs: 1000}},
			{Stats: &apiinstance.PeerConnStats{LatencyUs: 42000}},
			{Stats: &apiinstance.PeerConnStats{LatencyUs: 23500}},
		}},
		{PeerId: 2, Conns: []*apiinstance.PeerConnInfo{{Stats: &apiinstance.PeerConnStats{LatencyUs: 400}}}},
		{PeerId: 3, Conns: []*apiinstance.PeerConnInfo{{Stats: &apiinstance.PeerConnStats{LatencyUs: 0}}}},
	}
	got := easyTierPeerLatencies(peers)
	if got[1] != 23 || got[2] != 1 || len(got) != 2 {
		t.Fatalf("unexpected latencies: %v", got)
	}
	if got[4] != 0 {
		t.Fatal("unconnected route has a latency")
	}
}

func TestEasyTierConnectionDetails(t *testing.T) {
	result := easyTierConnections(&apiinstance.PeerInfo{Conns: []*apiinstance.PeerConnInfo{
		nil, {ConnId: "closed", IsClosed: true},
		{ConnId: "b"},
		{ConnId: "a", LossRate: 0.125, Tunnel: &apicommon.TunnelInfo{
			TunnelType: "udp", LocalAddr: &apicommon.Url{Url: "udp://127.0.0.1:1234"},
			RemoteAddr: &apicommon.Url{Url: "udp://192.0.2.1:5678"},
		}, Stats: &apiinstance.PeerConnStats{LatencyUs: 23456, RxBytes: 1024, TxBytes: 2048, RxPackets: 10, TxPackets: 20}},
	}})
	if len(result) != 2 || result[0].ID != "a" || result[1].ID != "b" {
		t.Fatalf("unexpected connections: %+v", result)
	}
	first := result[0]
	if first.Protocol != "udp" || first.RemoteEndpoint != "udp://192.0.2.1:5678" || first.LatencyMS != 23 {
		t.Fatalf("unexpected endpoint: %+v", first)
	}
	if *first.RxBytes != 1024 || *first.TxPackets != 20 || *first.LossRate != 0.125 {
		t.Fatalf("unexpected stats: %+v", first)
	}
	if result[1].RxBytes != nil || result[1].LossRate != nil {
		t.Fatal("missing stats reported as measurements")
	}
}

func TestEasyTierRouteDetailsFollowRoutingPreference(t *testing.T) {
	next := uint32(7)
	cost := int32(2)
	route := &apiinstance.Route{PeerId: 42, NextHopPeerId: 42, Cost: 1, Hostname: "peer", Version: "2.5",
		ProxyCidrs: []string{"192.168.2.0/24"}, NextHopPeerIdLatencyFirst: &next, CostLatencyFirst: &cost,
		FeatureFlag: &apicommon.PeerFeatureFlag{IsPublicServer: true, AvoidRelayData: true}}
	direct := easyTierRouteNode(route, nil, 23, false)
	if direct.ConnectionType != "direct" || direct.NextHop != 42 || direct.Cost != 1 || direct.Version != "2.5" ||
		!direct.FeatureFlags["is_public_server"] || !direct.FeatureFlags["avoid_relay_data"] || len(direct.FeatureFlags) != 2 {
		t.Fatalf("unexpected direct route: %+v", direct)
	}
	relayed := easyTierRouteNode(route, nil, 23, true)
	if relayed.ConnectionType != "relayed" || relayed.NextHop != 7 || relayed.Cost != 2 || relayed.LatencyMS != 0 || len(relayed.ProxyCIDRs) != 1 {
		t.Fatalf("unexpected relayed route: %+v", relayed)
	}
	unknown := easyTierRouteNode(&apiinstance.Route{PeerId: 42}, nil, 0, false)
	if unknown.ConnectionType != "" || unknown.FeatureFlags != nil {
		t.Fatal("missing next hop presented as connected")
	}
}

func TestEasyTierPathLatencies(t *testing.T) {
	latencyFirst := int32(25)
	route := &apiinstance.Route{PeerId: 42, NextHopPeerId: 7, PathLatency: 40, PathLatencyLatencyFirst: &latencyFirst}
	node := easyTierRouteNode(route, nil, 23, false)
	if *node.PathLatencyMS != 40 || *node.LatencyFirstPathLatencyMS != 25 || node.LatencyMS != 0 {
		t.Fatalf("unexpected path latencies: %+v", node)
	}
	missing := easyTierRouteNode(&apiinstance.Route{}, nil, 0, false)
	if missing.PathLatencyMS != nil || missing.LatencyFirstPathLatencyMS != nil {
		t.Fatal("missing path measurements exposed")
	}
}
