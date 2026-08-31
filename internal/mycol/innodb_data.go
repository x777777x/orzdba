package mycol

import (
	"fmt"

	"orzdba/internal/metric"
	"orzdba/internal/render"
)

// InnodbData reports Innodb_data_reads/writes (per-second) and read/written
// (per-second bytes). Color: RED when the rate exceeds 9 MiB/s, else WHITE
// (Perl threshold). Raw bytes are bytes/s (ES-friendly); --unit switches the
// display to k/m suffixes.
type InnodbData struct {
	src  *StatusSource
	unit metric.UnitMode
}

func NewInnodbData(s *StatusSource, unit metric.UnitMode) *InnodbData {
	return &InnodbData{src: s, unit: unit}
}

func (*InnodbData) Name() string { return "innodb_data" }
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
	// Leading space on byte columns keeps raw values separated.
	return []metric.Cell{
		{Text: fmt.Sprintf("%6d %6d ", reads, writes), Raw: float64(reads), Color: metric.White},
		{Text: " " + render.FormatBytesValue(readBytes, c.unit, 5, 6), Raw: readBytes, Color: innodbDataColor(readBytes)},
		{Text: " " + render.FormatBytesValue(writtenBytes, c.unit, 5, 6), Raw: writtenBytes, Color: innodbDataColor(writtenBytes)},
	}
}

// innodbDataColor: RED when >9 MiB/s, else WHITE (Perl).
func innodbDataColor(b float64) metric.Color {
	if b/1024/1024 > 9 {
		return metric.Red
	}
	return metric.White
}
