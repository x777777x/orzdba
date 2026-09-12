//go:build !darwin

package syscol

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"orzdba/internal/metric"
)

// mustRead loads a testdata file under testdata/proc.
func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "proc", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return b
}

// bytesReader wraps a []byte as an io.Reader for the parse helpers that take
// io.Reader (CountCPU).
func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

func TestCountCPU(t *testing.T) {
	n := CountCPU(bytesReader(mustRead(t, "cpuinfo.txt")))
	if n != 2 {
		t.Fatalf("CountCPU = %d, want 2", n)
	}
}

func TestCountCPUEmpty(t *testing.T) {
	if n := CountCPU(bytesReader(nil)); n != 1 {
		t.Fatalf("CountCPU(empty) = %d, want 1 fallback", n)
	}
}

func TestParseCPUStat(t *testing.T) {
	v, ok := parseCPUStat(mustRead(t, "stat_tick1.txt"))
	if !ok {
		t.Fatal("parseCPUStat returned !ok")
	}
	want := [7]uint64{100, 0, 100, 800, 0, 0, 0}
	if v != want {
		t.Fatalf("parseCPUStat = %v, want %v", v, want)
	}
}

func TestCPUSecondTick(t *testing.T) {
	c := NewCPU(2, true, false)
	c.consume(mustRead(t, "stat_tick1.txt")) // since-boot baseline
	c.consume(mustRead(t, "stat_tick2.txt")) // diffs: usr=10 sys=5 idl=85 iow=0
	cells := c.Collect()
	if len(cells) != 4 {
		t.Fatalf("got %d cells, want 4", len(cells))
	}
	cases := []struct {
		text string
		col  metric.Color
	}{
		{" 10", metric.Green},  // usr=10, not >10 → green
		{"   5", metric.White}, // sys=5
		{"  85", metric.White}, // idl=85
		{"   0", metric.Green}, // iow=0
	}
	for i, tc := range cases {
		if cells[i].Text != tc.text {
			t.Errorf("cell %d text = %q, want %q", i, cells[i].Text, tc.text)
		}
		if cells[i].Color != tc.col {
			t.Errorf("cell %d color = %v, want %v", i, cells[i].Color, tc.col)
		}
	}
}

func TestParseVMStatSwap(t *testing.T) {
	in, out := parseVMStatSwap(mustRead(t, "vmstat_tick1.txt"))
	if in != 50 || out != 30 {
		t.Fatalf("parseVMStatSwap = (%d,%d), want (50,30)", in, out)
	}
}

func TestSwapFirstTickZero(t *testing.T) {
	s := NewSwap(1)
	cells := s.consume(mustRead(t, "vmstat_tick1.txt"))
	if len(cells) != 2 || cells[0].Text != "    0" || cells[1].Text != "    0" {
		t.Fatalf("first-tick = %v, want two \"    0\"", cells)
	}
}

func TestSwapSecondTick(t *testing.T) {
	s := NewSwap(1)
	s.consume(mustRead(t, "vmstat_tick1.txt"))
	cells := s.consume(mustRead(t, "vmstat_tick2.txt"))
	// pswpin 50→75 (delta 25, RED), pswpout 30→42 (delta 12, RED)
	if cells[0].Text != "   25" || cells[0].Color != metric.Red {
		t.Errorf("si = %q/%v, want \"   25\"/Red", cells[0].Text, cells[0].Color)
	}
	if cells[1].Text != "   12" || cells[1].Color != metric.Red {
		t.Errorf("so = %q/%v, want \"   12\"/Red", cells[1].Text, cells[1].Color)
	}
}

func TestParseNetDev(t *testing.T) {
	recv, send := parseNetDev(mustRead(t, "netdev_tick1.txt"), "eth0")
	if recv != 1048576 || send != 2097152 {
		t.Fatalf("parseNetDev = (%d,%d), want (1048576,2097152)", recv, send)
	}
}

func TestParseNetDevMiss(t *testing.T) {
	recv, send := parseNetDev(mustRead(t, "netdev_tick1.txt"), "wlan9")
	if recv != 0 || send != 0 {
		t.Fatalf("missing dev = (%d,%d), want (0,0)", recv, send)
	}
}

