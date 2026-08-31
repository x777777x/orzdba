package mycol

import (
	"fmt"

	"orzdba/internal/metric"
	"orzdba/internal/render"
)

// InnodbLog reports Innodb_os_log_fsyncs (per-second) and os_log_written
// (per-second bytes). Color: RED when written >1 MiB/s, else YELLOW (Perl).
// Raw bytes are bytes/s; --unit switches the display to k/m suffixes.
type InnodbLog struct {
	src  *StatusSource
	unit metric.UnitMode
}

func NewInnodbLog(s *StatusSource, unit metric.UnitMode) *InnodbLog {
	return &InnodbLog{src: s, unit: unit}
}

func (*InnodbLog) Name() string { return "innodb_log" }
func (*InnodbLog) Headline() (string, string) {
	return "--innodb log-- ", "fsyncs written|"
}
func (c *InnodbLog) Collect() []metric.Cell {
	if !c.src.HasPrev() {
		return []metric.Cell{{Text: fmt.Sprintf("%6d %7d", 0, 0), Color: metric.White}}
	}
	fsyncs := int(c.src.Rate("Innodb_os_log_fsyncs"))
	written := c.src.Rate("Innodb_os_log_written")
	// Perl: "%6d " fsyncs WHITE; written "%6.1fm"/"%7s", RED if >1MiB else YELLOW.
	return []metric.Cell{
		{Text: fmt.Sprintf("%6d ", fsyncs), Raw: float64(fsyncs), Color: metric.White},
		{Text: " " + render.FormatBytesValue(written, c.unit, 6, 7), Raw: written, Color: innodbLogColor(written)},
	}
}

// innodbLogColor: RED when >1 MiB/s, else YELLOW (Perl).
func innodbLogColor(b float64) metric.Color {
	if b/1024/1024 > 1 {
		return metric.Red
	}
	return metric.Yellow
}
