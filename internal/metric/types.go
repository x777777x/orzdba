// Package metric defines shared sample types passed between collectors and
// the renderer.
//
// Color/Cell/Group are the data contract between collectors (which produce
// values) and the renderer (which paints them). The Color enum is just an
// identifier; the render package owns the ANSI byte mapping so that metric
// stays free of rendering concerns (and free of an import on render, which
// would be cyclic since render imports metric).
package metric

// Color selects an ANSI color/attribute. Renderers map each value to a byte
// sequence; in nocolor mode every value maps to empty so zero ANSI bytes are
// emitted (plan §7.3, fixing orzdba-go P1-13 where nocolor still printed
// "\033[;22m").
type Color uint8

const (
	ColorNone Color = iota
	Red
	Green
	Yellow
	Blue
	White
	Magenta
	Bold
	Underline
	OnBlue
)

// Cell is one formatted column value with its color. Text is what the
// renderer prints; Raw carries the original numeric value (bytes/s, bytes,
// percent) for ES/trending consumption — set by collectors regardless of the
// unit mode, so downstream JSON export never needs to re-parse formatted text.
type Cell struct {
	Text  string
	Raw   float64 // raw numeric value (0 when not applicable)
	Color Color
}

// UnitMode controls how byte/percent-valued metrics are presented.
// UnitRaw is the default and is ES-friendly: raw numbers, no k/m/g suffix.
type UnitMode uint8

const (
	UnitRaw   UnitMode = iota // raw numbers (bytes/s, bytes, percent float)
	UnitHuman                 // human-readable k/m/g suffixes (Perl-compatible)
)

// Group tags a collector's output so the renderer picks the right separator
// style: sys-group segments get a bold-blue '|', mysql-group segments a green
// '|' (matching the Perl original's two separator styles).
type Group uint8

const (
	GroupSys   Group = iota // blue-bold '|' separator (time/load/cpu/swap/net/disk)
	GroupMySQL              // green '|' separator (com/hit/threads/...)
)
