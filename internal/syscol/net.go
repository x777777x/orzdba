package syscol

import (
	"fmt"
	"strings"

	"orzdba/internal/metric"
	"orzdba/internal/render"
)

// Net reads /proc/net/dev for one interface and reports recv/send bytes-per-second.
//
// The Perl original parsed the device line with split(/\s+|:/), which is
// off-by-one (it reads an empty field for recv) — plan §7.1 fixes this by
// splitting on whitespace so the colon stays attached to the device name,
// making recv=field[1] and send=field[9] (the 8th transmit stat).
//
// Like swap, net has a first-tick guard: the Perl original prints zeros on
// the first tick.
//
// With full=true (--full) it emits 8 columns: rx/tx bytes, packets, errs,
// drops. unit controls byte presentation (Raw numbers vs k/m suffixes).
type Net struct {
	name     string
	interval float64
	notFirst bool
	full     bool
	unit     metric.UnitMode
	recv     uint64
	send     uint64
	// full-mode counters (deltaable): packets/errs/drop for rx and tx.
	prev [6]uint64
}

// NewNet returns a net collector for the named interface. The interface must
// exist in /proc/net/dev; existence is verified by the main loop at startup
// (plan §11.1). full enables the 8-column detail output; unit selects byte
// presentation.
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

// Collect reads /proc/net/dev and formats recv/send rates.
func (n *Net) Collect() []metric.Cell {
	data, _ := readFile("/proc/net/dev")
	return n.consume(data)
}

// netStat holds one device line's parsed counters (16 fields after the name).
type netStat struct {
	rxBytes, rxPackets, rxErrs, rxDrop uint64
	txBytes, txPackets, txErrs, txDrop uint64
	ok                                 bool
}

// parseNetDevFull finds the device line in /proc/net/dev and returns all its
// rx/tx counters. Fields are split on whitespace; the colon stays attached to
// the device name, so indices are stable (plan §7.1).
func parseNetDevFull(data []byte, dev string) netStat {
	var s netStat
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 17 {
			continue
		}
		if strings.TrimSuffix(f[0], ":") != dev {
			continue
		}
		s.rxBytes = parseUint(f[1])
		s.rxPackets = parseUint(f[2])
		s.rxErrs = parseUint(f[3])
		s.rxDrop = parseUint(f[4])
		s.txBytes = parseUint(f[9])
		s.txPackets = parseUint(f[10])
		s.txErrs = parseUint(f[11])
		s.txDrop = parseUint(f[12])
		s.ok = true
		return s
	}
	return s
}

// consume processes one /proc/net/dev sample and formats recv/send rates.
// First tick emits zeros. Color is RED when the rate exceeds 1 MiB/s, else
// WHITE (Perl).
func (n *Net) consume(data []byte) []metric.Cell {
	s := parseNetDevFull(data, n.name)
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
	// Full: rxbytes rxpkts rxerr rxdrop txbytes txpkts txerr txdrop (rates).
	// Leading space on every column keeps them separated even when a raw value
	// overflows its width (raw integers run together otherwise).
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

// parseNetDev finds the device line in /proc/net/dev and returns its recv
// (field[1]) and send (field[9]) byte counters. Fields are split on
// whitespace; the colon stays attached to the device name, so the byte
// counters sit at fixed indices regardless of name length (plan §7.1).
func parseNetDev(data []byte, dev string) (recv, send uint64) {
	s := parseNetDevFull(data, dev)
	return s.rxBytes, s.txBytes
}
