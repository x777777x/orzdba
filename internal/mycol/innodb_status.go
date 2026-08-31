package mycol

import (
	"context"
	"fmt"
	"strings"

	"orzdba/internal/metric"
	"orzdba/internal/render"
)

// InnodbStatusValues holds the fields parsed from SHOW ENGINE INNODB STATUS.
// All are instantaneous (current) values, not deltas.
type InnodbStatusValues struct {
	HistoryList         int64
	UnflushedLog        int64 // log_bytes_written - log_bytes_flushed
	UncheckpointedBytes int64 // log_bytes_written - last_checkpoint
	ReadViews           int64
	QueriesInside       int64
	QueriesQueued       int64
	OK                  bool // false if the query/parse failed
}

// InnodbStatus runs SHOW ENGINE INNODB STATUS and parses the row-operations
// and LOG sections (plan §7.8: the native protocol returns real newlines, so
// we split on "\n" — no backslash unescaping needed, unlike the Perl mysql
// client). This is the second per-tick query (≤2 budget, plan §9.1).
func (s *StatusSource) InnodbStatus() InnodbStatusValues {
	var out InnodbStatusValues
	if s.db == nil {
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	var typ, name, status string
	// SHOW ENGINE INNODB STATUS returns one row: (Type, Name, Status).
	row := s.db.QueryRowContext(ctx, "SHOW ENGINE INNODB STATUS")
	if err := row.Scan(&typ, &name, &status); err != nil {
		return out
	}
	raw := parseInnodbStatus(status)
	out.HistoryList = raw.historyList
	out.ReadViews = raw.readViews
	out.QueriesInside = raw.queriesInside
	out.QueriesQueued = raw.queriesQueued
	out.UnflushedLog = raw.logWritten - raw.logFlushed
	out.UncheckpointedBytes = raw.logWritten - raw.lastCheckpoint
	out.OK = true
	return out
}

// innodbStatusRaw is the direct parse output before computing derived fields.
type innodbStatusRaw struct {
	historyList    int64
	logWritten     int64 // "Log sequence number"
	logFlushed     int64 // "Log flushed up to"
	lastCheckpoint int64 // "Last checkpoint at"
	queriesInside  int64
	queriesQueued  int64
	readViews      int64
}

// parseInnodbStatus extracts the fields the Perl original reads from the
// ENGINE INNODB STATUS text. Substring + Fields matching mirrors Perl's
// index()/split(/\s+/). Missing fields stay 0.
func parseInnodbStatus(text string) innodbStatusRaw {
	var r innodbStatusRaw
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch {
		case strings.Contains(line, "History list length") && len(f) > 3:
			r.historyList = parseInt64(f[3])
		case strings.Contains(line, "Log sequence number") && len(f) > 3:
			r.logWritten = parseInt64(f[3])
		case strings.Contains(line, "Log flushed up to") && len(f) > 4:
			r.logFlushed = parseInt64(f[4])
		case strings.Contains(line, "Last checkpoint at") && len(f) > 3:
			r.lastCheckpoint = parseInt64(f[3])
		case strings.Contains(line, "queries inside InnoDB") && len(f) > 4:
			r.queriesInside = parseInt64(f[0])
			r.queriesQueued = parseInt64(f[4])
		case strings.Contains(line, "read views open inside InnoDB"):
			r.readViews = parseInt64(f[0])
		}
	}
	return r
}

// InnodbStatus reports history list, log unflushed/uncheckpointed bytes, read
// views, and queries inside/queued — parsed from SHOW ENGINE INNODB STATUS.
// Byte cells carry Raw bytes (absolute); --unit switches to k/m display.
type InnodbStatus struct {
	src  *StatusSource
	unit metric.UnitMode
}

func NewInnodbStatus(s *StatusSource, unit metric.UnitMode) *InnodbStatus {
	return &InnodbStatus{src: s, unit: unit}
}

func (*InnodbStatus) Name() string { return "innodb_status" }
func (*InnodbStatus) Headline() (string, string) {
	return "  his --log(byte)--  read ---query--- ", " list uflush  uckpt  view inside  que|"
}

func (c *InnodbStatus) Collect() []metric.Cell {
	// First tick: zeros, no query (Perl guards innodb_status under not_first).
	if !c.src.HasPrev() {
		return []metric.Cell{{Text: fmt.Sprintf("%5d %6d %6d %5d %5d %5d", 0, 0, 0, 0, 0, 0), Color: metric.White}}
	}
	v := c.src.InnodbStatus()
	if !v.OK {
		return []metric.Cell{{Text: fmt.Sprintf("%5d %6d %6d %5d %5d %5d", 0, 0, 0, 0, 0, 0), Color: metric.White}}
	}
	// history WHITE, unflushed/ucheckpointed YELLOW, view/inside/que WHITE.
	// Leading space on the checkpointed byte column keeps raw values separated.
	return []metric.Cell{
		{Text: fmt.Sprintf("%5d ", v.HistoryList), Raw: float64(v.HistoryList), Color: metric.White},
		{Text: render.FormatBytesValue(float64(v.UnflushedLog), c.unit, 5, 6) + " ", Raw: float64(v.UnflushedLog), Color: metric.Yellow},
		{Text: " " + render.FormatBytesValue(float64(v.UncheckpointedBytes), c.unit, 6, 7), Raw: float64(v.UncheckpointedBytes), Color: metric.Yellow},
		{Text: fmt.Sprintf("%5d %5d %5d", v.ReadViews, v.QueriesInside, v.QueriesQueued), Raw: float64(v.QueriesInside), Color: metric.White},
	}
}
