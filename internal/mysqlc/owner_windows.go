//go:build windows

package mysqlc

// fileOwnerUID is not reportable on Windows (no uid concept in syscall.Stat_t);
// owner checks are skipped there and only the permission bits are enforced.
func fileOwnerUID(path string) (uid int, ok bool) {
	return 0, false
}
