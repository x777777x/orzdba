package syscol

import (
	"testing"

	"orzdba/internal/metric"
)

// TestDiskZeroRowValueCount: a normal disk row carries 8 numeric values per
// device (the first cell packs r/s and w/s), so the zero row used on the first
// tick and on data-source failure must emit 8 too — otherwise a header/row
// (or a --sep column) mismatch appears exactly when the row is degraded.
func TestDiskZeroRowValueCount(t *testing.T) {
	for _, full := range []bool{false, true} {
		d := NewDisk(nil, []string{"sda", "sdb"}, 1, full, metric.UnitRaw, 1)
		if got, want := len(d.zeroRow()), 8*2; got != want {
			t.Errorf("full=%v zeroRow cells = %d, want %d", full, got, want)
		}
	}
}
