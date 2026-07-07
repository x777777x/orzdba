package mycol

import (
	"fmt"

	"orzdba/internal/metric"
	"orzdba/internal/render"
)

// InnodbData reports Innodb_data_reads/writes (per-second) and read/written
// (per-second bytes, k/m-formatted). Color: RED when the rate exceeds 9 MiB/s,
// else WHITE (Perl threshold).
type InnodbData struct{ src *StatusSource }

func NewInnodbData(s *StatusSource) *InnodbData { return &InnodbData{src: s} }
func (*InnodbData) Name() string                { return "innodb_data" }
func (*InnodbData) Headline() (string, string) {
	return "-----innodb data status---- ", " reads writes  read written|"
}
func (c *InnodbData) Collect() []metric.Cell {
	if !c.src.HasPrev() {
		return []metric.Cell{{Text: fmt.Sprintf("%6d %6d %6d %6d", 0, 0, 0, 0), Color: metric.White}}
	}
	reads := int(c.src.Rate("Innodb_data_reads"))
	writes := int(c.src.Rate("Innodb_data_writes"))
	readBytes := c.src.Rate("Innodb_data_read")
	writtenBytes := c.src.Rate("Innodb_data_written")
	// Perl: "%6d %6d " (reads/writes) WHITE; read "%5.1fm"/"%6s"; written " %5.1fm"/" %6s".
	return []metric.Cell{
		{Text: fmt.Sprintf("%6d %6d ", reads, writes), Color: metric.White},
		{Text: render.FormatBytesKM(readBytes, 5, 6), Color: innodbDataColor(readBytes)},
		{Text: " " + render.FormatBytesKM(writtenBytes, 5, 6), Color: innodbDataColor(writtenBytes)},
	}
}

// innodbDataColor: RED when >9 MiB/s, else WHITE (Perl).
func innodbDataColor(b float64) metric.Color {
	if b/1024/1024 > 9 {
		return metric.Red
	}
	return metric.White
}
