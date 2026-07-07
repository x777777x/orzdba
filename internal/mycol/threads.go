package mycol

import (
	"fmt"

	"orzdba/internal/metric"
)

// Threads reports Threads_running/connected (current), Threads_created (per-
// second delta), Threads_cached (current), and thread cache hit% — the orzdba-go
// extension adopted per plan §2.3/§6. hit% = (1 - Threads_created_delta /
// Connections_delta) * 100; 100.00 when there were no new connections.
//
// This is a 5-column layout, a documented deviation from the Perl original's
// 4-column -T (run/con/cre/cac).
type Threads struct{ src *StatusSource }

func NewThreads(s *StatusSource) *Threads { return &Threads{src: s} }
func (*Threads) Name() string             { return "threads" }
func (*Threads) Headline() (string, string) {
	return "------threads------------- ", " run  con  cre  cac   %hit|"
}

func (t *Threads) Collect() []metric.Cell {
	if !t.src.HasPrev() {
		return []metric.Cell{{Text: fmt.Sprintf("%4d %4d %4d %4d %6.2f", 0, 0, 0, 0, 100.0), Color: metric.White}}
	}
	run := t.src.Cur("Threads_running")
	con := t.src.Cur("Threads_connected")
	cre := int(t.src.Rate("Threads_created"))
	cac := t.src.Cur("Threads_cached")
	hit := threadCacheHit(t.src)
	return []metric.Cell{
		{Text: fmt.Sprintf("%4d %4d %4d %4d %6.2f", run, con, cre, cac, hit), Color: tchitColor(hit)},
	}
}

// threadCacheHit computes (1 - Threads_created_rate / Connections_rate) * 100.
// Returns 100 when Connections_rate is 0 (no new connections → perfect cache).
func threadCacheHit(s *StatusSource) float64 {
	conn := s.Rate("Connections")
	cre := s.Rate("Threads_created")
	if conn == 0 {
		return 100
	}
	return (1 - cre/conn) * 100
}

// tchitColor: >99 green, >90 yellow, else red (orzdba-go thresholds).
func tchitColor(hit float64) metric.Color {
	switch {
	case hit > 99:
		return metric.Green
	case hit > 90:
		return metric.Yellow
	default:
		return metric.Red
	}
}
