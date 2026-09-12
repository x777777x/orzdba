//go:build darwin

package syscol

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/IOKitLib.h>
#include <IOKit/storage/IOBlockStorageDriver.h>
#include <IOKit/storage/IOMedia.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

// orzdba_disk_stats enumerates whole-disk block devices via IOKit and copies
// their IO statistics into caller-owned buffers. nameStride is the byte stride
// between successive BSD names in the names buffer. Returns the count of disks
// found (whole disks with a Statistics parent), capped at max.
static int orzdba_disk_stats(char* names, int nameStride,
                             long long* rdBytes, long long* wrBytes,
                             long long* rdOps, long long* wrOps, int max) {
    int n = 0;
    io_iterator_t iter;
    io_registry_entry_t media, parent;
    kern_return_t kerr;
    CFDictionaryRef statDict;
    CFTypeRef val;
    long long v;
    char buf[128];

    kerr = IOServiceGetMatchingServices(kIOMainPortDefault,
            IOServiceMatching(kIOMediaClass), &iter);
    if (kerr != kIOReturnSuccess) return 0;
    while ((media = IOIteratorNext(iter)) != 0 && n < max) {
        // Only whole disks (disk0, disk1, ...) — skip partitions (disk0s1).
        CFTypeRef wholeRef = IORegistryEntryCreateCFProperty(media, CFSTR("Whole"), kCFAllocatorDefault, 0);
        int whole = 0;
        if (wholeRef) { CFNumberGetValue((CFNumberRef)wholeRef, kCFNumberIntType, &whole); CFRelease(wholeRef); }
        if (!whole) { IOObjectRelease(media); continue; }

        // BSD name, e.g. "disk0".
        names[n*nameStride] = 0;
        CFStringRef nameRef = (CFStringRef)IORegistryEntryCreateCFProperty(
            media, CFSTR("BSD Name"), kCFAllocatorDefault, 0);
        if (nameRef) {
            if (CFStringGetCString(nameRef, buf, sizeof(buf), kCFStringEncodingUTF8)) {
                strncpy(names + n*nameStride, buf, nameStride-1);
                names[n*nameStride + nameStride - 1] = 0;
            }
            CFRelease(nameRef);
        }

        // Walk parents to find the IOBlockStorageDriver holding the Statistics
        // dictionary. The first parent with Statistics wins.
        parent = media;
        for (int i = 0; i < 8; i++) {
            io_registry_entry_t p;
            if (IORegistryEntryGetParentEntry(parent, kIOServicePlane, &p) != kIOReturnSuccess) break;
            if (parent != media) IOObjectRelease(parent);
            parent = p;
            statDict = (CFDictionaryRef)IORegistryEntryCreateCFProperty(
                parent, CFSTR("Statistics"), kCFAllocatorDefault, 0);
            if (!statDict) continue;
            val = CFDictionaryGetValue(statDict, CFSTR("Bytes (Read)"));
            v = 0; if (val) CFNumberGetValue((CFNumberRef)val, kCFNumberSInt64Type, &v); rdBytes[n] = v;
            val = CFDictionaryGetValue(statDict, CFSTR("Bytes (Write)"));
            v = 0; if (val) CFNumberGetValue((CFNumberRef)val, kCFNumberSInt64Type, &v); wrBytes[n] = v;
            val = CFDictionaryGetValue(statDict, CFSTR("Operations (Read)"));
            v = 0; if (val) CFNumberGetValue((CFNumberRef)val, kCFNumberSInt64Type, &v); rdOps[n] = v;
            val = CFDictionaryGetValue(statDict, CFSTR("Operations (Write)"));
            v = 0; if (val) CFNumberGetValue((CFNumberRef)val, kCFNumberSInt64Type, &v); wrOps[n] = v;
            CFRelease(statDict);
            n++;
            break;
        }
        // The walk above only releases the *previous* parent each round, so
        // the final one would leak on every exit path (Statistics hit, parent
        // fetch failure, loop exhaustion) — 1 io_registry_entry_t per disk
        // per tick. Release it here; on the i==0 failure path parent == media
        // and the guard skips the release.
        if (parent != media) IOObjectRelease(parent);
        IOObjectRelease(media);
    }
    IOObjectRelease(iter);
    return n;
}
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"

	"orzdba/internal/metric"
)

