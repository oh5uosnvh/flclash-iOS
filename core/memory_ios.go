//go:build ios && cgo

package main

/*
#include <mach/mach.h>

// coreProcRSSBytes reports resident size.
static unsigned long long coreProcRSSBytes(void) {
	struct mach_task_basic_info info;
	mach_msg_type_number_t count = MACH_TASK_BASIC_INFO_COUNT;
	if (task_info(mach_task_self(), MACH_TASK_BASIC_INFO, (task_info_t)&info, &count) != KERN_SUCCESS) {
		return 0;
	}
	return (unsigned long long)info.resident_size;
}

// coreProcFootprintBytes reports phys_footprint, the metric jetsam actually
// enforces (resident + compressed + IOKit mappings).
static unsigned long long coreProcFootprintBytes(void) {
	struct task_vm_info info;
	mach_msg_type_number_t count = TASK_VM_INFO_COUNT;
	if (task_info(mach_task_self(), TASK_VM_INFO, (task_info_t)&info, &count) != KERN_SUCCESS) {
		return 0;
	}
	return (unsigned long long)info.phys_footprint;
}
*/
import "C"

// procRSSBytes reports the Mach resident size of the current process.
func procRSSBytes() uint64 { return uint64(C.coreProcRSSBytes()) }

// procFootprintBytes reports the phys_footprint of the current process.
func procFootprintBytes() uint64 { return uint64(C.coreProcFootprintBytes()) }
