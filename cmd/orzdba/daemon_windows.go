//go:build windows

package main

import (
	"errors"
)

// daemonize is unsupported on Windows: there is no setsid/daemon concept.
// Returns an error so main() exits with a clear message instead of silently
// running in the foreground.
func daemonize() error {
	return errors.New("--daemon is not supported on Windows")
}

// daemonDefaultLog is unused on Windows (daemonize always errors).
var daemonDefaultLog = "/tmp/orzdba.log"
