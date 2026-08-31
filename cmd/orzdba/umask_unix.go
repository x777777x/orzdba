//go:build unix

package main

import "syscall"

// setUmask applies a restrictive umask (0o077) so any created files (logs,
// tcprstat output) are 0600-by-default (plan §8.4). On Unix platforms the
// umask is inherited by the tcprstat subprocess as well.
func setUmask() { syscall.Umask(0o077) }
