package mycol

import (
	"fmt"

	"orzdba/internal/metric"
)

// Hit reports InnoDB buffer pool hit rate. In the default 1-column mode it
// matches the Perl original (lor + innodb hit%). In -hit full mode it shows
// the orzdba-go 5-column extended hit: Key Buffer read/write, Index current/
// total, Qcache, lor, Innodb (plan §2.3/§6).
//
// Formulas follow orzdba-go but without its 0.0001 fudge factors (plan §2.4
// P2-19): zero denominators yield 100.00 instead.
type Hit struct {
	src  *StatusSource
	full bool
}

func NewHit(s *StatusSource, full bool) *Hit { return &Hit{src: s, full: full} }
func (*Hit) Name() string                    { return "hit" }

func (h *Hit) Headline() (string, string) {
	if h.full {
		return "----KeyBuffer------Index----Qcache---Innodb---(%) ",
			"  read  write    cur  total qcache     lor    hit|"
	}
	return "         -Hit%- ", "     lor    hit|"
}

func (h *Hit) Collect() []metric.Cell {
	if h.full {
		return h.collectFull()
	}
	return h.collectOne()
}

// collectOne is the Perl-compatible 1-column hit (lor + innodb hit%).
func (h *Hit) collectOne() []metric.Cell {
	// P2-1: first tick has no previous sample — emit zeros like the other
	// collectors instead of a misleading "0 / 100.00" green.
	if !h.src.HasPrev() {
		return []metric.Cell{
			{Text: fmt.Sprintf(" %7d", 0), Color: metric.White},
			{Text: fmt.Sprintf(" %6.2f", 0.0), Color: metric.White},
		}
	}
	rr := h.src.Rate("Innodb_buffer_pool_read_requests")
	rd := h.src.Rate("Innodb_buffer_pool_reads")
	hit := 100.0
	if rr > 0 {
		hit = (rr - rd) / rr * 100
	}
	return []metric.Cell{
		{Text: fmt.Sprintf(" %7d", int(rr)), Raw: rr, Color: metric.White},
		{Text: fmt.Sprintf(" %6.2f", hit), Raw: hit, Color: hitColor(hit)},
	}
}

// collectFull is the orzdba-go 5-column extended hit (7 fields).
func (h *Hit) collectFull() []metric.Cell {
	keyReadReq := h.src.Rate("Key_read_requests")
	keyRead := h.src.Rate("Key_reads")
	keyWriteReq := h.src.Rate("Key_write_requests")
	keyWrite := h.src.Rate("Key_writes")
	hrr := h.src.Rate("Handler_read_rnd")
	hrrn := h.src.Rate("Handler_read_rnd_next")
	hrf := h.src.Rate("Handler_read_first")
	hrk := h.src.Rate("Handler_read_key")
	hrn := h.src.Rate("Handler_read_next")
	hrp := h.src.Rate("Handler_read_prev")
	qcache := h.src.Rate("Qcache_hits")
	comSel := h.src.Rate("Com_select")
	rr := h.src.Rate("Innodb_buffer_pool_read_requests")
	rd := h.src.Rate("Innodb_buffer_pool_reads")

	keyReadHit := pct(1 - keyRead/keyReadReq)
	keyWriteHit := pct(1 - keyWrite/keyWriteReq)
	idxCur := pct(1 - (hrr+hrrn)/(hrf+hrk+hrn+hrp+hrr+hrrn))
	// index_total uses CURRENT values (not rates) per orzdba-go.
	hSum := float64(h.src.Cur("Handler_read_first") + h.src.Cur("Handler_read_key") +
		h.src.Cur("Handler_read_next") + h.src.Cur("Handler_read_prev") +
		h.src.Cur("Handler_read_rnd") + h.src.Cur("Handler_read_rnd_next"))
	hRnd := float64(h.src.Cur("Handler_read_rnd") + h.src.Cur("Handler_read_rnd_next"))
	idxTot := pct(1 - hRnd/hSum)
	qHit := pct(qcache / (qcache + comSel))
	innodbHit := pct(1 - rd/rr)

	return []metric.Cell{
		{Text: fmt.Sprintf("%6.2f", keyReadHit), Raw: keyReadHit, Color: hitColor(keyReadHit)},
		{Text: fmt.Sprintf("%7.2f", keyWriteHit), Raw: keyWriteHit, Color: hitColor(keyWriteHit)},
		{Text: fmt.Sprintf("%7.2f", idxCur), Raw: idxCur, Color: hitColor(idxCur)},
		{Text: fmt.Sprintf("%7.2f", idxTot), Raw: idxTot, Color: hitColor(idxTot)},
		{Text: fmt.Sprintf("%7.2f", qHit), Raw: qHit, Color: hitColor(qHit)},
		{Text: fmt.Sprintf("%8d", int(rr)), Raw: rr, Color: metric.White},
		{Text: fmt.Sprintf("%7.2f", innodbHit), Raw: innodbHit, Color: hitColor(innodbHit)},
	}
}

// pct clamps a ratio to a 0–100 percentage; NaN/Inf (0/0) → 100.
func pct(ratio float64) float64 {
	if ratio != ratio || ratio > 1 { // NaN or >1
		if ratio > 1 {
			return 100
		}
		return 100
	}
	if ratio < 0 {
		return 0
	}
	return ratio * 100
}

func hitColor(hit float64) metric.Color {
	if hit > 99 {
		return metric.Green
	}
	return metric.Red
}
