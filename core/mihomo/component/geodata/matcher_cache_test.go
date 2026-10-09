package geodata

import (
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/metacubex/mihomo/component/geodata/router"
	C "github.com/metacubex/mihomo/constant"
	"github.com/stretchr/testify/require"
)

type matcherCacheLoader struct {
	LoaderImplementation
	domain    string
	prefix    string
	siteLoads int
	ipLoads   int
}

func (l *matcherCacheLoader) LoadSiteByPath(_, _ string) ([]*router.Domain, error) {
	l.siteLoads++
	return []*router.Domain{{Type: router.Domain_Domain, Value: l.domain}}, nil
}

func (l *matcherCacheLoader) LoadIPByPath(_, _ string) ([]*router.CIDR, error) {
	l.ipLoads++
	prefix := netip.MustParsePrefix(l.prefix)
	return []*router.CIDR{{Ip: prefix.Addr().AsSlice(), Prefix: uint32(prefix.Bits())}}, nil
}

func setupMatcherCache(t *testing.T) *matcherCacheLoader {
	t.Helper()
	oldHome, oldSave := C.Path.HomeDir(), C.SaveMatcherCache()
	oldLoader, oldMatcher := geoLoaderName, geoSiteMatcher
	oldLoaders := loaders
	dir := t.TempDir()
	C.SetHomeDir(dir)
	C.SetSaveMatcherCache(true)
	loader := &matcherCacheLoader{domain: "old.example", prefix: "192.0.2.0/24"}
	loaders = map[string]func() LoaderImplementation{"cache-test": func() LoaderImplementation { return loader }}
	SetLoader("cache-test")
	SetSiteMatcher("succinct")
	t.Cleanup(func() {
		ClearGeoSiteCache()
		ClearGeoIPCache()
		C.SetSaveMatcherCache(oldSave)
		C.SetHomeDir(oldHome)
		geoLoaderName, geoSiteMatcher, loaders = oldLoader, oldMatcher, oldLoaders
		require.Eventually(t, func() bool {
			runtime.GC()
			return os.RemoveAll(dir) == nil
		}, 5*time.Second, 10*time.Millisecond)
	})
	return loader
}

func TestMatcherCacheRebuildsV1AndMapsFirstLoad(t *testing.T) {
	loader := setupMatcherCache(t)
	for _, name := range []string{"geosite_test.bin", "geoip_test.bin"} {
		require.NoError(t, os.WriteFile(filepath.Join(C.Path.MatcherCache(), name), []byte{1, 0}, 0o600))
	}
	domain, err := LoadGeoSiteMatcher("test")
	require.NoError(t, err)
	ip, err := LoadGeoIPMatcher("test")
	require.NoError(t, err)
	require.Contains(t, reflect.TypeOf(domain).String(), "mappedDomainMatcher")
	require.Contains(t, reflect.TypeOf(ip).String(), "mappedIPMatcher")
	require.True(t, domain.ApplyDomain("www.old.example"))
	require.True(t, ip.Match(netip.MustParseAddr("192.0.2.1")))
	loadGeoSiteMatcherSF.Reset()
	loadGeoSiteMatcherListSF.Reset()
	loadGeoIPMatcherSF.Reset()
	C.SetSaveMatcherCache(false)
	domain, err = LoadGeoSiteMatcher("test")
	require.NoError(t, err)
	ip, err = LoadGeoIPMatcher("test")
	require.NoError(t, err)
	require.Equal(t, 1, loader.siteLoads)
	require.Equal(t, 1, loader.ipLoads)
	require.True(t, domain.ApplyDomain("old.example"))
	require.True(t, ip.Match(netip.MustParseAddr("192.0.2.255")))
}

func TestMatcherCacheInvalidationPreservesExistingReaders(t *testing.T) {
	loader := setupMatcherCache(t)
	oldDomain, err := LoadGeoSiteMatcher("test")
	require.NoError(t, err)
	oldIP, err := LoadGeoIPMatcher("test")
	require.NoError(t, err)
	loader.domain, loader.prefix = "new.example", "198.51.100.0/24"
	ClearGeoSiteCache()
	ClearGeoIPCache()
	newDomain, err := LoadGeoSiteMatcher("test")
	require.NoError(t, err)
	newIP, err := LoadGeoIPMatcher("test")
	require.NoError(t, err)
	require.Equal(t, 2, loader.siteLoads)
	require.Equal(t, 2, loader.ipLoads)
	require.True(t, oldDomain.ApplyDomain("old.example"))
	require.False(t, oldDomain.ApplyDomain("new.example"))
	require.True(t, newDomain.ApplyDomain("new.example"))
	require.False(t, newDomain.ApplyDomain("old.example"))
	require.True(t, oldIP.Match(netip.MustParseAddr("192.0.2.1")))
	require.False(t, newIP.Match(netip.MustParseAddr("192.0.2.1")))
	require.True(t, newIP.Match(netip.MustParseAddr("198.51.100.1")))
}

func TestFailedCacheWritePreservesPublishedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.bin")
	require.NoError(t, os.WriteFile(path, []byte("published"), 0o600))
	want := errors.New("interrupted write")
	err := writeMatcherCache(path, func(w io.Writer) error {
		_, err := w.Write([]byte("partial"))
		require.NoError(t, err)
		return want
	})
	require.ErrorIs(t, err, want)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "published", string(data))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestMatcherCacheReplacementPreservesOldMapping(t *testing.T) {
	setupMatcherCache(t)
	first, err := router.NewSuccinctMatcherGroup([]*router.Domain{{Type: router.Domain_Full, Value: "first.example"}})
	require.NoError(t, err)
	old := saveDomainMatcherCache("replace", first)
	second, err := router.NewSuccinctMatcherGroup([]*router.Domain{{Type: router.Domain_Full, Value: "second.example"}})
	require.NoError(t, err)
	current := saveDomainMatcherCache("replace", second)
	loaded, err := loadDomainMatcherCache("replace")
	require.NoError(t, err)
	require.True(t, old.ApplyDomain("first.example"))
	require.False(t, old.ApplyDomain("second.example"))
	require.True(t, current.ApplyDomain("second.example"))
	require.True(t, loaded.ApplyDomain("second.example"))
	require.False(t, loaded.ApplyDomain("first.example"))
}
