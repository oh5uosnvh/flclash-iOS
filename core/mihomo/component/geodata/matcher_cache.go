package geodata

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/metacubex/mihomo/component/geodata/router"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/log"
)

func loadDomainMatcherCache(name string) (router.DomainMatcher, error) {
	return router.OpenDomainMatcher(filepath.Join(C.Path.MatcherCache(), "geosite_"+name+".bin"))
}

func saveDomainMatcherCache(name string, m router.DomainMatcher) router.DomainMatcher {
	if !C.SaveMatcherCache() {
		return m
	}
	path := filepath.Join(C.Path.MatcherCache(), "geosite_"+name+".bin")
	if err := writeMatcherCache(path, func(w io.Writer) error {
		return router.WriteDomainMatcher(w, m)
	}); err != nil {
		log.Warnln("Save GeoSite cache failed: %s, %v", name, err)
		return m
	}
	mapped, err := router.OpenDomainMatcher(path)
	if err != nil {
		log.Warnln("Map GeoSite cache failed: %s, %v", name, err)
		return m
	}
	return mapped
}

func loadIPMatcherCache(name string) (router.IPMatcher, error) {
	return router.OpenIPMatcher(filepath.Join(C.Path.MatcherCache(), "geoip_"+name+".bin"))
}

func saveIPMatcherCache(name string, m router.IPMatcher) router.IPMatcher {
	if !C.SaveMatcherCache() {
		return m
	}
	path := filepath.Join(C.Path.MatcherCache(), "geoip_"+name+".bin")
	if err := writeMatcherCache(path, func(w io.Writer) error {
		return router.WriteIPMatcher(w, m)
	}); err != nil {
		log.Warnln("Save GeoIP cache failed: %s, %v", name, err)
		return m
	}
	mapped, err := router.OpenIPMatcher(path)
	if err != nil {
		log.Warnln("Map GeoIP cache failed: %s, %v", name, err)
		return m
	}
	return mapped
}

func writeMatcherCache(path string, write func(io.Writer) error) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".matcher-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	w := bufio.NewWriter(f)
	if err := write(w); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Replacing the directory entry keeps existing mappings on the old inode.
	return os.Rename(f.Name(), path)
}

func removeMatcherCaches(prefix string) {
	entries, err := os.ReadDir(C.Path.MatcherCache())
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) && strings.HasSuffix(entry.Name(), ".bin") {
			if err := os.Remove(filepath.Join(C.Path.MatcherCache(), entry.Name())); err != nil {
				log.Warnln("Remove matcher cache failed: %s, %v", entry.Name(), err)
			}
		}
	}
}
