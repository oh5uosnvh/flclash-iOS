package tunnel

import (
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/tunnel/statistic"
)

func rangeNodeTraffic(visit func(C.Proxy, *C.TrafficCounter), seen map[*C.TrafficCounter]struct{}) {
	configMux.RLock()
	defer configMux.RUnlock()
	visitProxy := func(proxy C.Proxy) {
		counter := proxy.TrafficCounter()
		if counter == nil {
			return
		}
		if _, exists := seen[counter]; exists {
			return
		}
		seen[counter] = struct{}{}
		visit(proxy, counter)
	}
	for _, proxy := range proxies {
		visitProxy(proxy)
	}
	for _, provider := range providers {
		for _, proxy := range provider.Proxies() {
			visitProxy(proxy)
		}
	}
}

func NodeTraffic() []statistic.NodeTraffic {
	result := make([]statistic.NodeTraffic, 0)
	rangeNodeTraffic(func(proxy C.Proxy, counter *C.TrafficCounter) {
		up, down := counter.Total()
		if up == 0 && down == 0 {
			return
		}
		result = append(result, statistic.NodeTraffic{
			Name:     proxy.Name(),
			Provider: proxy.ProxyInfo().ProviderName,
			Up:       up,
			Down:     down,
		})
	}, make(map[*C.TrafficCounter]struct{}))
	return result
}

func ResetNodeTraffic() {
	seen := make(map[*C.TrafficCounter]struct{})
	rangeNodeTraffic(func(_ C.Proxy, counter *C.TrafficCounter) {
		counter.Reset()
	}, seen)
	// Replaced nodes can still have active connections using their old counters.
	statistic.DefaultManager.Range(func(conn statistic.Tracker) bool {
		counter := conn.TrafficCounter()
		if _, exists := seen[counter]; !exists {
			seen[counter] = struct{}{}
			counter.Reset()
		}
		return true
	})
}
