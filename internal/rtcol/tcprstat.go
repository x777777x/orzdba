// Package rtcol wraps the external tcprstat binary for DB response-time
// monitoring.
//
// The binary path is hardcoded to /usr/bin/tcprstat (no PATH lookup, no arg
// injection — plan §9.5) and the subprocess is tracked by PID for precise
// cleanup. There is no `killall`: on exit we SIGTERM *this instance's* child,
// wait 200ms, SIGKILL as fallback, then unlink its log/lock files (plan §9.5,
// fixing orzdba-go P0-5).
package rtcol

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"orzdba/internal/metric"
)

// tcprstatBin is the subprocess path. It's a var (not const) so tests can
// point it at a fake script and exercise Start/Stop without the real binary
// (plan §9.5: no PATH lookup, prevents PATH injection).
var tcprstatBin = "/usr/bin/tcprstat"

// Collector runs one tcprstat subprocess and reports count/avg/avg_95/avg_99
// per tick. It implements render.Collector in the mysql group (green '|').
type Collector struct {
	port, ip         string
	cmd              *exec.Cmd
	logPath, lckPath string
	started          bool
	restarts         int         // crash-restart budget (§9.5: abandon after 1 retry)
	exited           atomic.Bool // set by the Wait goroutine when the child dies
}

// New returns an RT collector for the given MySQL port and listen IP.
func New(port int, ip string) *Collector {
	pid := os.Getpid()
	return &Collector{
		port:    strconv.Itoa(port),
		ip:      ip,
		logPath: fmt.Sprintf("/tmp/orzdba_tcprstat.%d.log", pid),
		lckPath: fmt.Sprintf("/tmp/orzdba_tcprstat.%d.lck", pid),
	}
}

func (*Collector) Name() string { return "rt" }

func (*Collector) Headline() (string, string) {
	return "--------tcprstat(us)-------- ", "  count    avg 95-avg 99-avg|"
}

// Start verifies tcprstat exists, takes the process lock, and launches the
// subprocess with stdout redirected to a 0600 log file. Returns an error
// (main exits, plan §11.1) if tcprstat is missing or the lock is held.
func (c *Collector) Start() error {
	if _, err := os.Stat(tcprstatBin); err != nil {
		return fmt.Errorf("tcprstat not found at %s (−rt is Linux-only and needs the tcprstat binary)", tcprstatBin)
	}
	// Process lock: O_CREAT|O_EXCL — fails if another orzdba instance owns it.
	lck, err := os.OpenFile(c.lckPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("cannot acquire tcprstat lock %s: %w (another orzdba -rt instance?)", c.lckPath, err)
	}
	_ = lck.Close()

	logFile, err := os.OpenFile(c.logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		_ = os.Remove(c.lckPath)
		return fmt.Errorf("cannot open tcprstat log %s: %w", c.logPath, err)
	}
	_ = os.Chmod(c.logPath, 0o600) // fallback in case a permissive existing file widened it (§9.6)

	c.cmd = exec.Command(tcprstatBin, "--no-header", "-t", "1", "-n", "0", "-p", c.port, "-l", c.ip)
	c.cmd.Stdout = logFile
	c.cmd.Stderr = nil // discard tcprstat stderr (§9.5)
	if err := c.cmd.Start(); err != nil {
		logFile.Close()
		_ = os.Remove(c.lckPath)
		return fmt.Errorf("tcprstat start failed: %w", err)
	}
	// The child dup'd the fd; the parent's handle can be closed (avoids an
	// fd leak for the process lifetime). Reaps the child on exit and sets the
	// exited flag so Collect detects a crash (a zombie would still answer
	// signal 0, masking the crash — plan §9.5 crash detection).
	logFile.Close()
	c.exited.Store(false)
	go func() { _ = c.cmd.Wait(); c.exited.Store(true) }()
	c.started = true
	return nil
}

// Collect reads the last tcprstat log line and renders the 4 RT columns.
// On crash it retries once (§9.5); further crashes degrade to zeros.
func (c *Collector) Collect() []metric.Cell {
	if !c.started || c.cmd == nil || c.cmd.Process == nil {
		return zeroRT()
	}
	if c.exited.Load() {
		if c.restarts == 0 {
			c.restarts++
			_ = c.restart()
		}
		// Regardless of restart outcome, this tick has no fresh line → zeros.
		return zeroRT()
	}
	count, avg, avg95, avg99, ok := c.lastSample()
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

// restart re-launches tcprstat after a crash (one-shot, called from Collect).
func (c *Collector) restart() error {
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

// lastSample reads the log file's last non-empty line and parses it.
func (c *Collector) lastSample() (count, avg, avg95, avg99 int64, ok bool) {
	data, err := os.ReadFile(c.logPath)
	if err != nil {
		return 0, 0, 0, 0, false
	}
	var last string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			last = line
		}
	}
	if last == "" {
		return 0, 0, 0, 0, false
	}
	return parseRTLine(last)
}

// Stop terminates the subprocess and removes log/lock files. Idempotent.
// The Start/restart Wait goroutine reaps the child, so we signal and poll the
// exited flag rather than calling Wait again (a second Wait would error).
func (c *Collector) Stop() {
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
