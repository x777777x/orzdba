package rtcol

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFakeBin writes an executable script at path and returns its path.
func writeFakeBin(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "tcprstat-fake")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// withPaths overrides the collector's log/lck paths to temp files for isolation.
func withPaths(t *testing.T, c *Collector) {
	c.logPath = filepath.Join(t.TempDir(), "rt.log")
	c.lckPath = filepath.Join(t.TempDir(), "rt.lck")
}

func TestStartBinaryMissing(t *testing.T) {
	old := tcprstatBin
	tcprstatBin = "/nonexistent/path/tcprstat"
	defer func() { tcprstatBin = old }()
	c := New(3306, "127.0.0.1")
	withPaths(t, c)
	if err := c.Start(); err == nil {
		t.Error("Start with missing binary should error")
	}
}

func TestStartLockHeld(t *testing.T) {
	old := tcprstatBin
	tcprstatBin = writeFakeBin(t, "sleep 60")
	defer func() { tcprstatBin = old }()
	c := New(3306, "127.0.0.1")
	withPaths(t, c)
	// Pre-create the lock file owned by a LIVE process (ourselves) → the
	// port lock must refuse a second instance (P1-1).
	if f, err := os.OpenFile(c.lckPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); err != nil {
		t.Fatal(err)
	} else {
		_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
		f.Close()
	}
	if err := c.Start(); err == nil {
		t.Error("Start with a held lock should error")
	}
}

func TestStartStaleLockReclaimed(t *testing.T) {
	old := tcprstatBin
	tcprstatBin = writeFakeBin(t, "sleep 60")
	defer func() { tcprstatBin = old }()
	c := New(3306, "127.0.0.1")
	withPaths(t, c)
	// A lock owned by a dead PID (e.g. 999999, or an empty file) must be
	// reclaimed instead of blocking the start (P1-1 stale-lock recovery).
	if f, err := os.OpenFile(c.lckPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); err != nil {
		t.Fatal(err)
	} else {
		_, _ = fmt.Fprintf(f, "%d\n", 999999) // almost certainly not alive
		f.Close()
	}
	if err := c.Start(); err != nil {
		t.Fatalf("Start with a stale lock should reclaim and succeed, got: %v", err)
	}
	c.Stop()
}

func TestStartStopLifecycle(t *testing.T) {
	old := tcprstatBin
	tcprstatBin = writeFakeBin(t, "sleep 60")
	defer func() { tcprstatBin = old }()
	c := New(3306, "127.0.0.1")
	withPaths(t, c)

	if err := c.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if !c.started || c.cmd == nil || c.cmd.Process == nil {
		t.Error("Start did not set up the subprocess")
	}
	if _, err := os.Stat(c.lckPath); err != nil {
		t.Errorf("lock file not created: %v", err)
	}
	if fi, err := os.Stat(c.logPath); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("log file not 0600: %v %v", err, fi)
	}

	c.Stop()
	if c.started {
		t.Error("Stop should clear started")
	}
	if _, err := os.Stat(c.lckPath); err == nil {
		t.Error("lock file should be removed by Stop")
	}
	if _, err := os.Stat(c.logPath); err == nil {
		t.Error("log file should be removed by Stop")
	}
}

func TestStopIdempotent(t *testing.T) {
	// Stop on a never-started collector must not panic.
	c := New(3306, "127.0.0.1")
	withPaths(t, c)
	c.Stop() // no panic
}

func TestCollectNotStarted(t *testing.T) {
	c := New(3306, "127.0.0.1")
	withPaths(t, c)
	cells := c.Collect()
	want := "      0      0      0      0"
	if cells[0].Text != want {
		t.Errorf("not-started Collect = %q, want %q", cells[0].Text, want)
	}
}

func TestLastSampleParses(t *testing.T) {
	c := New(3306, "127.0.0.1")
	withPaths(t, c)
	// 13 cols: ts count max min avg med stddev max_95 avg_95 std_95 max_99 avg_99 std_99
	line := "1234567890 150 50000 100 1200 1100 50 48000 1150 40 49000 1180 30\n"
	if err := os.WriteFile(c.logPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	count, avg, avg95, avg99, ok := c.lastSample()
	if !ok {
		t.Fatal("lastSample returned !ok on a valid line")
	}
	if count != 150 || avg != 1200 || avg95 != 1150 || avg99 != 1180 {
		t.Errorf("lastSample = %d/%d/%d/%d, want 150/1200/1150/1180", count, avg, avg95, avg99)
	}
}

func TestLastSampleEmptyAndMissing(t *testing.T) {
	c := New(3306, "127.0.0.1")
	withPaths(t, c)
	// Empty file → not ok.
	if err := os.WriteFile(c.logPath, []byte("\n  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, ok := c.lastSample(); ok {
		t.Error("lastSample on empty file returned ok, want false")
	}
	// Missing file → not ok.
	os.Remove(c.logPath)
	if _, _, _, _, ok := c.lastSample(); ok {
		t.Error("lastSample on missing file returned ok, want false")
	}
}

func TestCollectCrashRestartsOnce(t *testing.T) {
	// A "tcprstat" that exits immediately → Collect must restart once, then
	// degrade to zeros on the second crash (§9.5).
	old := tcprstatBin
	tcprstatBin = writeFakeBin(t, "exit 0")
	defer func() { tcprstatBin = old }()
	c := New(3306, "127.0.0.1")
	withPaths(t, c)
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop()

	waitExited := func() {
		// The Wait goroutine reaps the (immediately-exiting) child and sets the
		// flag; under -race the goroutine can be slow to schedule, so allow a
		// generous deadline.
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) && !c.exited.Load() {
			time.Sleep(5 * time.Millisecond)
		}
		if !c.exited.Load() {
			t.Fatal("child did not report exited in time")
		}
	}

	waitExited()
	// First crash: restart budget 0 → restart, emit zeros.
	c.Collect()
	if c.restarts != 1 {
		t.Errorf("after first crash, restarts = %d, want 1", c.restarts)
	}
	// Second crash: budget exhausted → no restart.
	waitExited()
	c.Collect()
	if c.restarts != 1 {
		t.Errorf("after second crash, restarts = %d, want still 1 (no further restart)", c.restarts)
	}
}
