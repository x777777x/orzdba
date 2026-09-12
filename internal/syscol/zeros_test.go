package syscol

import (
	"strings"
	"testing"

	"orzdba/internal/metric"
)

// TestDiskZeroRowMatchesDeviceCells: the zero row must mirror deviceCells'
// shape — 7 cells per device, the first packing r/s and w/s (8 --sep columns) —
// so a degraded row aligns with the header and with normal rows in both the
// default and the --sep output modes.
func TestDiskZeroRowMatchesDeviceCells(t *testing.T) {
	for _, full := range []bool{false, true} {
		d := NewDisk(nil, []string{"sda", "sdb"}, 1, full, metric.UnitRaw, 1)
		z := d.zeroRow()
		if got, want := len(z), 7*2; got != want {
			t.Fatalf("full=%v zeroRow cells = %d, want %d", full, got, want)
		}
		// First cell packs two zero values → 8 --sep columns per device.
		var tokens int
		for _, c := range z {
			tokens += len(strings.Fields(c.Text))
		}
		if got, want := tokens, 8*2; got != want {
			t.Errorf("full=%v zeroRow --sep tokens = %d, want %d", full, got, want)
		}
	}
}
