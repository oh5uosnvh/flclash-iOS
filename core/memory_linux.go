//go:build linux

package main

import (
	"bytes"
	"os"
)

// procFootprintBytes approximates phys_footprint on Linux via /proc/self/statm
// (resident pages). Close enough for sandbox verification of the memory curve;
// iOS builds use the Mach phys_footprint.
func procFootprintBytes() uint64 {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil || len(data) < 2 {
		return 0
	}
	fields := bytes.Fields(data)
	if len(fields) < 2 {
		return 0
	}
	var residentPages uint64
	for _, c := range fields[1] {
		if c < '0' || c > '9' {
			break
		}
		residentPages = residentPages*10 + uint64(c-'0')
	}
	return residentPages * uint64(os.Getpagesize())
}

// procRSSBytes mirrors procFootprintBytes for Linux sandboxes.
func procRSSBytes() uint64 { return procFootprintBytes() }