func TestNetSecondTick(t *testing.T) {
	// UnitRaw (default): raw byte values, ES-friendly. Leading space keeps the
	// column separated from its neighbor (run-on fix).
	n := NewNet("eth0", 1, false, metric.UnitRaw)
	n.consume(mustRead(t, "netdev_tick1.txt"))
	cells := n.consume(mustRead(t, "netdev_tick2.txt"))
	// recv delta 1572864 bytes/s → raw " 1572864", RED (rate > 1MiB/s)
	// send delta 1048576 bytes/s → raw " 1048576", WHITE
	if cells[0].Text != " 1572864" || cells[0].Color != metric.Red {
		t.Errorf("recv raw = %q/%v, want \" 1572864\"/Red", cells[0].Text, cells[0].Color)
	}
	if cells[0].Raw != 1572864 {
		t.Errorf("recv Raw = %v, want 1572864", cells[0].Raw)
	}
	if cells[1].Text != " 1048576" || cells[1].Color != metric.White {
		t.Errorf("send raw = %q/%v, want \" 1048576\"/White", cells[1].Text, cells[1].Color)
	}
	if cells[1].Raw != 1048576 {
		t.Errorf("send Raw = %v, want 1048576", cells[1].Raw)
	}
}

func TestNetSecondTickHuman(t *testing.T) {
	// UnitHuman: k/m suffixes (Perl-compatible display), leading space.
	n := NewNet("eth0", 1, false, metric.UnitHuman)
	n.consume(mustRead(t, "netdev_tick1.txt"))
	cells := n.consume(mustRead(t, "netdev_tick2.txt"))
	if cells[0].Text != "    1.5m" || cells[0].Color != metric.Red {
		t.Errorf("recv human = %q/%v, want \"    1.5m\"/Red", cells[0].Text, cells[0].Color)
	}
	if cells[1].Text != "   1024k" || cells[1].Color != metric.White {
		t.Errorf("send human = %q/%v, want \"   1024k\"/White", cells[1].Text, cells[1].Color)
	}
}

func TestParseDiskStat(t *testing.T) {
	s := parseDiskStat(mustRead(t, "diskstats_tick1.txt"), "sda")
	if s.rdIOS != 100 || s.rdSectors != 10000 || s.wrIOS != 50 || s.wrSectors != 5000 {
		t.Fatalf("parseDiskStat sda = %+v", s)
	}
	if s.totTicks != 1000 || s.aveq != 500 {
		t.Fatalf("parseDiskStat totTicks/aveq = %d/%d, want 1000/500", s.totTicks, s.aveq)
	}
}

func TestParseDiskStatPartition(t *testing.T) {
	// sda1 must be matched exactly, not conflated with sda.
	s := parseDiskStat(mustRead(t, "diskstats_tick1.txt"), "sda1")
	if s.rdIOS != 50 {
		t.Fatalf("sda1 rdIOS = %d, want 50 (exact match)", s.rdIOS)
	}
}

func TestDiskSecondTick(t *testing.T) {
	cpu := NewCPU(2, false, false)
	d := NewDisk(cpu, []string{"sda"}, 2, false, metric.UnitRaw)
	// Interleave cpu+disk per tick so deltams reflects the right cpu diffs.
	cpu.consume(mustRead(t, "stat_tick1.txt"))
	d.consume(mustRead(t, "diskstats_tick1.txt")) // first tick (since-boot), not asserted
	cpu.consume(mustRead(t, "stat_tick2.txt"))
	cells := d.consume(mustRead(t, "diskstats_tick2.txt"))
	// deltams = 1000*(10+5+85+0)/2/100 = 500
	want := []struct {
		text string
		col  metric.Color
	}{
		{"  120.0   80.0", metric.White}, // rd_ios_s=120, wr_ios_s=80
		{"  2000.0", metric.Red},         // rkbs=2000 > 1024
		{"   1000.0", metric.White},      // wkbs=1000, not > 1024
		{"   0.2", metric.White},         // queue=0.2
		{"   10.0", metric.Red},          // wait=10.0 > 5
		{"   5.0", metric.White},         // svc_t=5.0, not > 5
		{" 100.0", metric.Red},           // busy=100 > 80
	}
	if len(cells) != len(want) {
		t.Fatalf("got %d cells, want %d", len(cells), len(want))
	}
	for i, w := range want {
		if cells[i].Text != w.text {
			t.Errorf("cell %d text = %q, want %q", i, cells[i].Text, w.text)
		}
		if cells[i].Color != w.col {
			t.Errorf("cell %d color = %v, want %v", i, cells[i].Color, w.col)
		}
	}
}