// Disk reads per-disk I/O statistics via IOKit (IOBlockStorageDriver
// Statistics) and reports iostat-style fields. The Linux implementation reads
// /proc/diskstats; macOS has no per-device service-time or queue-depth
// counters, so queue/await/svctm/%util are 0 and only r/s, w/s, rkB/s, wkB/s
// carry real values (documented in README).
//
// macOS cpu_ticks are not Linux jiffies, so there is no deltams equivalent:
// rates divide each counter delta by the real elapsed wall-clock window
// (rateDenom, the same base the net collectors use). The first successful tick
// only records the baseline and prints zeros — there is no previous sample to
// rate against (matching the net/swap first-tick guard); never a since-boot
// spike.
type Disk struct {
	devices  []string
	interval float64
	full     bool
	notFirst bool
	prev     map[string]diskStat
	// last is the wall-clock of the previous successful sample. It (and prev)
	// survive a failed tick so the recovery sample rates over the true outage
	// window instead of a since-boot spike.
	last time.Time
	// nowFn and readFn are test seams (mirroring the Linux net's nowFn). A nil
	// readFn means the IOKit-backed readStats.
	nowFn  func() time.Time
	readFn func() map[string]diskStat
}

// diskStat holds the IOKit counters the formula needs (byte/op deltas).
type diskStat struct {
	rdBytes, wrBytes uint64
	rdOps, wrOps     uint64
}

// NewDisk returns a disk collector for the given device list. cpu/ncpu are
// retained in the signature for API compatibility with the Linux version but
// unused on macOS (D5: removed the dead fields — %util stays 0 here). full
// enables extended columns; unit is likewise unused. interval (seconds) is the
// rate denominator floor.
func NewDisk(_ *CPU, devices []string, _ int, full bool, _ metric.UnitMode, interval int) *Disk {
	return &Disk{devices: devices, interval: float64(interval), full: full,
		prev: make(map[string]diskStat, len(devices)), nowFn: time.Now}
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
	var l1, l2 string
	for i, dev := range d.devices {
		if i > 0 {
			l1 += "  "
			l2 += "  "
		}
		l1 += fmt.Sprintf("----%s: io-usage---- ", dev)
		if d.full {
			l2 += " r/s  w/s rkB/s wkB/s avgqu avgrq %iow %util"
		} else {
			l2 += "  r/s   w/s  rkB/s  wkB/s  queue await svctm %util"
		}
		l2 += "|"
	}
	return l1, l2
}

// Collect reads IOKit disk statistics and formats the columns for each device.
func (d *Disk) Collect() []metric.Cell {
	now := d.nowFn()
	denom := rateDenom(d.last, d.interval, now)
	stats := d.sample()
	if len(stats) == 0 {
		// IOKit temporarily unavailable: zeros for this tick, but keep the
		// baseline and the sample clock — the recovery tick then rates over
		// the real outage window (delta / true elapsed) instead of a
		// since-boot spike.
		return d.zeroRow()
	}
	d.last = now
	if !d.notFirst {
		// First successful sample: record the baseline, print zeros.
		for _, dev := range d.devices {
			d.prev[dev] = stats[dev]
		}
		d.notFirst = true
		return d.zeroRow()
	}
	cells := make([]metric.Cell, 0, len(d.devices)*7)
	for _, dev := range d.devices {
		cur := stats[dev]
		p := d.prev[dev]
		cells = append(cells, d.deviceCells(cur, p, denom)...)
		d.prev[dev] = cur
	}
	return cells
}

// sample returns the current per-device counters, preferring the test seam.
func (d *Disk) sample() map[string]diskStat {
	if d.readFn != nil {
		return d.readFn()
	}
	return d.readStats()
}

// diskSample is one whole disk's BSD name plus its IOKit counters.
type diskSample struct {
	name string
	stat diskStat
}

