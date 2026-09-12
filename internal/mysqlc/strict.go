package mysqlc

import (
	"fmt"
	"os"
)

// StrictCNFPath is the orzdba-owned credential file. Unlike the shared my.cnf
// search entries (which orzdba reads as a courtesy and only warns about), this
// file is orzdba's own: it is held to a strict standard — exclusive
// permissions (0600) and an owner that is the running user or root — and
// orzdba refuses to start if either check fails. It sits at the head of
// DefaultCNFSearch so it wins the merge.
const StrictCNFPath = "/etc/orzdba.cnf"

// CheckStrictFile enforces the orzdba credential-file standard on path:
//
//   - the file must not be readable by group or other (mode bits 0o077 clear)
//   - on Unix the owner must be the current euid or root
//
// A violation is an error, not a warning: silently continuing with a
// group-readable password file would be exactly the exposure the strict file
// exists to prevent. The operator fixes the file and reruns. (Windows has no
// owner concept; only the permission bits are enforced.)
func CheckStrictFile(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cannot stat credential file %s: %w", path, err)
	}
	if fi.Mode()&0o077 != 0 {
		return fmt.Errorf("refusing to read credential file %s (mode %o): it is readable by group/other, so the password could leak to other local users; run: chmod 600 %s", path, fi.Mode().Perm(), path)
	}
	if uid, ok := fileOwnerUID(path); ok {
		if uid != os.Geteuid() && uid != 0 {
			return fmt.Errorf("refusing to read credential file %s: owned by uid %d, want the current user (%d) or root; run: chown %d %s", path, uid, os.Geteuid(), os.Geteuid(), path)
		}
	}
	return nil
}
