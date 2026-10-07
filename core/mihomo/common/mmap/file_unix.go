//go:build darwin || linux

package mmap

import (
	"os"

	"golang.org/x/sys/unix"
)

func mapFile(f *os.File, size int) ([]byte, error) {
	return unix.Mmap(int(f.Fd()), 0, size, unix.PROT_READ, unix.MAP_PRIVATE)
}

func unmapFile(data []byte) error {
	return unix.Munmap(data)
}
