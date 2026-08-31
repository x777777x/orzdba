//go:build !darwin

package syscol

import (
	"fmt"
	"strings"

	"orzdba/internal/metric"
)

// HZ is the kernel tick rate, hardcoded to 100 to match the Perl original
// (plan §5.2: "HZ 硬编码 100，与 Perl 一致").
const HZ = 100

// Disk reads /proc/diskstats for one or more devices and reports iostat-style
// fields. It uses the Perl deltams formula (plan §7.7) rather than orzdba-go's
// `ticks/10` shortcut (P1-8), and consumes CPU's jiffies diffs for deltams.
//
// Like cpu, disk has no first-tick guard: prev is zero-initialized so the
// first tick yields since-boot averages (matching Perl).
//
// Devices are given as a comma-separated list (-d sda,sdb); each device gets
// its own column group. full=true (--full) emits the extended iostat fields.
type Disk struct {
	cpu     *CPU
	devices []string
	ncpu    int
	full    bool
	prev    map[string]diskStat
}

// diskStat holds the diskstats fields the formula needs.
type diskStat struct {
	rdIOS, rdSectors, rdTicks uint64
	wrIOS, wrSectors, wrTicks uint64
	totTicks, aveq            uint64
}

// NewDisk returns a disk collector for the given device list. cpu provides the
// jiffies diffs used by deltams (may be nil only if neither -c nor -d is set,
// which never happens when a Disk exists). full enables extended columns.
// unit is retained in the signature for API compatibility but unused by the
// disk renderer (rkB/s is always KiB/s — D5 removed the dead field).
func NewDisk(cpu *CPU, devices []string, ncpu int, full bool, _ metric.UnitMode) *Disk {
	return &Disk{cpu: cpu, devices: devices, ncpu: ncpu, full: full,
		prev: make(map[string]diskStat, len(devices))}
}

func (*Disk) Name() string { return "disk" }

func (d *Disk) Headline() (string, string) {
	if len(d.devices) == 1 {
		if d.full {
			return "-----------------------------io-usage----------------------------- ",
				"  r/s   w/s  rkB/s  wkB/s  avgqu  avgrq  %iow %util|"
		}
		return "-------------------------io-usage----------------------- ",
			"   r/s    w/s    rkB/s    wkB/s  queue await svctm %util|"
	}
	// Multi-device: one column group per device.
	var l1, l2 strings.Builder
	for i, dev := range d.devices {
		if i > 0 {
			l1.WriteString("  ")
			l2.WriteString("  ")
		}
		if d.full {
			fmt.Fprintf(&l1, "----%s: io-usage---- ", dev)
			l2.WriteString(" r/s  w/s rkB/s wkB/s avgqu avgrq %iow %util")
		} else {
			fmt.Fprintf(&l1, "----%s: io-usage---- ", dev)
			l2.WriteString("  r/s   w/s  rkB/s  wkB/s  queue await svctm %util")
		}
		l2.WriteString("|")
	}
	return l1.String(), l2.String()
}

// Collect reads /proc/diskstats and formats the iostat columns for each device.
func (d *Disk) Collect() []metric.Cell {
	data, err := readFile("/proc/diskstats")
	if err != nil {
		return d.consume(nil)
	}
	return d.consume(data)
}

// consume processes one /proc/diskstats sample and formats the columns for
// every configured device, in order.
func (d *Disk) consume(data []byte) []metric.Cell {
	stats := parseDiskStats(data)
	deltams := d.deltams()
	if deltams <= 0 {
		// /proc unavailable or CPU not sampled: degrade to zeros (plan §11.2).
		for _, dev := range d.devices {
			d.prev[dev] = stats[dev]
		}
		return d.zeroRow()
	}

	cells := make([]metric.Cell, 0, len(d.devices)*7)
	for _, dev := range d.devices {
		cur := stats[dev]
		p := d.prev[dev]
		cells = append(cells, d.deviceCells(dev, cur, p, deltams)...)
		d.prev[dev] = cur
	}
	return cells
}

