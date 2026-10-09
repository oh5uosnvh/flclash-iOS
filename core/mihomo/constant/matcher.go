package constant

import (
	"net/netip"
	"os"
)

type DomainMatcher interface {
	MatchDomain(domain string) bool
}

type IpMatcher interface {
	MatchIp(ip netip.Addr) bool
}

var saveMatcherCache bool

func SetSaveMatcherCache(save bool) {
	saveMatcherCache = save
	if save {
		os.RemoveAll(Path.MatcherCache())
		os.MkdirAll(Path.MatcherCache(), 0o755)
	}
}

func SaveMatcherCache() bool {
	return saveMatcherCache
}
