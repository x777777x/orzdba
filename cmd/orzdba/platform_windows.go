//go:build windows

package main

import (
	"fmt"
	"runtime"
)

// detectCPU returns runtime.NumCPU on Windows (no /proc, no sysctl).
func detectCPU() int {
	return runtime.NumCPU()
}

// checkDiskDevices skips validation on Windows; /proc-style checks do not
// apply. Devices degrade to zeros at runtime (matching the pre-existing
// non-Linux behavior).
func checkDiskDevices(devices []string) error {
	_ = devices
	return nil
}

// checkNetDevice skips validation on Windows; /proc-style checks do not apply.
func checkNetDevice(dev string) error {
	_ = dev
	return nil
}

// ensure fmt is referenced for future error paths on Windows.
var _ = fmt.Sprintf
