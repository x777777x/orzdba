package rtcol

import (
	"testing"

	"orzdba/internal/metric"
)

func TestParseRTLine(t *testing.T) {
	// 13 cols: timestamp count max min avg med stddev max_95 avg_95 std_95 max_99 avg_99 std_99
	line := "1234567890.123 150 50000 100 1200 1100 50 48000 1150 40 49000 1180 30"
	count, avg, avg95, avg99, ok := parseRTLine(line)
	if !ok {
		t.Fatal("parseRTLine returned !ok")
	}
	if count != 150 {
		t.Errorf("count = %d, want 150", count)
	}
	if avg != 1200 {
		t.Errorf("avg = %d, want 1200", avg)
	}
	if avg95 != 1150 {
		t.Errorf("avg95 = %d, want 1150", avg95)
	}
	if avg99 != 1180 {
		t.Errorf("avg99 = %d, want 1180", avg99)
	}
}

func TestParseRTLineTooFewFields(t *testing.T) {
	if _, _, _, _, ok := parseRTLine("1234567890 50 100"); ok {
		t.Error("parseRTLine on <12 fields returned ok, want false")
	}
	if _, _, _, _, ok := parseRTLine(""); ok {
		t.Error("parseRTLine on empty returned ok, want false")
	}
}

func TestRTColor(t *testing.T) {
	// Threshold is strictly >10000: 10000 → green, 10001 → red.
	if got := rtColor(10000); got != metric.Green {
		t.Errorf("rtColor(10000) = %v, want Green", got)
	}
	if got := rtColor(10001); got != metric.Red {
		t.Errorf("rtColor(10001) = %v, want Red", got)
	}
}
