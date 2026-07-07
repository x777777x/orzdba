package syscol

import (
	"fmt"
	"strings"

	"orzdba/internal/metric"
)

// HZ is the kernel tick rate, hardcoded to 100 to match the Perl original
// (plan §5.2: "HZ 硬编码 100，与 Perl 一致").
const HZ = 100

// Disk reads /proc/diskstats for one device and reports iostat-style fields.
// It uses the Perl deltams formula (plan §7.7) rather than orzdba-go's
// `ticks/10` shortcut (P1-8), and consumes CPU's jiffies diffs for deltams.
//
// Like cpu, disk has no first-tick guard: prev is zero-initialized so the
// first tick yields since-boot averages (matching Perl).
type Disk struct {
	cpu  *CPU
	name string
	ncpu int
	prev diskStat
}

// diskStat holds the diskstats fields the formula needs.
type diskStat struct {
	rdIOS, rdSectors, rdTicks uint64
	wrIOS, wrSectors, wrTicks uint64
	totTicks, aveq            uint64
}

// NewDisk returns a disk collector for device name. cpu provides the jiffies
// diffs used by deltams (may be nil only if neither -c nor -d is set, which
// never happens when a Disk exists).
func NewDisk(cpu *CPU, name string, ncpu int) *Disk {
	return &Disk{cpu: cpu, name: name, ncpu: ncpu}
}

func (*Disk) Name() string { return "disk" }

func (*Disk) Headline() (string, string) {
	return "-------------------------io-usage----------------------- ",
		"   r/s    w/s    rkB/s    wkB/s  queue await svctm %util|"
}

// Collect reads /proc/diskstats and formats the seven iostat columns.
func (d *Disk) Collect() []metric.Cell {
	data, err := readFile("/proc/diskstats")
	if err != nil {
		return d.consume(nil)
	}
	return d.consume(data)
}

// consume processes one /proc/diskstats sample and formats the columns.
func (d *Disk) consume(data []byte) []metric.Cell {
	cur := parseDiskStat(data, d.name)
	deltams := d.deltams()
	if deltams <= 0 {
		// /proc unavailable or CPU not sampled: degrade to zeros (plan §11.2).
		d.prev = cur
		return zeroDisk()
	}

	rdIOS := int64(cur.rdIOS) - int64(d.prev.rdIOS)
	wrIOS := int64(cur.wrIOS) - int64(d.prev.wrIOS)
	rdSectors := int64(cur.rdSectors) - int64(d.prev.rdSectors)
	wrSectors := int64(cur.wrSectors) - int64(d.prev.wrSectors)
	rdTicks := int64(cur.rdTicks) - int64(d.prev.rdTicks)
	wrTicks := int64(cur.wrTicks) - int64(d.prev.wrTicks)
	ticks := int64(cur.totTicks) - int64(d.prev.totTicks)
	aveq := int64(cur.aveq) - int64(d.prev.aveq)
	d.prev = cur

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
	rkbs := 1000.0 * float64(rdSectors) / deltams / 2
	wkbs := 1000.0 * float64(wrSectors) / deltams / 2

	return []metric.Cell{
		{Text: fmt.Sprintf("%7.1f%7.1f", rdIosS, wrIosS), Color: metric.White},
		{Text: fmt.Sprintf("%8.1f", rkbs), Color: diskBytesColor(rkbs)},
		{Text: fmt.Sprintf(" %8.1f", wkbs), Color: diskBytesColor(wkbs)},
		{Text: fmt.Sprintf(" %5.1f", queue), Color: metric.White},
		{Text: fmt.Sprintf(" %6.1f", wait), Color: diskWaitColor(wait)},
		{Text: fmt.Sprintf(" %5.1f", svcT), Color: diskSvcColor(svcT)},
		{Text: fmt.Sprintf(" %5.1f", busy), Color: diskBusyColor(busy)},
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

func zeroDisk() []metric.Cell {
	return []metric.Cell{
		{Text: fmt.Sprintf("%7.1f%7.1f", 0.0, 0.0), Color: metric.White},
		{Text: fmt.Sprintf("%8.1f", 0.0), Color: metric.White},
		{Text: fmt.Sprintf(" %8.1f", 0.0), Color: metric.White},
		{Text: fmt.Sprintf(" %5.1f", 0.0), Color: metric.White},
		{Text: fmt.Sprintf(" %6.1f", 0.0), Color: metric.Green},
		{Text: fmt.Sprintf(" %5.1f", 0.0), Color: metric.White},
		{Text: fmt.Sprintf(" %5.1f", 0.0), Color: metric.Green},
	}
}

// parseDiskStat finds the named device in /proc/diskstats and returns its
// fields. Go's strings.Fields keeps the colon-free numeric layout: the device
// name is field[2], and the stats Perl indexes as [4],[5],[6],[7],[8],[9],
// [10],[11],[13],[14] become [3],[4],[5],[6],[7],[8],[9],[10],[12],[13].
func parseDiskStat(data []byte, dev string) diskStat {
	var s diskStat
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 14 || f[2] != dev {
			continue
		}
		s.rdIOS = parseUint(f[3])
		s.rdSectors = parseUint(f[5])
		s.rdTicks = parseUint(f[6])
		s.wrIOS = parseUint(f[7])
		s.wrSectors = parseUint(f[9])
		s.wrTicks = parseUint(f[10])
		s.totTicks = parseUint(f[12])
		s.aveq = parseUint(f[13])
		return s
	}
	return s
}
