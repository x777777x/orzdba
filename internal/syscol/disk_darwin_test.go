//go:build darwin

package syscol

import (
	"testing"
	"time"

	"orzdba/internal/metric"
)

// TestDarwinDiskRatesArePerSecond pins the core fix: the IOKit counter deltas
// must be divided by the elapsed window, so the r/s, w/s, rkB/s, wkB/s columns
// are per-second regardless of the sampling interval (previously they were the
// raw per-tick deltas, i.e. N times too high at -i N).
func TestDarwinDiskRatesArePerSecond(t *testing.T) {
	d := NewDisk(nil, []string{"disk0"}, 1, false, metric.UnitRaw, 1)
	prev := diskStat{rdBytes: 1000, wrBytes: 2000, rdOps: 10, wrOps: 20}
	cur := diskStat{rdBytes: 1000 + 4096, wrBytes: 2000 + 8192, rdOps: 10 + 4, wrOps: 20 + 8}

	cells := d.deviceCells(cur, prev, 2) // 2-second window → halve the deltas
	if cells[0].Raw != 2 {               // 4 read ops / 2s
		t.Errorf("r/s Raw = %v, want 2", cells[0].Raw)
	}
	if cells[1].Raw != 2048 { // 4096 B / 2s
		t.Errorf("rd B/s Raw = %v, want 2048", cells[1].Raw)
	}
	if cells[2].Raw != 4096 { // 8192 B / 2s
		t.Errorf("wr B/s Raw = %v, want 4096", cells[2].Raw)
	}
}

// TestDarwinDiskFirstTickZerosThenRates: the first successful sample only
// records the baseline and prints zeros (no since-boot spike); the next tick
// rates the counter delta over the real elapsed window.
func TestDarwinDiskFirstTickZerosThenRates(t *testing.T) {
	now := time.Unix(1000, 0)
	d := NewDisk(nil, []string{"disk0"}, 1, false, metric.UnitRaw, 1)
	d.nowFn = func() time.Time { return now }
	d.readFn = func() map[string]diskStat {
		return map[string]diskStat{"disk0": {rdOps: 10, wrOps: 20, rdBytes: 1000, wrBytes: 2000}}
	}

	cells := d.Collect()
	for i, c := range cells {
		if c.Raw != 0 {
			t.Fatalf("first tick cell %d Raw = %v, want 0", i, c.Raw)
		}
	}

	now = now.Add(time.Second)
	d.readFn = func() map[string]diskStat {
		return map[string]diskStat{"disk0": {rdOps: 15, wrOps: 25, rdBytes: 1000 + 1024, wrBytes: 2000 + 3072}}
	}
	cells = d.Collect()
	if cells[0].Raw != 5 { // 5 read ops / 1s
		t.Errorf("second tick r/s Raw = %v, want 5", cells[0].Raw)
	}
	if cells[1].Raw != 1024 { // 1024 B / 1s
		t.Errorf("second tick rd B/s Raw = %v, want 1024", cells[1].Raw)
	}
}

// TestDarwinDiskFailedTickKeepsBaseline: one unreadable IOKit tick must zero
// the columns but keep the baseline and the sample clock, so the recovery tick
// rates over the true outage window (not a since-boot spike).
func TestDarwinDiskFailedTickKeepsBaseline(t *testing.T) {
	now := time.Unix(1000, 0)
	d := NewDisk(nil, []string{"disk0"}, 1, false, metric.UnitRaw, 1)
	d.nowFn = func() time.Time { return now }
	d.readFn = func() map[string]diskStat {
		return map[string]diskStat{"disk0": {rdOps: 10}}
	}
	d.Collect() // baseline at t=1000

	now = now.Add(time.Second)
	d.readFn = func() map[string]diskStat { return nil } // failed tick at t=1001
	cells := d.Collect()
	for i, c := range cells {
		if c.Raw != 0 {
			t.Fatalf("failed tick cell %d Raw = %v, want 0", i, c.Raw)
		}
	}

	now = now.Add(time.Second) // recovery at t=1002 → 2s after the baseline
	d.readFn = func() map[string]diskStat {
		return map[string]diskStat{"disk0": {rdOps: 20}}
	}
	cells = d.Collect()
	if cells[0].Raw != 5 { // 10 ops / 2s, over the outage window
		t.Errorf("recovery r/s Raw = %v, want 5 (delta over the 2s window)", cells[0].Raw)
	}
}
