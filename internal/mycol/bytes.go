package mycol

import (
	"fmt"

	"orzdba/internal/metric"
	"orzdba/internal/render"
)

// Bytes reports Bytes_received/sent per second, formatted with the same k/m
// branching as the net collector (FormatBytesRate). Both cells are WHITE (no
// red threshold, unlike net — Perl bytes has no color escalation).
type Bytes struct {
	src *StatusSource
}

func NewBytes(s *StatusSource) *Bytes { return &Bytes{src: s} }

func (*Bytes) Name() string { return "bytes" }

func (*Bytes) Headline() (string, string) {
	return "-----bytes---- ", "   recv   send|"
}

func (b *Bytes) Collect() []metric.Cell {
	if !b.src.HasPrev() {
		return []metric.Cell{
			{Text: fmt.Sprintf(" %6d", 0), Color: metric.White},
			{Text: fmt.Sprintf(" %6d", 0), Color: metric.White},
		}
	}
	recv := b.src.Rate("Bytes_received")
	send := b.src.Rate("Bytes_sent")
	return []metric.Cell{
		{Text: render.FormatBytesRate(recv), Color: metric.White},
		{Text: render.FormatBytesRate(send), Color: metric.White},
	}
}
