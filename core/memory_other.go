//go:build !ios || !cgo

package main

// procRSSBytes and procFootprintBytes are unavailable off-iOS; the memory
// probe reports zero for them.
func procRSSBytes() uint64       { return 0 }
func procFootprintBytes() uint64 { return 0 }
