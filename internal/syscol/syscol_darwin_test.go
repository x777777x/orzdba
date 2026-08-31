//go:build darwin

package syscol

import "testing"

// These tests exercise the macOS-native collectors against the real host. They
// assert that the raw data sources are readable and produce plausible values,
// providing both unit coverage of the parsing and end-to-end verification of
// the sysctl/host_statistics/getifaddrs/IOKit paths on the build machine.

func TestDarwinReadLoadavg(t *testing.T) {
	la, ok := readLoadavg()
	if !ok {
		t.Fatal("readLoadavg failed on darwin")
	}
	for i, v := range la {
		if v < 0 {
			t.Fatalf("load[%d] = %v, want >= 0", i, v)
		}
	}
}

func TestDarwinReadCPULoadInfo(t *testing.T) {
	ticks := readCPULoadInfo()
	total := ticks[0] + ticks[1] + ticks[2] + ticks[3]
	if total == 0 {
		t.Fatalf("cpu ticks all zero: %v", ticks)
	}
	// idle is normally the dominant counter; just assert non-zero totals.
	if ticks[3] == 0 {
		t.Errorf("idle ticks = 0, suspicious")
	}
}

func TestDarwinReadSwapUsage(t *testing.T) {
	total, used, avail, ok := readSwapUsage()
	if !ok {
		t.Fatal("readSwapUsage failed on darwin")
	}
	if total == 0 {
		t.Fatal("swap total = 0")
	}
	if used+avail > total {
		t.Errorf("used(%d)+avail(%d) > total(%d)", used, avail, total)
	}
}

func TestDarwinCollectMemInfo(t *testing.T) {
	m := collectMemInfo()
	if !m.ok || m.total == 0 {
		t.Fatal("collectMemInfo failed on darwin")
	}
	if m.used > m.total {
		t.Errorf("used(%d) > total(%d)", m.used, m.total)
	}
	if m.available > m.total {
		t.Errorf("available(%d) > total(%d)", m.available, m.total)
	}
}

func TestDarwinNCPU(t *testing.T) {
	n := DarwinNCPU()
	if n < 1 {
		t.Fatalf("DarwinNCPU = %d, want >= 1", n)
	}
}

func TestDarwinDiskNames(t *testing.T) {
	names := DarwinDiskNames()
	if len(names) == 0 {
		t.Fatal("no disks found via IOKit")
	}
	t.Logf("disks: %v", names)
}

func TestDarwinInterfaceExists(t *testing.T) {
	if !InterfaceExists("lo0") {
		t.Error("lo0 should always exist on darwin")
	}
	if InterfaceExists("definitely-not-a-real-iface") {
		t.Error("bogus interface should not exist")
	}
}
