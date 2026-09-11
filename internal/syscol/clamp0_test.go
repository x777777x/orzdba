package syscol

import (
	"testing"
	"time"
)

// TestClamp0 covers the counter-reset guard on both numeric types the
// collectors use (int64 diffs and float64 rates). Platform-neutral file so it
// runs on darwin too (the consumers in net/swap/disk are !darwin).
func TestClamp0(t *testing.T) {
	if got := clamp0(int64(-5)); got != 0 {
		t.Errorf("clamp0(int64(-5)) = %d, want 0", got)
	}
	if got := clamp0(int64(0)); got != 0 {
		t.Errorf("clamp0(int64(0)) = %d, want 0", got)
	}
	if got := clamp0(int64(42)); got != 42 {
		t.Errorf("clamp0(int64(42)) = %d, want 42", got)
	}
	if got := clamp0(-0.5); got != 0 {
		t.Errorf("clamp0(-0.5) = %v, want 0", got)
	}
	if got := clamp0(1.5); got != 1.5 {
		t.Errorf("clamp0(1.5) = %v, want 1.5", got)
	}
}

// TestRateDenom pins the rate-denominator rule: the real elapsed window
// floored at the configured interval (first sample has no window → interval).
func TestRateDenom(t *testing.T) {
	base := time.Now()
	// First sample (no previous) → interval.
	if d := rateDenom(time.Time{}, 1, base); d != 1 {
		t.Errorf("rateDenom(no last) = %v, want 1", d)
	}
	// Long tick (5s window, interval 1) → the real window.
	if d := rateDenom(base.Add(-5*time.Second), 1, base); d != 5 {
		t.Errorf("rateDenom(5s window) = %v, want 5", d)
	}
	// Short window (back-to-back test calls) → interval floor.
	if d := rateDenom(base.Add(-300*time.Millisecond), 1, base); d != 1 {
		t.Errorf("rateDenom(0.3s window) = %v, want 1", d)
	}
}
