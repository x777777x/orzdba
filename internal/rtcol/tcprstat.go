// Package rtcol wraps the external tcprstat binary for DB response-time
// monitoring.
//
// The binary path is hardcoded to /usr/bin/tcprstat (no PATH lookup, no arg
// injection — plan §9.5) and the subprocess is tracked by PID for precise
// cleanup. There is no `killall`: on exit we SIGTERM *this instance's* child,
// wait 200ms, SIGKILL as fallback, then unlink its log/lock files (plan §9.5,
// fixing orzdba-go P0-5).
//
// Hardening (P1):
//   - The process lock is keyed by PORT (not pid), so two orzdba -rt instances
//     cannot both monitor the same port (previously a per-pid lock name made
//     the lock a no-op). The lock file stores the owning PID and is cleared
//     when that PID is no longer alive (stale-lock recovery after SIGKILL).
//   - All shared fields (cmd/started/restarts) are guarded by a mutex: the
//     signal handler may call Stop() while the main loop is inside Collect()/
//     restart(), so without a lock these were a data race.
//   - The tcprstat log is read from the TAIL (not the whole file, which grew
//     unboundedly over long runs) and truncated once it exceeds a threshold.
package rtcol

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"orzdba/internal/metric"
)

// tcprstatBin is the subprocess path. It's a var (not const) so tests can
// point it at a fake script and exercise Start/Stop without the real binary
// (plan §9.5: no PATH lookup, prevents PATH injection).
var tcprstatBin = "/usr/bin/tcprstat"

// tcprstatLogMax is the size (bytes) at which the log is truncated. At ~100
// bytes/line this is ~2 hours of 1s samples — far more than the reader needs,
// and it bounds both disk usage and per-tick read cost (P1-2).
const tcprstatLogMax = 1 << 20 // 1 MiB

// Collector runs one tcprstat subprocess and reports count/avg/avg_95/avg_99
// per tick. It implements render.Collector in the mysql group (green '|').
type Collector struct {
	port, ip string

	mu       sync.Mutex
	cmd      *exec.Cmd
	started  bool
	restarts int         // crash-restart budget (§9.5: abandon after 1 retry)
	exited   atomic.Bool // set by the Wait goroutine when the child dies
	logPath  string
	lckPath  string // /tmp/orzdba_tcprstat.p<port>.lck (port-keyed, P1-1)
}

// New returns an RT collector for the given MySQL port and listen IP.
// The lock is keyed by port so two instances cannot double-monitor a port.
func New(port int, ip string) *Collector {
	pid := os.Getpid()
	return &Collector{
		port:    strconv.Itoa(port),
		ip:      ip,
		logPath: fmt.Sprintf("/tmp/orzdba_tcprstat.%d.log", pid),
		lckPath: fmt.Sprintf("/tmp/orzdba_tcprstat.p%d.lck", port),
	}
}

func (*Collector) Name() string { return "rt" }

func (*Collector) Headline() (string, string) {
	return "--------tcprstat(us)-------- ", "  count    avg 95-avg 99-avg|"
}

