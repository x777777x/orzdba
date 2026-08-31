//go:build darwin

package syscol

// #include <net/if.h>
// #include <net/if_var.h>
// #include <ifaddrs.h>
// #include <stdlib.h>
import "C"

import (
	"fmt"
	"strings"
	"unsafe"

	"orzdba/internal/metric"
	"orzdba/internal/render"
)

// Net reads per-interface byte/packet counters via getifaddrs (struct if_data)
// for one interface and reports recv/send bytes-per-second. The Linux
// implementation reads /proc/net/dev; macOS has no such file, so getifaddrs is
// the native equivalent. if_data provides all the counters the --full 8-column
// layout needs (bytes, packets, errors, drops for rx and tx).
//
// Like swap/net on Linux, net has a first-tick guard: zeros on the first tick.
type Net struct {
	name     string
	interval float64
	notFirst bool
	full     bool
	unit     metric.UnitMode
	recv     uint64
	send     uint64
	prev     [6]uint64
}

// NewNet returns a net collector for the named interface. The interface must
// exist (getifaddrs); existence is verified by the platform helper at startup.
// full enables the 8-column detail output; unit selects byte presentation.
func NewNet(name string, interval int, full bool, unit metric.UnitMode) *Net {
	return &Net{name: name, interval: float64(interval), full: full, unit: unit}
}

func (*Net) Name() string { return "net" }

func (n *Net) Headline() (string, string) {
	if n.full {
		return "----------------------------net(B)---------------------------- ", " rxbytes rxpkts rxerr rxdrop txbytes txpkts txerr txdrop|"
	}
	return "----net(B)---- ", "   recv   send|"
}

// Collect reads per-interface counters via getifaddrs and formats recv/send
// rates.
func (n *Net) Collect() []metric.Cell {
	s := n.readStats()
	if !n.notFirst {
		n.recv = s.rxBytes
		n.send = s.txBytes
		n.prev = [6]uint64{s.rxPackets, s.rxErrs, s.rxDrop, s.txPackets, s.txErrs, s.txDrop}
		n.notFirst = true
		return netZeros(n.full)
	}
	dRecv := float64(s.rxBytes) - float64(n.recv)
	dSend := float64(s.txBytes) - float64(n.send)
	n.recv = s.rxBytes
	n.send = s.txBytes
	recvRate := dRecv / n.interval
	sendRate := dSend / n.interval

	if !n.full {
		return []metric.Cell{
			{Text: " " + render.FormatBytesValue(recvRate, n.unit, 6, 7), Raw: recvRate, Color: netColor(recvRate)},
			{Text: " " + render.FormatBytesValue(sendRate, n.unit, 6, 7), Raw: sendRate, Color: netColor(sendRate)},
		}
	}
	rate := func(cur, prev uint64) float64 { return float64(cur) - float64(prev) }
	cells := []metric.Cell{
		{Text: " " + render.FormatBytesValue(recvRate, n.unit, 7, 7), Raw: recvRate, Color: netColor(recvRate)},
		{Text: fmt.Sprintf(" %7.0f", rate(s.rxPackets, n.prev[0])), Raw: rate(s.rxPackets, n.prev[0]), Color: metric.White},
		{Text: fmt.Sprintf(" %7.0f", rate(s.rxErrs, n.prev[1])), Raw: rate(s.rxErrs, n.prev[1]), Color: netErrColor(rate(s.rxErrs, n.prev[1]))},
		{Text: fmt.Sprintf(" %7.0f", rate(s.rxDrop, n.prev[2])), Raw: rate(s.rxDrop, n.prev[2]), Color: netErrColor(rate(s.rxDrop, n.prev[2]))},
		{Text: " " + render.FormatBytesValue(sendRate, n.unit, 7, 7), Raw: sendRate, Color: netColor(sendRate)},
		{Text: fmt.Sprintf(" %7.0f", rate(s.txPackets, n.prev[3])), Raw: rate(s.txPackets, n.prev[3]), Color: metric.White},
		{Text: fmt.Sprintf(" %7.0f", rate(s.txErrs, n.prev[4])), Raw: rate(s.txErrs, n.prev[4]), Color: netErrColor(rate(s.txErrs, n.prev[4]))},
		{Text: fmt.Sprintf(" %7.0f", rate(s.txDrop, n.prev[5])), Raw: rate(s.txDrop, n.prev[5]), Color: netErrColor(rate(s.txDrop, n.prev[5]))},
	}
	n.prev = [6]uint64{s.rxPackets, s.rxErrs, s.rxDrop, s.txPackets, s.txErrs, s.txDrop}
	return cells
}

// netStat holds one interface's parsed counters (same layout as the Linux
// version so the renderer sees identical columns).
type netStat struct {
	rxBytes, rxPackets, rxErrs, rxDrop uint64
	txBytes, txPackets, txErrs, txDrop uint64
	ok                                 bool
}

// readStats finds the named interface via getifaddrs and returns its if_data
// counters. Zero (ok=false) when not found or getifaddrs fails.
func (n *Net) readStats() netStat {
	var s netStat
	var ifa *C.struct_ifaddrs
	if C.getifaddrs(&ifa) != 0 {
		return s
	}
	defer C.freeifaddrs(ifa)
	for it := ifa; it != nil; it = it.ifa_next {
		if it.ifa_name == nil || it.ifa_data == nil {
			continue
		}
		if C.GoString(it.ifa_name) != n.name {
			continue
		}
		ifd := (*C.struct_if_data)(unsafe.Pointer(it.ifa_data))
		s.rxBytes = uint64(ifd.ifi_ibytes)
		s.rxPackets = uint64(ifd.ifi_ipackets)
		s.rxErrs = uint64(ifd.ifi_ierrors)
		s.rxDrop = uint64(ifd.ifi_iqdrops)
		s.txBytes = uint64(ifd.ifi_obytes)
		s.txPackets = uint64(ifd.ifi_opackets)
		s.txErrs = uint64(ifd.ifi_oerrors)
		s.txDrop = 0 // if_data has no tx-drop field; expose as 0
		s.ok = true
		return s
	}
	return s
}

// netZeros returns zero-valued cells matching the current column layout.
func netZeros(full bool) []metric.Cell {
	if full {
		z := make([]metric.Cell, 8)
		for i := range z {
			z[i] = metric.Cell{Text: fmt.Sprintf("%7s", "0"), Color: metric.White}
		}
		return z
	}
	return []metric.Cell{
		{Text: fmt.Sprintf("%7s", "0"), Color: metric.White},
		{Text: fmt.Sprintf("%7s", "0"), Color: metric.White},
	}
}

// netColor: rate>1MiB/s RED else WHITE (Perl).
func netColor(rate float64) metric.Color {
	if rate/1024/1024 > 1 {
		return metric.Red
	}
	return metric.White
}

// netErrColor: any errors/drops in the window → RED, else WHITE.
func netErrColor(d float64) metric.Color {
	if d > 0 {
		return metric.Red
	}
	return metric.White
}

// InterfaceExists reports whether the named interface is present via
// getifaddrs. Used by the platform net-device check.
func InterfaceExists(dev string) bool {
	var ifa *C.struct_ifaddrs
	if C.getifaddrs(&ifa) != 0 {
		return false
	}
	defer C.freeifaddrs(ifa)
	for it := ifa; it != nil; it = it.ifa_next {
		if it.ifa_name == nil {
			continue
		}
		if strings.TrimSpace(C.GoString(it.ifa_name)) == dev {
			return true
		}
	}
	return false
}