// ---- mem ----

func TestParseMemInfo(t *testing.T) {
	m := parseMemInfo(mustRead(t, "meminfo_tick1.txt"))
	if !m.ok || m.total != 16384000 || m.available != 8000000 {
		t.Fatalf("parseMemInfo = %+v", m)
	}
}

func TestMemUsage(t *testing.T) {
	m := parseMemInfo(mustRead(t, "meminfo_tick1.txt"))
	// usage = (16384000-8000000)/16384000*100 = 51.17%
	if u := m.usage(); u < 51 || u > 52 {
		t.Errorf("mem usage = %v, want ~51.2", u)
	}
}

func TestMemCollectDefault(t *testing.T) {
	c := NewMem(false, metric.UnitRaw)
	cells := c.consume(mustRead(t, "meminfo_tick1.txt"))
	if len(cells) != 1 {
		t.Fatalf("default mem = %d cells, want 1", len(cells))
	}
	if cells[0].Raw < 51 || cells[0].Raw > 52 {
		t.Errorf("default mem usage Raw = %v, want ~51.2", cells[0].Raw)
	}
}

func TestMemCollectFull(t *testing.T) {
	c := NewMem(true, metric.UnitRaw)
	cells := c.consume(mustRead(t, "meminfo_tick1.txt"))
	if len(cells) != 7 {
		t.Fatalf("full mem = %d cells, want 7", len(cells))
	}
	// total Raw = 16384000 kB * 1024 = 16777216000 bytes
	if cells[1].Raw != 16384000*1024 {
		t.Errorf("total Raw = %v, want %v", cells[1].Raw, 16384000*1024)
	}
	// used = total - available (the SAME definition usage% is computed from,
	// NOT total-free which double-counted page cache and contradicted the
	// usage column on the same row). Fixture: MemAvailable = 8000000 kB.
	if cells[2].Raw != float64((16384000-8000000)*1024) {
		t.Errorf("used Raw = %v, want %v (total - MemAvailable)", cells[2].Raw, (16384000-8000000)*1024)
	}
	if cells[4].Raw != float64(8000000*1024) {
		t.Errorf("avail Raw = %v, want %v", cells[4].Raw, 8000000*1024)
	}
}

func TestMemMissingDegrade(t *testing.T) {
	c := NewMem(false, metric.UnitRaw)
	cells := c.consume(nil)
	if len(cells) != 1 || cells[0].Raw != 0 {
		t.Errorf("missing meminfo = %v, want single 0 cell", cells)
	}
}

// ---- multi-disk ----

func TestParseDiskStatsMulti(t *testing.T) {
	m := parseDiskStats(mustRead(t, "diskstats_multi_tick1.txt"))
	if len(m) != 3 { // sda, sda1, sdb
		t.Fatalf("parseDiskStatsMulti = %d devices, want 3: %v", len(m), m)
	}
	if s, ok := m["sdb"]; !ok || s.rdIOS != 60 {
		t.Errorf("sdb = %+v, want rdIOS=60", m["sdb"])
	}
}

func TestDiskMultiSecondTick(t *testing.T) {
	cpu := NewCPU(2, false, false)
	d := NewDisk(cpu, []string{"sda", "sdb"}, 2, false, metric.UnitRaw)
	cpu.consume(mustRead(t, "stat_tick1.txt"))
	d.consume(mustRead(t, "diskstats_multi_tick1.txt")) // baseline
	cpu.consume(mustRead(t, "stat_tick2.txt"))
	cells := d.consume(mustRead(t, "diskstats_multi_tick2.txt"))
	// 2 devices × 7 columns = 14 cells
	if len(cells) != 14 {
		t.Fatalf("multi-disk cells = %d, want 14", len(cells))
	}
	// sda: rd_ios_s = 1000*(160-100)/500 = 120
	if cells[0].Raw != 120 {
		t.Errorf("sda r/s Raw = %v, want 120", cells[0].Raw)
	}
	// sdb: rd_ios_s = 1000*(100-60)/500 = 80 (cell index 7)
	if cells[7].Raw != 80 {
		t.Errorf("sdb r/s Raw = %v, want 80", cells[7].Raw)
	}
}