// diskSamples enumerates whole disks via the shared C helper: it allocates the
// transfer buffers, calls orzdba_disk_stats once, and unpacks the parallel
// arrays. Empty names (missing/unparseable "BSD Name") are skipped — the
// buffers are zero-initialized, so unfilled slots read as "".
func diskSamples() []diskSample {
	const max = 64
	const stride = 64
	names := make([]C.char, max*stride)
	rdB := make([]C.longlong, max)
	wrB := make([]C.longlong, max)
	rdO := make([]C.longlong, max)
	wrO := make([]C.longlong, max)
	n := C.orzdba_disk_stats(
		(*C.char)(unsafe.Pointer(&names[0])), C.int(stride),
		(*C.longlong)(unsafe.Pointer(&rdB[0])),
		(*C.longlong)(unsafe.Pointer(&wrB[0])),
		(*C.longlong)(unsafe.Pointer(&rdO[0])),
		(*C.longlong)(unsafe.Pointer(&wrO[0])),
		C.int(max),
	)
	out := make([]diskSample, 0, int(n))
	for i := 0; i < int(n); i++ {
		name := C.GoString((*C.char)(unsafe.Pointer(&names[i*stride])))
		if name == "" {
			continue
		}
		out = append(out, diskSample{name: name, stat: diskStat{
			rdBytes: uint64(rdB[i]),
			wrBytes: uint64(wrB[i]),
			rdOps:   uint64(rdO[i]),
			wrOps:   uint64(wrO[i]),
		}})
	}
	return out
}

// readStats enumerates whole disks and returns a map of BSD name → counters.
func (d *Disk) readStats() map[string]diskStat {
	samples := diskSamples()
	out := make(map[string]diskStat, len(samples))
	for _, s := range samples {
		out[s.name] = s.stat
	}
	return out
}

// deviceCells computes the iostat fields for one device. On macOS only
// r/s, w/s, rkB/s, wkB/s carry real values; queue/await/svctm/%iow/%util are 0.
// Counter deltas are normalized by the real elapsed window (seconds) so the
// columns are per-second regardless of the sampling interval. Byte columns
// carry bytes/s in Raw (ES-friendly); the display is KiB/s.
func (d *Disk) deviceCells(cur, prev diskStat, denom float64) []metric.Cell {
	rdIosS := float64(clamp0(int64(cur.rdOps)-int64(prev.rdOps))) / denom
	wrIosS := float64(clamp0(int64(cur.wrOps)-int64(prev.wrOps))) / denom
	rdBytesS := float64(clamp0(int64(cur.rdBytes)-int64(prev.rdBytes))) / denom
	wrBytesS := float64(clamp0(int64(cur.wrBytes)-int64(prev.wrBytes))) / denom
	rkibs := rdBytesS / 1024
	wkibs := wrBytesS / 1024

	if !d.full {
		return []metric.Cell{
			{Text: fmt.Sprintf("%7.1f%7.1f", rdIosS, wrIosS), Raw: rdIosS, Color: metric.White},
			{Text: fmt.Sprintf("%8.1f", rkibs), Raw: rdBytesS, Color: diskBytesColor(rkibs)},
			{Text: fmt.Sprintf(" %8.1f", wkibs), Raw: wrBytesS, Color: diskBytesColor(wkibs)},
			{Text: fmt.Sprintf(" %5.1f", 0.0), Raw: 0, Color: metric.White}, // queue
			{Text: fmt.Sprintf(" %6.1f", 0.0), Raw: 0, Color: metric.White}, // await
			{Text: fmt.Sprintf(" %5.1f", 0.0), Raw: 0, Color: metric.White}, // svctm
			{Text: fmt.Sprintf(" %5.1f", 0.0), Raw: 0, Color: metric.White}, // %util
		}
	}
	// Full mode: r/s w/s rkB/s wkB/s avgqu-sz avgrq-sz %iow %util.
	return []metric.Cell{
		{Text: fmt.Sprintf(" %5.1f%6.1f", rdIosS, wrIosS), Raw: rdIosS, Color: metric.White},
		{Text: fmt.Sprintf(" %6.1f", rkibs), Raw: rdBytesS, Color: diskBytesColor(rkibs)},
		{Text: fmt.Sprintf(" %6.1f", wkibs), Raw: wrBytesS, Color: diskBytesColor(wkibs)},
		{Text: fmt.Sprintf(" %6.1f", 0.0), Raw: 0, Color: metric.White}, // avgqu-sz
		{Text: fmt.Sprintf(" %6.1f", 0.0), Raw: 0, Color: metric.White}, // avgrq-sz
		{Text: fmt.Sprintf(" %5.1f", 0.0), Raw: 0, Color: metric.White}, // %iow
		{Text: fmt.Sprintf(" %5.1f", 0.0), Raw: 0, Color: metric.White}, // %util
	}
}

// DarwinDiskNames returns the BSD names of all whole disks on this macOS
// host (e.g. "disk0", "disk1"), used by the platform disk-device check.
func DarwinDiskNames() []string {
	samples := diskSamples()
	out := make([]string, 0, len(samples))
	for _, s := range samples {
		out = append(out, s.name)
	}
	return out
}
