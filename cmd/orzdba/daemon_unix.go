//go:build unix

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// daemonize re-launches the current process in the background (setsid), with
// stdin/stdout/stderr redirected to /dev/null, then exits the parent.
//
// It is called immediately after argument parsing and before any resources
// (sinks, tcprstat, MySQL) are opened, so the daemon child starts clean. The
// --daemon flag is stripped from the child's argv to avoid infinite recursion.
//
// The daemon's output goes to its logfile: -L/--logfile if given, else a
// default path is injected into the child's argv (stdout is /dev/null).
func daemonize() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("daemonize: cannot locate executable: %w", err)
	}
	args := daemonChildArgs(os.Args[1:], daemonDefaultLog)
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("daemonize: cannot open %s: %w", os.DevNull, err)
	}
	defer devnull.Close()

	cmd := exec.Command(exe, args...)
	cmd.Stdin = devnull
	cmd.Stdout = devnull
	cmd.Stderr = devnull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("daemonize: cannot start background process: %w", err)
	}
	// Read the pid BEFORE Release() (Release clears cmd.Process).
	pid := cmd.Process.Pid
	// Detach: the child keeps running after we exit.
	_ = cmd.Process.Release()
	// Tell the operator the daemon pid, then main() returns and the parent
	// process exits.
	fmt.Fprintf(os.Stderr, "orzdba daemon started (pid %d)\n", pid)
	return nil
}

// daemonChildArgs rebuilds the daemon child's argv: it strips --daemon and,
// when no logfile flag is present, appends the default log path (with daily
// rotation) so the daemon persists output. Pure so it can be unit-tested.
func daemonChildArgs(argv []string, defaultLog string) []string {
	args := make([]string, 0, len(argv)+2)
	hasLogfile := false
	for _, a := range argv {
		if a == "--daemon" || a == "--daemon=true" {
			continue
		}
		// Detect any logfile form: "-L", "--logfile", "--logfile=path".
		if a == "-L" || a == "--logfile" || strings.HasPrefix(a, "--logfile=") {
			hasLogfile = true
		}
		args = append(args, a)
	}
	if !hasLogfile {
		args = append(args, "-L", defaultLog, "-logfile_by_day")
	}
	return args
}

// daemonDefaultLog is the logfile used when a daemon runs without an explicit
// -L. Kept as a var so tests can override.
var daemonDefaultLog = "/tmp/orzdba.log"
