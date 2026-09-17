package syscol

import (
	"fmt"

	"orzdba/internal/metric"
	"orzdba/internal/render"
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

// zeroRow returns a zero-valued row matching disk's current cell layout for
// every configured device. deviceCells emits 7 cells per device, the first
// packing r/s and w/s (so the --sep column count is 8); the zero row mirrors
// that shape exactly — 7 cells, first cell carrying two zero values — so a
// degraded row aligns with the header in both the default and the --sep modes.
func (d *Disk) zeroRow() []metric.Cell {
	const n = 7
	cells := make([]metric.Cell, 0, len(d.devices)*n)
	for range d.devices {
		cells = append(cells, metric.Cell{Text: fmt.Sprintf("%7s%7s", "0", "0"), Color: metric.White})
		for i := 1; i < n; i++ {
			cells = append(cells, metric.Cell{Text: fmt.Sprintf("%7s", "0"), Color: metric.White})
		}
	}
	return cells
}

// memZeros returns a zero-valued row matching mem's current cell layout (1
// cell, or 7 cells in --full). A data-source failure must not collapse the
// row to a single cell: that would misalign every later column in the default
// mode and change the --sep column count — the same shape-consistency rule
// the disk zero row follows. unit drives the byte-column presentation so the
// zero row matches a normal row's format.
func memZeros(full bool, unit metric.UnitMode) []metric.Cell {
	if !full {
		return []metric.Cell{{Text: fmt.Sprintf(" %6.1f", 0.0), Raw: 0, Color: metric.White}}
	}
	z := make([]metric.Cell, 7)
	z[0] = metric.Cell{Text: fmt.Sprintf(" %6.1f", 0.0), Raw: 0, Color: metric.White}
	for i := 1; i < 7; i++ {
		z[i] = metric.Cell{Text: " " + render.FormatBytesValue(0, unit, 8, 8), Raw: 0, Color: metric.White}
	}
	return z
}
