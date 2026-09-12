package syscol

import (
	"fmt"

	"orzdba/internal/metric"
)

// Platform-shared zero-value row builders. Like the color thresholds in
// colors.go, the darwin and Linux files each carried identical copies; they
// live here once. Callers emit these on the first tick (Perl parity) and on
// data-source failure.

// netZeros returns zero-valued cells matching net's current column layout
// (2 columns, 8 in --full).
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

// zeroLoad returns three zero-valued load cells (read failure / first tick).
func zeroLoad() []metric.Cell {
	return []metric.Cell{
		{Text: fmt.Sprintf("%5.2f", 0.0), Color: metric.White},
		{Text: fmt.Sprintf(" %5.2f", 0.0), Color: metric.White},
		{Text: fmt.Sprintf(" %5.2f", 0.0), Color: metric.White},
	}
}

// zeroRow returns a zero-valued row matching disk's current column layout for
// every configured device. A normal row carries 8 numeric values per device
// (the first cell packs r/s and w/s), so the zero row emits 8 — matching the
// header and the --sep column count on both the non-full and full layouts.
func (d *Disk) zeroRow() []metric.Cell {
	const n = 8
	cells := make([]metric.Cell, 0, len(d.devices)*n)
	for range d.devices {
		for i := 0; i < n; i++ {
			cells = append(cells, metric.Cell{Text: fmt.Sprintf("%7s", "0"), Color: metric.White})
		}
	}
	return cells
}
