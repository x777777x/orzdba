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
type Net struct {
	name     string
	interval float64
	notFirst bool
	recv     uint64
	send     uint64
}

// NewNet returns a net collector for the named interface. The interface must
// exist in /proc/net/dev; existence is verified by the main loop at startup
// (plan §11.1).
func NewNet(name string, interval int) *Net {
	return &Net{name: name, interval: float64(interval)}
}

func (*Net) Name() string { return "net" }

func (*Net) Headline() (string, string) {
	return "----net(B)---- ", "   recv   send|"
}

// Collect reads /proc/net/dev and formats recv/send rates.
func (n *Net) Collect() []metric.Cell {
	data, _ := readFile("/proc/net/dev")
	return n.consume(data)
}

// consume processes one /proc/net/dev sample and formats recv/send rates.
// First tick emits zeros. Color is RED when the rate exceeds 1 MiB/s, else
// WHITE (Perl).
func (n *Net) consume(data []byte) []metric.Cell {
	recv, send := parseNetDev(data, n.name)
	if !n.notFirst {
		n.recv = recv
		n.send = send
		n.notFirst = true
		return []metric.Cell{
			{Text: fmt.Sprintf("%7s", "0"), Color: metric.White},
			{Text: fmt.Sprintf("%7s", "0"), Color: metric.White},
		}
	}
	dRecv := float64(recv) - float64(n.recv)
	dSend := float64(send) - float64(n.send)
	n.recv = recv
	n.send = send
	recvRate := dRecv / n.interval
	sendRate := dSend / n.interval
	return []metric.Cell{
		{Text: render.FormatBytesRate(recvRate), Color: netColor(recvRate)},
		{Text: render.FormatBytesRate(sendRate), Color: netColor(sendRate)},
	}
}

// netColor: rate>1MiB/s RED else WHITE (Perl).
func netColor(rate float64) metric.Color {
	if rate/1024/1024 > 1 {
		return metric.Red
	}
	return metric.White
}

// parseNetDev finds the device line in /proc/net/dev and returns its recv
// (field[1]) and send (field[9]) byte counters. Fields are split on
// whitespace; the colon stays attached to the device name, so the byte
// counters sit at fixed indices regardless of name length (plan §7.1).
func parseNetDev(data []byte, dev string) (recv, send uint64) {
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 10 {
			continue
		}
		// f[0] looks like "eth0:" — strip the trailing colon to compare.
		name := strings.TrimSuffix(f[0], ":")
		if name != dev {
			continue
		}
		recv = parseUint(f[1])
		send = parseUint(f[9])
		return
	}
	return
}
