package router

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func domainFixture(t testing.TB, domains []*Domain) (DomainMatcher, []byte) {
	t.Helper()
	m, err := NewSuccinctMatcherGroup(domains)
	require.NoError(t, err)
	var data bytes.Buffer
	require.NoError(t, WriteDomainMatcher(&data, m))
	return m, data.Bytes()
}

func ipFixture(t testing.TB, prefixes []string) (IPMatcher, []byte) {
	t.Helper()
	var entries []*CIDR
	for _, s := range prefixes {
		prefix := netip.MustParsePrefix(s)
		entries = append(entries, &CIDR{Ip: prefix.Addr().AsSlice(), Prefix: uint32(prefix.Bits())})
	}
	m, err := NewGeoIPMatcher(entries)
	require.NoError(t, err)
	var data bytes.Buffer
	require.NoError(t, WriteIPMatcher(&data, m))
	return m, data.Bytes()
}

func TestMappedDomainMatcher(t *testing.T) {
	for _, domains := range [][]*Domain{
		nil,
		{{Type: Domain_Plain, Value: "keyword"}, {Type: Domain_Regex, Value: `^regexp[0-9]+\.test$`}},
		{{Type: Domain_Full, Value: "exact.test"}, {Type: Domain_Domain, Value: "suffix.test"},
			{Type: Domain_Plain, Value: "keyword"}, {Type: Domain_Regex, Value: `^regexp[0-9]+\.test$`}},
	} {
		t.Run(fmt.Sprint(len(domains)), func(t *testing.T) {
			original, data := domainFixture(t, domains)
			path := filepath.Join(t.TempDir(), "domain.bin")
			require.NoError(t, os.WriteFile(path, data, 0o600))
			mapped, err := OpenDomainMatcher(path)
			require.NoError(t, err)
			t.Cleanup(mapped.(*mappedDomainMatcher).mapping.Close)
			require.Equal(t, original.Count(), mapped.Count())
			for _, query := range []string{"", "exact.test", "EXACT.TEST", "a.exact.test", "suffix.test",
				"a.suffix.test", "notsuffix.test", "the-keyword.test", "regexp12.test", "regexp.test"} {
				require.Equal(t, original.ApplyDomain(query), mapped.ApplyDomain(query), query)
				require.Equal(t, !original.ApplyDomain(query), NewNotDomainMatcherGroup(mapped).ApplyDomain(query))
			}
			var rewritten bytes.Buffer
			require.NoError(t, WriteDomainMatcher(&rewritten, mapped))
			require.Equal(t, data, rewritten.Bytes())
		})
	}
}

func TestMappedIPMatcher(t *testing.T) {
	for _, prefixes := range [][]string{nil,
		{"10.0.0.0/24", "10.0.1.0/24", "192.0.2.5/32", "2001:db8::/32", "fe80::/10"},
		{"0.0.0.0/0", "::/0"},
		{"::ffff:192.0.2.0/120"},
	} {
		t.Run(fmt.Sprint(prefixes), func(t *testing.T) {
			original, data := ipFixture(t, prefixes)
			path := filepath.Join(t.TempDir(), "ip.bin")
			require.NoError(t, os.WriteFile(path, data, 0o600))
			mapped, err := OpenIPMatcher(path)
			require.NoError(t, err)
			t.Cleanup(mapped.(*mappedIPMatcher).mapping.Close)
			require.Equal(t, original.Count(), mapped.Count())
			queries := []netip.Addr{{}}
			for _, s := range []string{"0.0.0.0", "255.255.255.255", "10.0.0.0", "10.0.1.255", "10.0.2.0",
				"192.0.2.5", "192.0.2.6", "::", "2001:db8::1", "2001:db9::", "fe80::1%en0",
				"::ffff:192.0.2.5", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"} {
				queries = append(queries, netip.MustParseAddr(s))
			}
			for _, query := range queries {
				require.Equal(t, original.Match(query), mapped.Match(query), query.String())
				require.Equal(t, !original.Match(query), NewNotIpMatcherGroup(mapped).Match(query))
			}
			var rewritten bytes.Buffer
			require.NoError(t, WriteIPMatcher(&rewritten, mapped))
			require.Equal(t, data, rewritten.Bytes())
		})
	}
}

func TestMatcherV2RejectsCorruption(t *testing.T) {
	_, domainData := domainFixture(t, []*Domain{{Type: Domain_Domain, Value: "example.com"}, {Type: Domain_Plain, Value: "word"}})
	_, ipData := ipFixture(t, []string{"192.0.2.0/24", "2001:db8::/32"})
	for _, test := range []struct {
		name string
		data []byte
		read func([]byte) error
	}{
		{"domain", domainData, func(data []byte) error { _, err := readDomainMatcher(data); return err }},
		{"ip", ipData, func(data []byte) error { _, _, err := readIPMatcher(data); return err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			for length := 0; length < len(test.data); length++ {
				require.Error(t, test.read(test.data[:length]), "truncated at %d", length)
			}
			legacy := append([]byte(nil), test.data...)
			legacy[0] = 1
			require.Error(t, test.read(legacy))
			for _, offset := range []int{8, 16, 24} {
				corrupt := append([]byte(nil), test.data...)
				binary.LittleEndian.PutUint64(corrupt[offset:], ^uint64(0))
				require.Error(t, test.read(corrupt))
			}
			require.Error(t, test.read(append(append([]byte(nil), test.data...), 1)))
		})
	}
}

func TestMappedMatcherSurvivesConcurrentGC(t *testing.T) {
	_, data := domainFixture(t, []*Domain{{Type: Domain_Domain, Value: "example.com"}})
	path := filepath.Join(t.TempDir(), "domain.bin")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	mapped, err := OpenDomainMatcher(path)
	require.NoError(t, err)
	t.Cleanup(mapped.(*mappedDomainMatcher).mapping.Close)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10000; i++ {
				if !mapped.ApplyDomain("www.example.com") {
					t.Error("mapping lost during lookup")
					return
				}
			}
		}()
	}
	for i := 0; i < 5; i++ {
		runtime.GC()
	}
	wg.Wait()
}

func FuzzMatcherV2(f *testing.F) {
	_, domains := domainFixture(f, []*Domain{{Type: Domain_Domain, Value: "example.com"}, {Type: Domain_Regex, Value: "test.*"}})
	_, ips := ipFixture(f, []string{"192.0.2.0/24", "2001:db8::/32"})
	f.Add(domains)
	f.Add(ips)
	f.Fuzz(func(t *testing.T, data []byte) {
		if m, err := readDomainMatcher(data); err == nil {
			m.ApplyDomain("www.example.com")
		}
		if m, _, err := readIPMatcher(data); err == nil {
			m.Contains(netip.MustParseAddr("192.0.2.1"))
		}
	})
}
