package mycol

import (
	"fmt"

	"orzdba/internal/metric"
)

// Com reports QPS/TPS: Com_insert/update/delete/select deltas per second, and
// TPS = insert+update+delete (plan §2.5: TPS mode iud is the default, matching
// Perl; commit mode is a future option).
type Com struct {
	src *StatusSource
}

// NewCom returns a Com collector sharing the given status source.
func NewCom(s *StatusSource) *Com { return &Com{src: s} }

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
	return []metric.Cell{
		{Text: fmt.Sprintf("%5d %5d %5d", int(ins), int(upd), int(del)), Color: metric.White},
		{Text: fmt.Sprintf(" %6d", int(sel)), Color: metric.Yellow},
		{Text: fmt.Sprintf(" %5d", int(tps)), Color: metric.Yellow},
	}
}
