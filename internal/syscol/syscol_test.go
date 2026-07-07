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
	c := NewCPU(2, true)
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
	n := NewNet("eth0", 1)
	n.consume(mustRead(t, "netdev_tick1.txt"))
	cells := n.consume(mustRead(t, "netdev_tick2.txt"))
	// recv delta 1572864 → 1.5MiB/s → "   1.5m" Red
	// send delta 1048576 → 1.0MiB/s → k branch → "  1024k" White
	if cells[0].Text != "   1.5m" || cells[0].Color != metric.Red {
		t.Errorf("recv = %q/%v, want \"   1.5m\"/Red", cells[0].Text, cells[0].Color)
	}
	if cells[1].Text != "  1024k" || cells[1].Color != metric.White {
		t.Errorf("send = %q/%v, want \"  1024k\"/White", cells[1].Text, cells[1].Color)
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
	cpu := NewCPU(2, false)
	d := NewDisk(cpu, "sda", 2)
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
