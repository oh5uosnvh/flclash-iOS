package router

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"runtime"
	"strings"

	"github.com/metacubex/mihomo/common/mmap"
	"github.com/metacubex/mihomo/component/cidr"
	"github.com/metacubex/mihomo/component/geodata/strmatcher"
	"github.com/metacubex/mihomo/component/trie"
)

const matcherHeaderSize = 32

var (
	domainMatcherMagic = [8]byte{2, 'M', 'I', 'H', 'O', 'M', 'O', 'D'}
	ipMatcherMagic     = [8]byte{2, 'M', 'I', 'H', 'O', 'M', 'O', 'I'}
)

func WriteDomainMatcher(w io.Writer, m DomainMatcher) error {
	switch m := m.(type) {
	case *succinctDomainMatcher:
		return m.WriteBin(w)
	case *mappedDomainMatcher:
		_, err := w.Write(m.mapping.Data)
		runtime.KeepAlive(m.mapping)
		return err
	default:
		return fmt.Errorf("only succinct domain matchers support serialization")
	}
}

func WriteIPMatcher(w io.Writer, m IPMatcher) error {
	switch m := m.(type) {
	case *geoIPMatcher:
		return m.WriteBin(w)
	case *mappedIPMatcher:
		_, err := w.Write(m.mapping.Data)
		runtime.KeepAlive(m.mapping)
		return err
	default:
		return fmt.Errorf("unsupported IP matcher for serialization")
	}
}

func (m *succinctDomainMatcher) WriteBin(w io.Writer) error {
	var header [matcherHeaderSize]byte
	copy(header[:], domainMatcherMagic[:])
	binary.LittleEndian.PutUint64(header[8:], uint64(m.count))
	binary.LittleEndian.PutUint64(header[16:], m.set.MappedSize())
	binary.LittleEndian.PutUint64(header[24:], uint64(len(m.otherMatchers)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if err := m.set.WriteMapped(w); err != nil {
		return err
	}
	for _, matcher := range m.otherMatchers {
		s := matcher.String()
		if err := binary.Write(w, binary.LittleEndian, uint64(len(s))); err != nil {
			return err
		}
		if _, err := io.WriteString(w, s); err != nil {
			return err
		}
	}
	return nil
}

func (m *geoIPMatcher) WriteBin(w io.Writer) error {
	var header [matcherHeaderSize]byte
	copy(header[:], ipMatcherMagic[:])
	binary.LittleEndian.PutUint64(header[8:], uint64(m.count))
	binary.LittleEndian.PutUint64(header[16:], uint64(m.cidrSet.RangeCount()))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	return m.cidrSet.WriteMapped(w)
}

type mappedDomainMatcher struct {
	matcher succinctDomainMatcher
	mapping *mmap.File
}

func (m *mappedDomainMatcher) ApplyDomain(domain string) bool {
	matched := m.matcher.ApplyDomain(domain)
	runtime.KeepAlive(m.mapping)
	return matched
}

func (m *mappedDomainMatcher) Count() int {
	return m.matcher.count
}

type mappedIPMatcher struct {
	set     *cidr.MappedIPSet
	count   int
	mapping *mmap.File
}

func (m *mappedIPMatcher) Match(ip netip.Addr) bool {
	matched := m.set.Contains(ip)
	runtime.KeepAlive(m.mapping)
	return matched
}

func (m *mappedIPMatcher) Count() int {
	return m.count
}

func OpenDomainMatcher(path string) (DomainMatcher, error) {
	mapping, err := mmap.Open(path)
	if err != nil {
		return nil, err
	}
	m, err := readDomainMatcher(mapping.Data)
	if err != nil {
		mapping.Close()
		return nil, err
	}
	return &mappedDomainMatcher{matcher: *m, mapping: mapping}, nil
}

func OpenIPMatcher(path string) (IPMatcher, error) {
	mapping, err := mmap.Open(path)
	if err != nil {
		return nil, err
	}
	set, count, err := readIPMatcher(mapping.Data)
	if err != nil {
		mapping.Close()
		return nil, err
	}
	return &mappedIPMatcher{set: set, count: count, mapping: mapping}, nil
}

func matcherCount(data []byte, magic [8]byte) (int, error) {
	if len(data) < matcherHeaderSize || string(data[:8]) != string(magic[:]) {
		return 0, errors.New("invalid matcher cache: version 2 required")
	}
	count := binary.LittleEndian.Uint64(data[8:])
	if count > uint64(int(^uint(0)>>1)) {
		return 0, errors.New("invalid matcher count")
	}
	return int(count), nil
}

func readDomainMatcher(data []byte) (*succinctDomainMatcher, error) {
	count, err := matcherCount(data, domainMatcherMagic)
	if err != nil {
		return nil, err
	}
	setSize := binary.LittleEndian.Uint64(data[16:])
	otherCount := binary.LittleEndian.Uint64(data[24:])
	data = data[matcherHeaderSize:]
	if setSize > uint64(len(data)) {
		return nil, errors.New("invalid domain set size")
	}
	set, err := trie.ReadDomainSetMapped(data[:setSize])
	if err != nil {
		return nil, err
	}
	data = data[setSize:]
	if otherCount > uint64(len(data))/9 || otherCount > uint64(count) {
		return nil, errors.New("invalid domain matcher count")
	}
	m := &succinctDomainMatcher{set: set, count: count}
	for i := uint64(0); i < otherCount; i++ {
		if len(data) < 8 {
			return nil, io.ErrUnexpectedEOF
		}
		length := binary.LittleEndian.Uint64(data)
		data = data[8:]
		if length == 0 || length > uint64(len(data)) {
			return nil, errors.New("invalid domain matcher length")
		}
		matcher, err := parseMatcherString(string(data[:length]))
		if err != nil {
			return nil, err
		}
		m.otherMatchers = append(m.otherMatchers, matcher)
		data = data[length:]
	}
	if len(data) != 0 {
		return nil, errors.New("trailing domain matcher data")
	}
	return m, nil
}

func readIPMatcher(data []byte) (*cidr.MappedIPSet, int, error) {
	count, err := matcherCount(data, ipMatcherMagic)
	if err != nil {
		return nil, 0, err
	}
	ranges := binary.LittleEndian.Uint64(data[16:])
	if binary.LittleEndian.Uint64(data[24:]) != 0 {
		return nil, 0, errors.New("invalid IP matcher flags")
	}
	data = data[matcherHeaderSize:]
	if len(data)%cidr.MappedIPRangeSize != 0 || ranges != uint64(len(data)/cidr.MappedIPRangeSize) || ranges > uint64(count) {
		return nil, 0, errors.New("invalid IP matcher range count")
	}
	set, err := cidr.ReadMappedIPSet(data)
	return set, count, err
}

var matcherPrefixMap = map[string]strmatcher.Type{
	"full":    strmatcher.Full,
	"keyword": strmatcher.Substr,
	"domain":  strmatcher.Domain,
	"regexp":  strmatcher.Regex,
}

func parseMatcherString(s string) (strmatcher.Matcher, error) {
	idx := strings.IndexByte(s, ':')
	if idx < 0 {
		return nil, errors.New("invalid matcher string: missing type prefix")
	}
	typ, ok := matcherPrefixMap[s[:idx]]
	if !ok {
		return nil, errors.New("invalid matcher string: unknown type prefix")
	}
	return typ.New(s[idx+1:])
}
