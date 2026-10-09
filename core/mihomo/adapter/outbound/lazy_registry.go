package outbound

import (
	"sync"
	"time"
)

// Lazy traffic transports (gun/xhttp clients with their pools) are built on
// first dial and kept for reuse. On a 100+-node subscription a full group
// test materializes every client, so the NE memory probe evicts the ones
// idle past lazyIdleTTL; the next dial transparently rebuilds them.
const lazyIdleTTL = 3 * time.Minute

var (
	lazyRegistryMu sync.Mutex
	lazyRegistry   = map[*X365]struct{}{}
)

func registerLazy(v *X365) {
	lazyRegistryMu.Lock()
	lazyRegistry[v] = struct{}{}
	lazyRegistryMu.Unlock()
}

func unregisterLazy(v *X365) {
	lazyRegistryMu.Lock()
	delete(lazyRegistry, v)
	lazyRegistryMu.Unlock()
}

// evictIdleLazyClients closes and drops the lazy transports of outbounds
// that have not dialed for lazyIdleTTL. Called from the NE memory probe.
// Returns the number of clients evicted.
func EvictIdleLazyClients() int {
	return evictIdleLazyClients()
}

func evictIdleLazyClients() int {
	now := time.Now()
	lazyRegistryMu.Lock()
	defer lazyRegistryMu.Unlock()
	evicted := 0
	for v := range lazyRegistry {
		if v == nil {
			delete(lazyRegistry, v)
			continue
		}
		v.lazyMu.Lock()
		ready := v.lazyReady
		idle := now.Sub(v.lazyAt)
		if ready && idle >= lazyIdleTTL {
			if v.gunClient != nil {
				_ = v.gunClient.Close()
				v.gunClient = nil
			}
			if v.xhttpClient != nil {
				_ = v.xhttpClient.Close()
				v.xhttpClient = nil
			}
			v.lazyReady = false
			v.lazyErr = nil
			evicted++
		}
		v.lazyMu.Unlock()
	}
	return evicted
}
