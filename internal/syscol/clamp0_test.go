package syscol

import "testing"

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
