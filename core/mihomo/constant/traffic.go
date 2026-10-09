package constant

import "sync/atomic"

type TrafficCounter struct {
	upload   atomic.Int64
	download atomic.Int64
}

func (c *TrafficCounter) AddUpload(size int64) {
	if c != nil && size > 0 {
		c.upload.Add(size)
	}
}

func (c *TrafficCounter) AddDownload(size int64) {
	if c != nil && size > 0 {
		c.download.Add(size)
	}
}

func (c *TrafficCounter) Total() (up, down int64) {
	if c == nil {
		return 0, 0
	}
	return c.upload.Load(), c.download.Load()
}

func (c *TrafficCounter) Reset() {
	if c != nil {
		c.upload.Store(0)
		c.download.Store(0)
	}
}
