package mycol

import (
	"fmt"

	"orzdba/internal/metric"
)

// InnodbRows reports Innodb_rows_inserted/updated/deleted/read per second.
type InnodbRows struct{ src *StatusSource }

func NewInnodbRows(s *StatusSource) *InnodbRows { return &InnodbRows{src: s} }
func (*InnodbRows) Name() string                { return "innodb_rows" }
func (*InnodbRows) Headline() (string, string) {
	return "---innodb rows status--- ", "  ins   upd   del   read|"
}
func (c *InnodbRows) Collect() []metric.Cell {
	if !c.src.HasPrev() {
		return []metric.Cell{{Text: fmt.Sprintf("%5d %5d %5d %6d", 0, 0, 0, 0), Color: metric.White}}
	}
	ins := c.src.Rate("Innodb_rows_inserted")
	upd := c.src.Rate("Innodb_rows_updated")
	del := c.src.Rate("Innodb_rows_deleted")
	rd := c.src.Rate("Innodb_rows_read")
	return []metric.Cell{
		{Text: fmt.Sprintf("%5d %5d %5d %6d", int(ins), int(upd), int(del), int(rd)), Color: metric.White},
	}
}
