package trie

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"math/bits"
	"unsafe"
)

const domainSetHeaderSize = 88

var domainSetMagic = [8]byte{2, 'D', 'O', 'M', 'S', 'E', 'T', 0}
var errInvalidMappedDomainSet = errors.New("invalid mapped domain set")

func alignDomainSection(n uint64) uint64 {
	return (n + 7) &^ 7
}

func (ss *DomainSet) mappedLengths() [5]uint64 {
	return [5]uint64{uint64(len(ss.leaves)) * 8, uint64(len(ss.labelBitmap)) * 8,
		uint64(len(ss.labels)), uint64(len(ss.ranks)) * 4, uint64(len(ss.selects)) * 4}
}

func (ss *DomainSet) MappedSize() uint64 {
	if ss == nil {
		return 0
	}
	size := uint64(domainSetHeaderSize)
	for _, length := range ss.mappedLengths() {
		size += alignDomainSection(length)
	}
	return size
}

func (ss *DomainSet) WriteMapped(w io.Writer) error {
	if ss == nil {
		return nil
	}
	var header [domainSetHeaderSize]byte
	copy(header[:], domainSetMagic[:])
	offset := uint64(len(header))
	lengths := ss.mappedLengths()
	for i, length := range lengths {
		binary.LittleEndian.PutUint64(header[8+i*16:], offset)
		binary.LittleEndian.PutUint64(header[16+i*16:], length)
		offset += alignDomainSection(length)
	}
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	sections := []any{ss.leaves, ss.labelBitmap, ss.labels, ss.ranks, ss.selects}
	for i, section := range sections {
		var err error
		switch values := section.(type) {
		case []uint64:
			err = writeDomainWords(w, values)
		case []int32:
			err = writeDomainWords(w, values)
		case []byte:
			_, err = w.Write(values)
		}
		if err != nil {
			return err
		}
		var padding [8]byte
		if _, err = w.Write(padding[:alignDomainSection(lengths[i])-lengths[i]]); err != nil {
			return err
		}
	}
	return nil
}

func writeDomainWords[T uint64 | int32](w io.Writer, values []T) error {
	for len(values) > 0 {
		n := len(values)
		if n > 512 {
			n = 512
		}
		if err := binary.Write(w, binary.LittleEndian, values[:n]); err != nil {
			return err
		}
		values = values[n:]
	}
	return nil
}

// ReadDomainSetMapped borrows data; its owner must keep it immutable and alive.
func ReadDomainSetMapped(data []byte) (*DomainSet, error) {
	if len(data) == 0 {
		return nil, nil
	}
	invalid := errInvalidMappedDomainSet
	if len(data) < domainSetHeaderSize || string(data[:8]) != string(domainSetMagic[:]) {
		return nil, invalid
	}
	var endian uint16 = 1
	if *(*byte)(unsafe.Pointer(&endian)) != 1 || uintptr(unsafe.Pointer(&data[0]))%8 != 0 {
		return nil, errors.New("mapped domain set requires aligned little-endian storage")
	}
	var sections [5][]byte
	offset := uint64(domainSetHeaderSize)
	for i := range sections {
		start := binary.LittleEndian.Uint64(data[8+i*16:])
		length := binary.LittleEndian.Uint64(data[16+i*16:])
		if start != offset || start > uint64(len(data)) || length > uint64(len(data))-start {
			return nil, invalid
		}
		sections[i] = data[start : start+length : start+length]
		offset += alignDomainSection(length)
	}
	if offset != uint64(len(data)) || len(sections[0]) == 0 || len(sections[0])%8 != 0 ||
		len(sections[1]) == 0 || len(sections[1])%8 != 0 || len(sections[2]) == 0 ||
		len(sections[3]) == 0 || len(sections[3])%4 != 0 || len(sections[4]) == 0 || len(sections[4])%4 != 0 {
		return nil, invalid
	}
	ss := &DomainSet{
		leaves:      unsafe.Slice((*uint64)(unsafe.Pointer(&sections[0][0])), len(sections[0])/8),
		labelBitmap: unsafe.Slice((*uint64)(unsafe.Pointer(&sections[1][0])), len(sections[1])/8),
		labels:      sections[2],
		ranks:       unsafe.Slice((*int32)(unsafe.Pointer(&sections[3][0])), len(sections[3])/4),
		selects:     unsafe.Slice((*int32)(unsafe.Pointer(&sections[4][0])), len(sections[4])/4),
	}
	if !ss.validMapped() {
		return nil, invalid
	}
	return ss, nil
}

func (ss *DomainSet) validMapped() bool {
	if len(ss.labels) > (math.MaxInt32-1)/2 {
		return false
	}
	nodes := len(ss.labels) + 1
	bitCount := nodes*2 - 1
	if len(ss.leaves) != (nodes+63)/64 || len(ss.labelBitmap) != (bitCount+63)/64 ||
		len(ss.ranks) != len(ss.labelBitmap)+1 || len(ss.selects) != (nodes+31)/32 {
		return false
	}
	ones := 0
	for wordIndex, word := range ss.labelBitmap {
		if ss.ranks[wordIndex] != int32(ones) {
			return false
		}
		for remaining := word; remaining != 0; remaining &= remaining - 1 {
			position := wordIndex*64 + bits.TrailingZeros64(remaining)
			if position >= bitCount || ones >= nodes || (ones%32 == 0 && ss.selects[ones/32] != int32(position)) {
				return false
			}
			ones++
			if position != bitCount-1 && ones > position+1-ones {
				return false
			}
		}
	}
	if ones != nodes || ss.ranks[len(ss.labelBitmap)] != int32(ones) || getBit(ss.labelBitmap, bitCount-1) == 0 {
		return false
	}
	if nodes%64 != 0 && ss.leaves[len(ss.leaves)-1]>>uint(nodes%64) != 0 {
		return false
	}
	return true
}
