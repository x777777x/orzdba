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
	// --also-stdout writes rows to stdout, which is /dev/null in the daemon —
	// say so instead of letting the flag silently do nothing.
	for _, a := range os.Args[1:] {
		if a == "--also-stdout" || a == "-also-stdout" {
			fmt.Fprintln(os.Stderr, "note: --also-stdout has no effect under --daemon (the daemon's stdout is /dev/null)")
			break
		}
	}
	// The rebuilt argv always carries a logfile (the default is injected when
	// the user gave none). Pre-open it here as the child's stderr so startup
	// failures — unwritable path, MySQL down, bad -d — are diagnosable:
	// they used to vanish into /dev/null while the parent already reported
	// success. An unwritable log path also fails fast HERE instead of
	// silently in the child. (With -logfile_by_day the child's data goes to
	// the dated file; stderr diagnostics land in the bare path.)
	logPath := findLogfileArg(args)
	if logPath == "" {
		logPath = daemonDefaultLog // defensive: injection guarantees a value
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("daemonize: cannot open logfile %s: %w", logPath, err)
	}
	defer logFile.Close()
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("daemonize: cannot open %s: %w", os.DevNull, err)
	}
	defer devnull.Close()

	cmd := exec.Command(exe, args...)
	cmd.Stdin = devnull
	cmd.Stdout = devnull
	cmd.Stderr = logFile
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

// isLogfileFlag reports whether the argv token is the logfile flag in any
// accepted spelling: -L / --logfile / -logfile (single-dash via longFlagNames)
// plus the "-L=" / "--logfile=" equals forms. This predicate is the single
// source of truth for that surface — daemonChildArgs (whether to inject the
// default log) and daemonize (which path to pre-open) must agree on it, which
// is exactly where they used to drift. ("-logfile=" is not pflag-legal.)
func isLogfileFlag(a string) bool {
	return a == "-L" || a == "--logfile" || a == "-logfile" ||
		strings.HasPrefix(a, "-L=") || strings.HasPrefix(a, "--logfile=")
}

// findLogfileArg returns the logfile path carried by argv (any accepted
// spelling), or "" when the flag is absent. Space-separated forms take the
// next token as the value.
func findLogfileArg(argv []string) string {
	for i, a := range argv {
		if !isLogfileFlag(a) {
			continue
		}
		if strings.HasPrefix(a, "-L=") {
			return strings.TrimPrefix(a, "-L=")
		}
		if strings.HasPrefix(a, "--logfile=") {
			return strings.TrimPrefix(a, "--logfile=")
		}
		if i+1 < len(argv) {
			return argv[i+1]
		}
		return ""
	}
	return ""
}

// daemonChildArgs rebuilds the daemon child's argv: it strips --daemon and,
// when no logfile flag is present, appends the default log path (with daily
// rotation) so the daemon persists output. Pure so it can be unit-tested.
func daemonChildArgs(argv []string, defaultLog string) []string {
	args := make([]string, 0, len(argv)+2)
	hasLogfile := false
	for _, a := range argv {
		// Strip the daemon flag in every form the parser accepts: "-daemon"
		// (Perl-style single dash — longFlagNames whitelists it, so
		// normalizeArgs turns it into --daemon) and "--daemon" /
		// "--daemon=<bool>" (pflag ParseBool accepts 1/t/T/TRUE/...). Missing
		// any accepted form leaves the flag in the child's argv, and the
		// child daemonizes again — an endless fork/exec loop.
		if a == "-daemon" || a == "--daemon" || strings.HasPrefix(a, "--daemon=") {
			continue
		}
		if isLogfileFlag(a) {
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
