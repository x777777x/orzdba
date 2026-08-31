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
        IOObjectRelease(media);
    }
    IOObjectRelease(iter);
    return n;
}
*/
import "C"

import (
	"fmt"
	"unsafe"

	"orzdba/internal/metric"
)

// Disk reads per-disk I/O statistics via IOKit (IOBlockStorageDriver
// Statistics) and reports iostat-style fields. The Linux implementation reads
// /proc/diskstats; macOS has no per-device service-time or queue-depth
// counters, so queue/await/svctm/%util are 0 and only r/s, w/s, rkB/s, wkB/s
// carry real values (documented in README).
//
// Like the Linux disk, prev is zero-initialized so the first tick yields
// since-boot averages.
type Disk struct {
	devices []string
	full    bool
	prev    map[string]diskStat
}

// diskStat holds the IOKit counters the formula needs (byte/op deltas).
type diskStat struct {
	rdBytes, wrBytes uint64
	rdOps, wrOps     uint64
}

// NewDisk returns a disk collector for the given device list. cpu/ncpu are
// retained in the signature for API compatibility with the Linux version but
// unused on macOS (D5: removed the dead fields — %util stays 0 here). full
// enables extended columns; unit is likewise unused.
func NewDisk(_ *CPU, devices []string, _ int, full bool, _ metric.UnitMode) *Disk {
	return &Disk{devices: devices, full: full,
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
	stats := d.readStats()
	// If the platform data source is unavailable, degrade to zeros.
	if len(stats) == 0 {
		for _, dev := range d.devices {
			d.prev[dev] = diskStat{}
		}
		return d.zeroRow()
	}
	cells := make([]metric.Cell, 0, len(d.devices)*7)
	for _, dev := range d.devices {
		cur := stats[dev]
		p := d.prev[dev]
		cells = append(cells, d.deviceCells(dev, cur, p)...)
		d.prev[dev] = cur
	}
	return cells
}

// readStats enumerates whole disks and returns a map of BSD name → counters.
func (d *Disk) readStats() map[string]diskStat {
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
	out := make(map[string]diskStat, int(n))
	for i := 0; i < int(n); i++ {
		name := C.GoString((*C.char)(unsafe.Pointer(&names[i*stride])))
		if name == "" {
			continue
		}
		out[name] = diskStat{
			rdBytes: uint64(rdB[i]),
			wrBytes: uint64(wrB[i]),
			rdOps:   uint64(rdO[i]),
			wrOps:   uint64(wrO[i]),
		}
	}
	return out
}

// deviceCells computes the iostat fields for one device. On macOS only
// r/s, w/s, rkB/s, wkB/s carry real values; queue/await/svctm/%iow/%util are 0.
func (d *Disk) deviceCells(_ string, cur, prev diskStat) []metric.Cell {
	rdBytes := int64(cur.rdBytes) - int64(prev.rdBytes)
	wrBytes := int64(cur.wrBytes) - int64(prev.wrBytes)
	rdOps := int64(cur.rdOps) - int64(prev.rdOps)
	wrOps := int64(cur.wrOps) - int64(prev.wrOps)

	if !d.full {
		return []metric.Cell{
			{Text: fmt.Sprintf("%7.1f%7.1f", float64(rdOps), float64(wrOps)), Raw: float64(rdOps), Color: metric.White},
			{Text: fmt.Sprintf("%8.1f", float64(rdBytes)/1024), Raw: float64(rdBytes), Color: diskBytesColor(float64(rdBytes) / 1024)},
			{Text: fmt.Sprintf(" %8.1f", float64(wrBytes)/1024), Raw: float64(wrBytes), Color: diskBytesColor(float64(wrBytes) / 1024)},
			{Text: fmt.Sprintf(" %5.1f", 0.0), Raw: 0, Color: metric.White},  // queue
			{Text: fmt.Sprintf(" %6.1f", 0.0), Raw: 0, Color: metric.White},  // await
			{Text: fmt.Sprintf(" %5.1f", 0.0), Raw: 0, Color: metric.White},  // svctm
			{Text: fmt.Sprintf(" %5.1f", 0.0), Raw: 0, Color: metric.White},  // %util
		}
	}
	// Full mode: r/s w/s rkB/s wkB/s avgqu-sz avgrq-sz %iow %util.
	return []metric.Cell{
		{Text: fmt.Sprintf(" %5.1f%6.1f", float64(rdOps), float64(wrOps)), Raw: float64(rdOps), Color: metric.White},
		{Text: fmt.Sprintf(" %6.1f", float64(rdBytes)/1024), Raw: float64(rdBytes), Color: diskBytesColor(float64(rdBytes) / 1024)},
		{Text: fmt.Sprintf(" %6.1f", float64(wrBytes)/1024), Raw: float64(wrBytes), Color: diskBytesColor(float64(wrBytes) / 1024)},
		{Text: fmt.Sprintf(" %6.1f", 0.0), Raw: 0, Color: metric.White},  // avgqu-sz
		{Text: fmt.Sprintf(" %6.1f", 0.0), Raw: 0, Color: metric.White},  // avgrq-sz
		{Text: fmt.Sprintf(" %5.1f", 0.0), Raw: 0, Color: metric.White},  // %iow
		{Text: fmt.Sprintf(" %5.1f", 0.0), Raw: 0, Color: metric.White},  // %util
	}
}

func diskBytesColor(v float64) metric.Color {
	if v > 1024 {
		return metric.Red
	}
	return metric.White
}

// DarwinDiskNames returns the BSD names of all whole disks on this macOS
// host (e.g. "disk0", "disk1"), used by the platform disk-device check.
func DarwinDiskNames() []string {
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
	out := make([]string, 0, int(n))
	for i := 0; i < int(n); i++ {
		name := C.GoString((*C.char)(unsafe.Pointer(&names[i*stride])))
		if name != "" {
			out = append(out, name)
		}
	}
	return out
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
