package trie

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func mappedDomainFixture(t testing.TB) (*DomainSet, []byte) {
	t.Helper()
	var builder DomainSetBuilder
	for _, domain := range []string{"+.example.com", "*.wild.example", ".suffix.test", "exact.test", "例子.测试"} {
		require.NoError(t, builder.Insert(domain))
	}
	for i := 0; i < 300; i++ {
		require.NoError(t, builder.Insert(fmt.Sprintf("node%d.test", i)))
	}
	set := builder.Build()
	var buf bytes.Buffer
	require.NoError(t, set.WriteMapped(&buf))
	require.Equal(t, set.MappedSize(), uint64(buf.Len()))
	return set, buf.Bytes()
}

func TestMappedDomainSetBorrowsEveryArray(t *testing.T) {
	original, data := mappedDomainFixture(t)
	mapped, err := ReadDomainSetMapped(data)
	require.NoError(t, err)
	require.Equal(t, original, mapped)
	pointers := []unsafe.Pointer{unsafe.Pointer(&mapped.leaves[0]), unsafe.Pointer(&mapped.labelBitmap[0]),
		unsafe.Pointer(&mapped.labels[0]), unsafe.Pointer(&mapped.ranks[0]), unsafe.Pointer(&mapped.selects[0])}
	for i, pointer := range pointers {
		offset := binary.LittleEndian.Uint64(data[8+i*16:])
		require.Equal(t, unsafe.Pointer(&data[offset]), pointer)
	}
	for _, query := range []string{"", "example.com", "www.example.com", "EXAMPLE.COM", "notexample.com",
		"wild.example", "a.wild.example", "a.b.wild.example", "suffix.test", "a.suffix.test", "exact.test",
		"a.exact.test", "例子.测试", "node0.test", "node299.test", "node300.test"} {
		require.Equal(t, original.Has(query), mapped.Has(query), query)
	}
}

func TestMappedDomainSetRejectsCorruption(t *testing.T) {
	_, data := mappedDomainFixture(t)
	for length := 1; length < len(data); length++ {
		_, err := ReadDomainSetMapped(data[:length])
		require.Error(t, err, "truncated at %d", length)
	}
	for _, section := range []int{0, 1, 3, 4} {
		corrupt := append([]byte(nil), data...)
		offset := binary.LittleEndian.Uint64(corrupt[8+section*16:])
		for i := 0; i < 8; i++ {
			corrupt[int(offset)+i] = 0xff
		}
		if section == 0 {
			binary.LittleEndian.PutUint64(corrupt[16:], 1)
		}
		_, err := ReadDomainSetMapped(corrupt)
		require.Error(t, err, "section %d", section)
	}
	corrupt := append([]byte(nil), data...)
	binary.LittleEndian.PutUint64(corrupt[8:], ^uint64(0))
	_, err := ReadDomainSetMapped(corrupt)
	require.Error(t, err)
	unaligned := append([]byte{0}, data...)
	_, err = ReadDomainSetMapped(unaligned[1:])
	require.Error(t, err)
}

func FuzzMappedDomainSet(f *testing.F) {
	var builder DomainSetBuilder
	require.NoError(f, builder.Insert("+.example.com"))
	require.NoError(f, builder.Insert("*.example.org"))
	var data bytes.Buffer
	require.NoError(f, builder.Build().WriteMapped(&data))
	f.Add(data.Bytes(), "www.example.com")
	f.Fuzz(func(t *testing.T, data []byte, query string) {
		set, err := ReadDomainSetMapped(data)
		if err == nil {
			set.Has(query)
		}
	})
}

func BenchmarkReadMappedDomainSet(b *testing.B) {
	_, data := mappedDomainFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ReadDomainSetMapped(data); err != nil {
			b.Fatal(err)
		}
	}
}
