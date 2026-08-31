//go:build windows

package rtcol

import (
	"os"
	"strconv"
	"strings"
)

// procAlive on Windows cannot probe a PID with signal 0 (no syscall.Kill).
// -rt is Linux-only in practice (tcprstat is a Linux binary), so the lock
// simply trusts the recorded PID: a stale lock is not reclaimed, and Start
// fails with a manual-removal hint. This keeps the package compilable on
// Windows while production behavior is unchanged on Linux.
func procAlive(pid int) bool { return false }

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
