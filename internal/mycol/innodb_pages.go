package mycol

import (
	"fmt"

	"orzdba/internal/metric"
)

// InnodbPages reports buffer pool pages: data/free (current), dirty (current),
// flushed (per-second delta).
type InnodbPages struct{ src *StatusSource }

func NewInnodbPages(s *StatusSource) *InnodbPages { return &InnodbPages{src: s} }
func (*InnodbPages) Name() string                 { return "innodb_pages" }
func (*InnodbPages) Headline() (string, string) {
	return "---innodb bp pages status-- ", "   data   free  dirty flush|"
}
func (c *InnodbPages) Collect() []metric.Cell {
	if !c.src.HasPrev() {
		return []metric.Cell{{Text: fmt.Sprintf("%7d %6d %6d %5d", 0, 0, 0, 0), Color: metric.White}}
	}
	data := c.src.Cur("Innodb_buffer_pool_pages_data")
	free := c.src.Cur("Innodb_buffer_pool_pages_free")
	dirty := c.src.Cur("Innodb_buffer_pool_pages_dirty")
	flushed := int(c.src.Rate("Innodb_buffer_pool_pages_flushed"))
	// Perl: data/free WHITE, dirty/flushed YELLOW.
	return []metric.Cell{
		{Text: fmt.Sprintf("%7d %6d ", data, free), Color: metric.White},
		{Text: fmt.Sprintf("%6d %5d", dirty, flushed), Color: metric.Yellow},
	}
}