// deviceCells computes the iostat fields for one device and returns its cells.
func (d *Disk) deviceCells(dev string, cur, prev diskStat, deltams float64) []metric.Cell {
	rdIOS := int64(cur.rdIOS) - int64(prev.rdIOS)
	wrIOS := int64(cur.wrIOS) - int64(prev.wrIOS)
	rdSectors := int64(cur.rdSectors) - int64(prev.rdSectors)
	wrSectors := int64(cur.wrSectors) - int64(prev.wrSectors)
	rdTicks := int64(cur.rdTicks) - int64(prev.rdTicks)
	wrTicks := int64(cur.wrTicks) - int64(prev.wrTicks)
	ticks := int64(cur.totTicks) - int64(prev.totTicks)
	aveq := int64(cur.aveq) - int64(prev.aveq)

	nIOS := rdIOS + wrIOS
	nTicks := rdTicks + wrTicks
	queue := float64(aveq) / deltams
	var wait, svcT float64
	if nIOS != 0 {
		wait = float64(nTicks) / float64(nIOS)
		svcT = float64(ticks) / float64(nIOS)
	}
	busy := 100.0 * float64(ticks) / deltams
	if busy > 100.0 {
		busy = 100.0
	}
	rdIosS := 1000.0 * float64(rdIOS) / deltams
	wrIosS := 1000.0 * float64(wrIOS) / deltams
	// KiB/s (Perl-compatible display) and bytes/s (ES-friendly raw).
	rkibs := 1000.0 * float64(rdSectors) / deltams / 2
	wkibs := 1000.0 * float64(wrSectors) / deltams / 2
	rdBytesS := 1000.0 * float64(rdSectors) * 512 / deltams
	wrBytesS := 1000.0 * float64(wrSectors) * 512 / deltams
	// avg request size in sectors (avgrq-sz) and %iowait.
	var avgRq float64
	if nIOS != 0 {
		avgRq = float64(rdSectors+wrSectors) / float64(nIOS)
	}
	percentIow := 0.0
	if d.cpu != nil {
		percentIow = d.cpu.LastIowDiff / (d.cpu.LastUserDiff + d.cpu.LastSysDiff + d.cpu.LastIdleDiff + d.cpu.LastIowDiff) * 100
	}
	_ = dev

	if !d.full {
		return []metric.Cell{
			{Text: fmt.Sprintf("%7.1f%7.1f", rdIosS, wrIosS), Raw: rdIosS, Color: metric.White},
			{Text: fmt.Sprintf("%8.1f", rkibs), Raw: rdBytesS, Color: diskBytesColor(rkibs)},
			{Text: fmt.Sprintf(" %8.1f", wkibs), Raw: wrBytesS, Color: diskBytesColor(wkibs)},
			{Text: fmt.Sprintf(" %5.1f", queue), Raw: queue, Color: metric.White},
			{Text: fmt.Sprintf(" %6.1f", wait), Raw: wait, Color: diskWaitColor(wait)},
			{Text: fmt.Sprintf(" %5.1f", svcT), Raw: svcT, Color: diskSvcColor(svcT)},
			{Text: fmt.Sprintf(" %5.1f", busy), Raw: busy, Color: diskBusyColor(busy)},
		}
	}
	// Full mode: r/s w/s rkB/s wkB/s avgqu-sz avgrq-sz %iow %util
	return []metric.Cell{
		{Text: fmt.Sprintf(" %5.1f%6.1f", rdIosS, wrIosS), Raw: rdIosS, Color: metric.White},
		{Text: fmt.Sprintf(" %6.1f", rkibs), Raw: rdBytesS, Color: diskBytesColor(rkibs)},
		{Text: fmt.Sprintf(" %6.1f", wkibs), Raw: wrBytesS, Color: diskBytesColor(wkibs)},
		{Text: fmt.Sprintf(" %6.1f", queue), Raw: queue, Color: metric.White},
		{Text: fmt.Sprintf(" %6.1f", avgRq), Raw: avgRq, Color: metric.White},
		{Text: fmt.Sprintf(" %5.1f", percentIow), Raw: percentIow, Color: metric.White},
		{Text: fmt.Sprintf(" %5.1f", busy), Raw: busy, Color: diskBusyColor(busy)},
	}
}

// deltams replicates the Perl formula:
//
//	deltams = 1000 * (user_diff + system_diff + idle_diff + iowait_diff)
//	         / ncpu / HZ
//
// using the CPU collector's most-recent jiffies diffs.
func (d *Disk) deltams() float64 {
	if d.cpu == nil {
		return 0
	}
	return 1000.0 * (d.cpu.LastUserDiff + d.cpu.LastSysDiff + d.cpu.LastIdleDiff + d.cpu.LastIowDiff) / float64(d.ncpu) / HZ
}

func diskBytesColor(v float64) metric.Color {
	if v > 1024 {
		return metric.Red
	}
	return metric.White
}
func diskWaitColor(v float64) metric.Color {
	if v > 5 {
		return metric.Red
	}
	return metric.Green
}
func diskSvcColor(v float64) metric.Color {
	if v > 5 {
		return metric.Red
	}
	return metric.White
}
func diskBusyColor(v float64) metric.Color {
	if v > 80 {
		return metric.Red
	}
	return metric.Green
}

// zeroRow returns a zero-valued row matching the current column layout.
func (d *Disk) zeroRow() []metric.Cell {
	n := 7
	if d.full {
		n = 8
	}
	cells := make([]metric.Cell, 0, len(d.devices)*n)
	for range d.devices {
		for i := 0; i < n; i++ {
			cells = append(cells, metric.Cell{Text: fmt.Sprintf("%7s", "0"), Color: metric.White})
		}
	}
	return cells
}

// parseDiskStats parses every device line in /proc/diskstats into a map keyed
// by device name. Go's strings.Fields keeps the colon-free numeric layout: the
// device name is field[2], and the stats Perl indexes as [4],[5],[6],[7],[8],
// [9],[10],[11],[13],[14] become [3],[4],[5],[6],[7],[8],[9],[10],[12],[13].
func parseDiskStats(data []byte) map[string]diskStat {
	out := make(map[string]diskStat)
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 14 {
			continue
		}
		var s diskStat
		s.rdIOS = parseUint(f[3])
		s.rdSectors = parseUint(f[5])
		s.rdTicks = parseUint(f[6])
		s.wrIOS = parseUint(f[7])
		s.wrSectors = parseUint(f[9])
		s.wrTicks = parseUint(f[10])
		s.totTicks = parseUint(f[12])
		s.aveq = parseUint(f[13])
		out[f[2]] = s
	}
	return out
}

// parseDiskStat returns the named device's stat (zero if absent). Retained for
// single-device callers and tests.
func parseDiskStat(data []byte, dev string) diskStat {
	return parseDiskStats(data)[dev]
}