// ---- full-mode CPU ----

func TestCPUSecondTickFull(t *testing.T) {
	c := NewCPU(2, true, true)
	c.consume(mustRead(t, "stat_tick1.txt")) // baseline
	c.consume(mustRead(t, "stat_tick2.txt")) // diffs: usr=10 sys=5 idl=85 iow=0
	cells := c.Collect()
	if len(cells) != 8 {
		t.Fatalf("full cpu = %d cells, want 8", len(cells))
	}
	// usr = user+nice = 10
	if cells[0].Raw < 9.9 || cells[0].Raw > 10.1 {
		t.Errorf("full cpu usr Raw = %v, want ~10", cells[0].Raw)
	}
}

// ---- full-mode net ----

func TestNetFullSecondTick(t *testing.T) {
	n := NewNet("eth0", 1, true, metric.UnitRaw)
	n.consume(mustRead(t, "netdev_tick1.txt"))
	cells := n.consume(mustRead(t, "netdev_tick2.txt"))
	if len(cells) != 8 {
		t.Fatalf("full net = %d cells, want 8", len(cells))
	}
	// rxbytes rate = 1572864
	if cells[0].Raw != 1572864 {
		t.Errorf("full net rxbytes Raw = %v, want 1572864", cells[0].Raw)
	}
	// txbytes rate = 1048576 (cell 4)
	if cells[4].Raw != 1048576 {
		t.Errorf("full net txbytes Raw = %v, want 1048576", cells[4].Raw)
	}
}

// ---- counter-reset guard (clamp0): a shrank counter must yield 0, not -N ----

// netDevLine builds a /proc/net/dev-style line: "name:" plus 16 counters
// (parseNetDevFull keeps the colon attached to the name and splits on
// whitespace, so the counters land at fixed indices regardless of name length).
func netDevLine(dev string, counters [16]uint64) []byte {
	fields := make([]string, 17)
	fields[0] = dev + ":"
	for i, v := range counters {
		fields[i+1] = strconv.FormatUint(v, 10)
	}
	return []byte(strings.Join(fields, " ") + "\n")
}

func TestNetCounterResetClampsToZero(t *testing.T) {
	// Interface down/up (or veth recreate) zeroes rx/tx between ticks: the
	// raw delta is negative and must clamp to 0, not print -N.
	var high [16]uint64
	high[0], high[8] = 1048576, 2097152 // rxBytes, txBytes
	n := NewNet("eth0", 1, false, metric.UnitRaw)
	n.consume(netDevLine("eth0", high))
	cells := n.consume(netDevLine("eth0", [16]uint64{}))
	for i, c := range cells {
		if c.Raw != 0 {
			t.Errorf("reset cell %d Raw = %v, want 0", i, c.Raw)
		}
		if strings.Contains(c.Text, "-") {
			t.Errorf("reset cell %d text = %q, want no negative value", i, c.Text)
		}
	}
}

func TestNetRateDenomElapsedWindow(t *testing.T) {
	// A tick that runs long (5s between samples, interval 1) must divide the
	// delta by the real window, not the fixed interval (which would overstate
	// the rate 5x). nowFn is the injected clock seam.
	n := NewNet("eth0", 1, false, metric.UnitRaw)
	base := time.Now()
	ticks := []time.Time{base, base.Add(5 * time.Second)}
	i := 0
	n.nowFn = func() time.Time { i++; return ticks[i-1] }
	n.consume(mustRead(t, "netdev_tick1.txt"))          // baseline, last=base
	cells := n.consume(mustRead(t, "netdev_tick2.txt")) // delta 1572864 over 5s
	if cells[0].Raw != 1572864.0/5 {
		t.Errorf("recv Raw = %v, want %v (delta / 5s window)", cells[0].Raw, 1572864.0/5)
	}
}

func TestSwapCounterResetClampsToZero(t *testing.T) {
	s := NewSwap(1)
	s.consume([]byte("pswpin 50\npswpout 30\n"))
	cells := s.consume([]byte("pswpin 0\npswpout 0\n"))
	if cells[0].Text != "    0" || cells[0].Color != metric.White {
		t.Errorf("reset si = %q/%v, want \"    0\"/White", cells[0].Text, cells[0].Color)
	}
	if cells[1].Text != "    0" || cells[1].Color != metric.White {
		t.Errorf("reset so = %q/%v, want \"    0\"/White", cells[1].Text, cells[1].Color)
	}
}

