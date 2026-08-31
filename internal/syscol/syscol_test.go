package syscol

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

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
