package mycol

import (
	"fmt"

	"orzdba/internal/metric"
)

// Com reports QPS/TPS: Com_insert/update/delete/select deltas per second, and
// TPS. In the default iud mode TPS = insert+update+delete (matching Perl); in
// commit mode TPS = Com_commit+Com_rollback (P2-3: --tps-mode commit was
// documented but previously accepted-and-ignored).
type Com struct {
	src      *StatusSource
	commitTP bool // --tps-mode commit
}

// NewCom returns a Com collector sharing the given status source.
func NewCom(s *StatusSource, commitTP bool) *Com { return &Com{src: s, commitTP: commitTP} }

func (*Com) Name() string { return "com" }

func (*Com) Headline() (string, string) {
	return "                    -QPS- -TPS-", "  ins   upd   del    sel   iud|"
}

func (c *Com) Collect() []metric.Cell {
	if !c.src.HasPrev() {
		return []metric.Cell{
			{Text: fmt.Sprintf("%5d %5d %5d", 0, 0, 0), Color: metric.White},
			{Text: fmt.Sprintf(" %6d", 0), Color: metric.Yellow},
			{Text: fmt.Sprintf(" %5d", 0), Color: metric.Yellow},
		}
	}
	ins := c.src.Rate("Com_insert")
	upd := c.src.Rate("Com_update")
	del := c.src.Rate("Com_delete")
	sel := c.src.Rate("Com_select")
	tps := ins + upd + del
	if c.commitTP {
		tps = c.src.Rate("Com_commit") + c.src.Rate("Com_rollback")
	}
	return []metric.Cell{
		{Text: fmt.Sprintf("%5d %5d %5d", int(ins), int(upd), int(del)), Raw: tps, Color: metric.White},
		{Text: fmt.Sprintf(" %6d", int(sel)), Raw: sel, Color: metric.Yellow},
		{Text: fmt.Sprintf(" %5d", int(tps)), Raw: tps, Color: metric.Yellow},
	}
}
