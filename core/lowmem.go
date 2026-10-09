//go:build with_low_memory

package main

import "runtime/debug"

// iOS NE jetsam counts dirty pages against a ~50MB budget (≈33MB net after
// the system's own footprint). Two knobs keep the dirty set small:
//   - GOGC=30: pace collections so the heap goal hugs the live set (a 7MB
//     live heap now targets ~9MB instead of ~14-20MB at the default GOGC=100)
//     — this directly shrinks the arena pages that become phys_footprint.
//   - 20MB soft limit: backstop so transient load peaks are collected hard
//     instead of ballooning the arena.
const lowMemoryLimit = 20 * 1024 * 1024

func init() {
	debug.SetGCPercent(30)
	debug.SetMemoryLimit(lowMemoryLimit)
}
