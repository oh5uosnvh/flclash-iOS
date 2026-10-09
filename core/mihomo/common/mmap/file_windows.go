package mmap

import (
	"os"
	"reflect"
	"unsafe"

	"golang.org/x/sys/windows"
)

func mapFile(f *os.File, size int) ([]byte, error) {
	h, err := windows.CreateFileMapping(windows.Handle(f.Fd()), nil, windows.PAGE_READONLY, 0, 0, nil)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)
	address, err := windows.MapViewOfFile(h, windows.FILE_MAP_READ, 0, 0, uintptr(size))
	if err != nil {
		return nil, err
	}
	var data []byte
	header := (*reflect.SliceHeader)(unsafe.Pointer(&data))
	header.Data, header.Len, header.Cap = address, size, size
	return data, nil
}

func unmapFile(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return windows.UnmapViewOfFile(uintptr(unsafe.Pointer(&data[0])))
}
