//go:build unix

package rtcol

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// procAlive reports whether the given PID has a live process. Used for
// stale-lock detection (P1-1): signal 0 probes existence without signaling.
func procAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// readLockPID reads the PID recorded in a lock file, if parseable.
func readLockPID(path string) (int, bool) {
	holder, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(holder)))
	if err != nil {
		return 0, false
	}
	return pid, true
}
