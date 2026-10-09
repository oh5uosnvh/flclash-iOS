//go:build (!ios && !linux) || (!ios && !cgo && !linux)

package main

// procRSSBytes and procFootprintBytes are unavailable on this platform; the
// memory probe reports zero for them.
func procRSSBytes() uint64       { return 0 }
func procFootprintBytes() uint64 { return 0 }