// Start verifies tcprstat exists, takes the port lock, and launches the
// subprocess with stdout redirected to a 0600 log file. Returns an error
// (main exits, plan §11.1) if tcprstat is missing or the port is locked by a
// live process.
func (c *Collector) Start() error {
	if _, err := os.Stat(tcprstatBin); err != nil {
		return fmt.Errorf("tcprstat not found at %s (−rt is Linux-only and needs the tcprstat binary)", tcprstatBin)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.acquireLock(); err != nil {
		return err
	}
	if err := c.launchLocked(); err != nil {
		_ = os.Remove(c.lckPath)
		return err
	}
	return nil
}

// acquireLock takes the port lock (O_CREATE|O_EXCL). If a stale lock exists
// from a crashed process, it recovers by checking whether the recorded PID is
// still alive (signal 0). P1-1: previously the lock was per-pid and could
// never guard a second instance; now it is per-port and self-healing.
func (c *Collector) acquireLock() error {
	lck, err := os.OpenFile(c.lckPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		// Write our PID so a future instance can detect a stale lock.
		_, _ = fmt.Fprintf(lck, "%d\n", os.Getpid())
		return lck.Close()
	}
	if !os.IsExist(err) {
		return fmt.Errorf("cannot create tcprstat lock %s: %w", c.lckPath, err)
	}
	// Lock exists - is its owner still alive? (procAlive: signal 0 on Unix.)
	if pid, ok := readLockPID(c.lckPath); ok {
		if procAlive(pid) {
			return fmt.Errorf("cannot acquire tcprstat lock %s: another orzdba -rt instance (pid %d) monitors port %s", c.lckPath, pid, c.port)
		}
		// Stale lock: owner is gone. Reclaim it.
		_ = os.Remove(c.lckPath)
		if lck, err := os.OpenFile(c.lckPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); err == nil {
			_, _ = fmt.Fprintf(lck, "%d\n", os.Getpid())
			return lck.Close()
		}
	}
	return fmt.Errorf("cannot acquire tcprstat lock %s (stale lock, remove manually if needed)", c.lckPath)
}

// launchLocked opens the log file (truncating) and starts the subprocess.
// Caller must hold c.mu and must remove the lock on failure.
func (c *Collector) launchLocked() error {
	logFile, err := os.OpenFile(c.logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("cannot open tcprstat log %s: %w", c.logPath, err)
	}
	_ = os.Chmod(c.logPath, 0o600) // fallback in case a permissive existing file widened it (§9.6)

	c.cmd = exec.Command(tcprstatBin, "--no-header", "-t", "1", "-n", "0", "-p", c.port, "-l", c.ip)
	c.cmd.Stdout = logFile
	c.cmd.Stderr = nil // discard tcprstat stderr (§9.5)
	if err := c.cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("tcprstat start failed: %w", err)
	}
	// The child dup'd the fd; the parent's handle can be closed (avoids an
	// fd leak for the process lifetime). Reaps the child on exit and sets the
	// exited flag so Collect detects a crash (a zombie would still answer
	// signal 0, masking the crash — plan §9.5 crash detection).
	logFile.Close()
	c.exited.Store(false)
	c.restarts = 0
	go func() { _ = c.cmd.Wait(); c.exited.Store(true) }()
	c.started = true
	return nil
}

// Collect reads the last tcprstat log line and renders the 4 RT columns.
// On crash it retries once (§9.5); further crashes degrade to zeros.
func (c *Collector) Collect() []metric.Cell {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.started || c.cmd == nil || c.cmd.Process == nil {
		return zeroRT()
	}
	if c.exited.Load() {
		if c.restarts == 0 {
			c.restarts++
			_ = c.restartLocked()
		}
		// Regardless of restart outcome, this tick has no fresh line → zeros.
		return zeroRT()
	}
	count, avg, avg95, avg99, ok := c.lastSampleLocked()
	if !ok {
		return zeroRT()
	}
	return []metric.Cell{
		{Text: fmt.Sprintf(" %6d", count), Color: metric.White},
		{Text: fmt.Sprintf(" %6d", avg), Color: rtColor(avg)},
		{Text: fmt.Sprintf(" %6d", avg95), Color: rtColor(avg95)},
		{Text: fmt.Sprintf(" %6d", avg99), Color: rtColor(avg99)},
	}
}

