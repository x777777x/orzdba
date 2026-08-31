// Package render — header/row assembly.
//
// Renderer builds the two header lines from each collector's Headline()
// fragments and renders data Rows with per-cell color and the per-group
// separator ('|'). The Perl original reprints the header every 15 rows;
// --header-period overrides (plan §7.11).
package render

import (
	"strings"

	"orzdba/internal/metric"
)

// Collector is the contract every sampling module implements. Headline
// returns the two header fragments (top label, sub label) the module
// contributes; Collect returns the module's cells for one tick (without a
// trailing separator — the Renderer inserts the group separator).
type Collector interface {
	// Name is a short identifier for diagnostics.
	Name() string
	// Headline returns the (line1, line2) header fragments. line2 conventionally
	// ends with '|' so it lines up under line1's trailing space, matching Perl.
	Headline() (line1, line2 string)
	// Collect returns the module's cells for this tick. First-tick collectors
	// return zero-valued cells (plan §7.12).
	Collect() []metric.Cell
}

// Renderer assembles headers and rows for an ordered set of collectors,
// grouping them into the sys half (bold-blue '|' separator) and the mysql
// half (green '|' separator) — the two separator styles the Perl original
// uses.
type Renderer struct {
	ansi      *ANSI
	sys       []Collector // includes the time module, which is sys-styled
	mysql     []Collector
	period    int    // header repeat period in data rows
	headerOff bool   // -noheader: suppress the title block and periodic headers
	sepStr    string // --sep custom data-row separator; "" = not set (default "|" per group)
}

// NewRenderer returns a Renderer. period is the header repeat cadence
// (Perl default 15); color toggles ANSI escapes. period <= 0 is normalized to
// 15 here — this is the single source of truth (P0-1: runLoop must not divide
// by the raw user-supplied period).
func NewRenderer(color bool, period int) *Renderer {
	if period <= 0 {
		period = 15
	}
	return &Renderer{ansi: NewANSI(color), period: period}
}

// SetHeaderOff enables/disables header output (-noheader). When off, Header()
// returns "" and the caller skips the title block too.
func (r *Renderer) SetHeaderOff(off bool) { r.headerOff = off }

// SetSep sets the data-row column separator (--sep). A value of "\\t" is
// converted to a tab character; any other value is used verbatim. When sep is
// non-empty it replaces the group-based separators entirely (the user asked
// for one uniform separator, not the per-group "|" / green "|").
func (r *Renderer) SetSep(sep string) {
	if sep == `\t` {
		sep = "\t"
	}
	r.sepStr = sep
}

// Period returns the normalized header repeat cadence. runLoop uses this
// instead of the raw flag value so a --header-period 0 can never divide-by-zero.
func (r *Renderer) Period() int { return r.period }

// AddSys appends a sys-group collector (time/load/cpu/swap/net/disk).
func (r *Renderer) AddSys(c Collector) { r.sys = append(r.sys, c) }

// AddMySQL appends a mysql-group collector (com/hit/threads/...).
func (r *Renderer) AddMySQL(c Collector) { r.mysql = append(r.mysql, c) }

// Header renders the two header lines, applying the sys styling (BLUE+BOLD
// on line1, BLUE+UNDERLINE+BOLD on line2) and, when mysql collectors exist,
// the mysql styling (ON_BLUE+GREEN / GREEN+UNDERLINE). Returns "" when header
// output is disabled (-noheader).
func (r *Renderer) Header() string {
	if r.headerOff {
		return ""
	}
	var sys1, sys2, my1, my2 strings.Builder
	for _, c := range r.sys {
		l1, l2 := c.Headline()
		sys1.WriteString(l1)
		sys2.WriteString(l2)
	}
	for _, c := range r.mysql {
		l1, l2 := c.Headline()
		my1.WriteString(l1)
		my2.WriteString(l2)
	}
	var b strings.Builder
	b.WriteString(r.ansi.Colorize(sys1.String(), metric.Blue, metric.Bold))
	if my1.Len() > 0 {
		b.WriteString(r.ansi.Colorize(my1.String(), metric.OnBlue, metric.Green))
	}
	b.WriteString("\n")
	b.WriteString(r.ansi.Colorize(sys2.String(), metric.Blue, metric.Underline, metric.Bold))
	if my2.Len() > 0 {
		b.WriteString(r.ansi.Colorize(my2.String(), metric.Green, metric.Underline))
	}
	b.WriteString("\n")
	return b.String()
}

// BuildRow calls Collect on every collector in order (sys then mysql) and
// renders one sampling line. Collectors that return no cells (e.g. CPU when
// only -d is set) are skipped entirely — no segment, no separator — so they
// leave no trace in the line.
func (r *Renderer) BuildRow() string {
	var b strings.Builder
	emit := func(c Collector, group metric.Group) {
		cells := c.Collect()
		if len(cells) == 0 {
			return
		}
		for _, cell := range cells {
			if cell.Color == metric.ColorNone {
				b.WriteString(cell.Text)
				continue
			}
			b.WriteString(r.ansi.Escape(cell.Color))
			b.WriteString(cell.Text)
			b.WriteString(r.ansi.Reset())
		}
		b.WriteString(r.sep(group))
	}
	for _, c := range r.sys {
		emit(c, metric.GroupSys)
	}
	for _, c := range r.mysql {
		emit(c, metric.GroupMySQL)
	}
	b.WriteString("\n")
	return b.String()
}

// sep returns the column separator. With no --sep configured, it is the
// per-group colored '|' (blue for sys, green for mysql). When --sep is set,
// every column uses that single literal separator (no color) — the user
// explicitly asked for a uniform separator.
func (r *Renderer) sep(g metric.Group) string {
	if r.sepStr != "" {
		return r.sepStr
	}
	switch g {
	case metric.GroupMySQL:
		return r.ansi.Colorize("|", metric.Green)
	default: // GroupSys
		return r.ansi.Colorize("|", metric.Blue, metric.Bold)
	}
}

// ANSI exposes the underlying ANSI helper for callers (e.g. the title block)
// that need direct escapes.
func (r *Renderer) ANSI() *ANSI { return r.ansi }