func TestDiskCounterResetClampsToZero(t *testing.T) {
	// Device removed and re-added: every diskstats counter for sda resets to
	// 0; all iostat columns must clamp to 0 (busy included), never negative.
	cpu := NewCPU(2, false, false)
	d := NewDisk(cpu, []string{"sda"}, 2, false, metric.UnitRaw)
	cpu.consume(mustRead(t, "stat_tick1.txt"))
	d.consume(mustRead(t, "diskstats_tick1.txt"))
	cpu.consume(mustRead(t, "stat_tick2.txt"))
	reset := []byte("   8       0 sda 0 0 0 0 0 0 0 0 0 0 0 0\n")
	cells := d.consume(reset)
	for i, c := range cells {
		if c.Raw != 0 {
			t.Errorf("reset cell %d Raw = %v, want 0", i, c.Raw)
		}
		if strings.Contains(c.Text, "-") {
			t.Errorf("reset cell %d text = %q, want no negative value", i, c.Text)
		}
	}
}

func TestDiskDegradeZeroDeltamsThenRecover(t *testing.T) {
	// CPU diffs unavailable for a tick (e.g. /proc/stat unreadable) leaves
	// deltams == 0. The consume path must emit zeros — never Inf/NaN from
	// division by zero — AND refresh prev, so the recovery ticks compute
	// real rates instead of a since-boot spike.
	cpu := NewCPU(2, false, false)
	d := NewDisk(cpu, []string{"sda"}, 2, false, metric.UnitRaw)

	// Degrade tick: no CPU sample yet → deltams() == 0.
	cells := d.consume(mustRead(t, "diskstats_tick1.txt"))
	if len(cells) != 7 {
		t.Fatalf("degrade row = %d cells, want 7", len(cells))
	}
	for i, c := range cells {
		if c.Text != "      0" {
			t.Errorf("degrade cell %d = %q, want zero cell", i, c.Text)
		}
	}

	// Recovery: first tick re-reads the same counters (prev was refreshed by
	// the degrade) → zeros, then the real delta tick.
	cpu.consume(mustRead(t, "stat_tick1.txt"))
	d.consume(mustRead(t, "diskstats_tick1.txt"))
	cpu.consume(mustRead(t, "stat_tick2.txt"))
	cells = d.consume(mustRead(t, "diskstats_tick2.txt"))
	for i, c := range cells {
		if math.IsInf(c.Raw, 0) || math.IsNaN(c.Raw) {
			t.Fatalf("recovery cell %d Raw = %v, want finite", i, c.Raw)
		}
	}
	// rd_ios_s = 1000*(160-100)/500 = 120 — same as the normal-path test.
	if cells[0].Raw != 120 {
		t.Errorf("recovery r/s Raw = %v, want 120 (no since-boot spike)", cells[0].Raw)
	}
}

func TestMemFallbackWithoutMemAvailable(t *testing.T) {
	// Pre-3.14 kernels export no MemAvailable: availKB must fall back to
	// free+buffers+cached and usage% must derive from that same value.
	m := parseMemInfo(mustRead(t, "meminfo_noavail.txt"))
	if m.available != 0 {
		t.Fatalf("fixture must lack MemAvailable, got %d", m.available)
	}
	// total 16384000, free 2000000, buffers 500000, cached 4500000 kB.
	if got := m.availKB(); got != 7000000 {
		t.Errorf("availKB fallback = %d, want 7000000 (free+buff+cached)", got)
	}
	// usage = (16384000-7000000)/16384000*100 ≈ 57.31
	if u := m.usage(); u < 57.2 || u > 57.4 {
		t.Errorf("usage = %v, want ~57.3", u)
	}
}

func TestMemAvailClampExceedsTotal(t *testing.T) {
	// free+buffers+cached can exceed total under odd accounting (shmem etc.):
	// availKB must clamp to total so usage never goes negative.
	m := memInfo{total: 1000, free: 900, buffers: 800, cached: 800, ok: true}
	if got := m.availKB(); got != 1000 {
		t.Errorf("availKB = %d, want 1000 (clamped to total)", got)
	}
	if u := m.usage(); u != 0 {
		t.Errorf("usage = %v, want 0", u)
	}
}
