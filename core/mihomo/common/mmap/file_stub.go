//go:build !darwin && !linux && !windows

package mmap

import (
	"errors"
	"os"
)

func mapFile(_ *os.File, _ int) ([]byte, error) {
	return nil, errors.New("file mapping is unsupported on this platform")
}

func unmapFile(_ []byte) error {
	return nil
}
