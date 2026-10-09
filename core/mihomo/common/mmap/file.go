package mmap

import (
	"errors"
	"os"
	"runtime"
)

// File owns a read-only mapping; derived slices require runtime.KeepAlive(File).
type File struct {
	Data []byte
}

func Open(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || uint64(info.Size()) > uint64(int(^uint(0)>>1)) {
		return nil, errors.New("invalid mapping size or file type")
	}
	data, err := mapFile(f, int(info.Size()))
	if err != nil {
		return nil, err
	}
	m := &File{Data: data}
	runtime.SetFinalizer(m, (*File).release)
	return m, nil
}

func (m *File) release() {
	_ = unmapFile(m.Data)
	m.Data = nil
}

// Close is only safe before publishing slices or after all readers have stopped.
func (m *File) Close() {
	runtime.SetFinalizer(m, nil)
	m.release()
}
