package cidr

import (
	"errors"
	"io"
	"net/netip"
	"sort"
)

type MappedIPSet struct {
	data []byte
}

const MappedIPRangeSize = 40

var errInvalidMappedIPSet = errors.New("invalid mapped IP ranges")

func (ss *IpCidrSet) RangeCount() int {
	return len(ss.rr)
}

func (ss *IpCidrSet) WriteMapped(w io.Writer) error {
	var record [MappedIPRangeSize]byte
	for _, r := range ss.rr {
		record[0] = byte(r.From().BitLen())
		from, to := r.From().As16(), r.To().As16()
		copy(record[8:24], from[:])
		copy(record[24:], to[:])
		if _, err := w.Write(record[:]); err != nil {
			return err
		}
	}
	return nil
}

// ReadMappedIPSet borrows data; its owner must keep it immutable and alive.
func ReadMappedIPSet(data []byte) (*MappedIPSet, error) {
	invalid := errInvalidMappedIPSet
	if len(data)%MappedIPRangeSize != 0 {
		return nil, invalid
	}
	ss := &MappedIPSet{data: data}
	var previous netip.Addr
	for i := 0; i < len(data)/MappedIPRangeSize; i++ {
		family := data[i*MappedIPRangeSize]
		if family != 32 && family != 128 {
			return nil, invalid
		}
		from, to := ss.rangeAt(i)
		if from.BitLen() != int(family) || to.BitLen() != int(family) || from.Compare(to) > 0 || (previous.IsValid() && previous.Compare(from) >= 0) {
			return nil, invalid
		}
		previous = to
	}
	return ss, nil
}

func (ss *MappedIPSet) rangeAt(i int) (netip.Addr, netip.Addr) {
	var from, to [16]byte
	record := ss.data[i*MappedIPRangeSize : (i+1)*MappedIPRangeSize]
	copy(from[:], record[8:24])
	copy(to[:], record[24:])
	start, end := netip.AddrFrom16(from), netip.AddrFrom16(to)
	if record[0] == 32 {
		start, end = start.Unmap(), end.Unmap()
	}
	return start, end
}

func (ss *MappedIPSet) Contains(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.WithZone("")
	i := sort.Search(len(ss.data)/MappedIPRangeSize, func(i int) bool {
		from, _ := ss.rangeAt(i)
		return from.Compare(ip) > 0
	})
	if i == 0 {
		return false
	}
	from, to := ss.rangeAt(i - 1)
	return from.BitLen() == ip.BitLen() && ip.Compare(to) <= 0
}
