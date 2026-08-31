//go:build windows

package main

// setUmask is a no-op on Windows: the umask concept does not exist. File
// permissions are enforced per-file via 0600 opens (logsink openFile, rtcol
// log/lock), which is the portable equivalent.
func setUmask() {}
