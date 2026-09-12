//go:build unix

package mysqlc

import "syscall"

// fileOwnerUID returns the owning uid of path, or ok=false when the platform
// cannot report it (the Windows stub) or stat fails.
func fileOwnerUID(path string) (uid int, ok bool) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return 0, false
	}
	return int(st.Uid), true
}