// restartLocked re-launches tcprstat after a crash (one-shot, called from
// Collect under c.mu).
func (c *Collector) restartLocked() error {
	logFile, err := os.OpenFile(c.logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_ = os.Chmod(c.logPath, 0o600)
	c.cmd = exec.Command(tcprstatBin, "--no-header", "-t", "1", "-n", "0", "-p", c.port, "-l", c.ip)
	c.cmd.Stdout = logFile
	c.cmd.Stderr = nil
	if err := c.cmd.Start(); err != nil {
		logFile.Close()
		return err
	}
	logFile.Close()
	c.exited.Store(false)
	go func() { _ = c.cmd.Wait(); c.exited.Store(true) }()
	return nil
}

// lastSample is a lock-free wrapper for tests/external callers that just want
// the current log tail (no concurrency with Collect). Production uses
// lastSampleLocked under c.mu.
func (c *Collector) lastSample() (count, avg, avg95, avg99 int64, ok bool) {
	return c.lastSampleLocked()
}

// lastSampleLocked reads the tail of the log file, parses the last non-empty
// line, and truncates the file once it exceeds tcprstatLogMax (P1-2: bounds
// both disk usage and per-tick read cost). Caller holds c.mu.
func (c *Collector) lastSampleLocked() (count, avg, avg95, avg99 int64, ok bool) {
	f, err := os.Open(c.logPath)
	if err != nil {
		return 0, 0, 0, 0, false
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return 0, 0, 0, 0, false
	}
	// Read only the last 64 KiB (or the whole file if smaller).
	readSize := int64(64 << 10)
	if fi.Size() < readSize {
		readSize = fi.Size()
	}
	if readSize == 0 {
		return 0, 0, 0, 0, false
	}
	buf := make([]byte, readSize)
	if _, err := f.Seek(fi.Size()-readSize, 0); err != nil {
		return 0, 0, 0, 0, false
	}
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return 0, 0, 0, 0, false
	}
	last := lastNonEmptyLine(buf[:n])
	if last == "" {
		return 0, 0, 0, 0, false
	}
	// Truncate the file to keep it bounded (keep the tail window we just read).
	if fi.Size() > tcprstatLogMax {
		_ = f.Truncate(0)
		_, _ = f.Seek(0, 0)
	}
	return parseRTLine(last)
}

// lastNonEmptyLine returns the last non-empty line in buf.
func lastNonEmptyLine(buf []byte) string {
	lines := bytes.Split(buf, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(string(lines[i])); s != "" {
			return s
		}
	}
	return ""
}

// Stop terminates the subprocess and removes log/lock files. Idempotent.
// The Start/restart Wait goroutine reaps the child, so we signal and poll the
// exited flag rather than calling Wait again (a second Wait would error).
func (c *Collector) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd != nil && c.cmd.Process != nil && !c.exited.Load() {
		_ = c.cmd.Process.Signal(syscall.SIGTERM)
		// Wait up to 200ms for graceful exit, then SIGKILL (§9.5).
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) && !c.exited.Load() {
			time.Sleep(5 * time.Millisecond)
		}
		if !c.exited.Load() {
			_ = c.cmd.Process.Signal(syscall.SIGKILL)
		}
	}
	// Let the Wait goroutine finish reaping (esp. after SIGKILL).
	for i := 0; i < 40 && !c.exited.Load(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	_ = os.Remove(c.logPath)
	_ = os.Remove(c.lckPath)
	c.started = false
}

// parseRTLine extracts count, avg, avg_95, avg_99 from a tcprstat output line.
// tcprstat emits 13 whitespace-separated columns:
//
//	timestamp count max min avg med stddev max_95 avg_95 std_95 max_99 avg_99 std_99
//
// so 0-indexed: count=1, avg=4, avg_95=8, avg_99=11.
func parseRTLine(line string) (count, avg, avg95, avg99 int64, ok bool) {
	f := strings.Fields(line)
	if len(f) < 12 {
		return 0, 0, 0, 0, false
	}
	count = atoi64(f[1])
	avg = atoi64(f[4])
	avg95 = atoi64(f[8])
	avg99 = atoi64(f[11])
	return count, avg, avg95, avg99, true
}

// rtColor: >10000 Red else Green (Perl).
func rtColor(v int64) metric.Color {
	if v > 10000 {
		return metric.Red
	}
	return metric.Green
}

func zeroRT() []metric.Cell {
	return []metric.Cell{{Text: fmt.Sprintf(" %6d %6d %6d %6d", 0, 0, 0, 0), Color: metric.White}}
}

// atoi64 parses an int, 0 on failure.
func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}
