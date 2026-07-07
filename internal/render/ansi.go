// Package render provides ANSI color constants and column formatting.
//
// Colors are looked up by index into a table (plan §5.5), avoiding the string
// switches orzdba-go used. In nocolor mode every escape is the empty string,
// so Colorize returns the bare text and zero ANSI bytes leak into output
// (plan §7.3, fixing orzdba-go P1-13 which still emitted "\033[;22m").
package render

import "orzdba/internal/metric"

// escapes maps each metric.Color to its ANSI escape sequence, matching the
// Term::ANSIColor constants the Perl original used.
var escapes = [...]string{
	metric.ColorNone: "",
	metric.Red:       "\x1b[31m",
	metric.Green:     "\x1b[32m",
	metric.Yellow:    "\x1b[33m",
	metric.Blue:      "\x1b[34m",
	metric.White:     "\x1b[37m",
	metric.Magenta:   "\x1b[35m",
	metric.Bold:      "\x1b[1m",
	metric.Underline: "\x1b[4m",
	metric.OnBlue:    "\x1b[44m",
}

const resetSeq = "\x1b[0m"

// ANSI renders colors when enabled; when disabled (nocolor, or logfile which
// implies nocolor per plan §6) it returns "" for every escape and reset, so
// callers need no special-casing.
type ANSI struct {
	color bool
}

// NewANSI returns an ANSI helper; color controls whether escapes are emitted.
func NewANSI(color bool) *ANSI { return &ANSI{color: color} }

// Escape returns the ANSI escape for one color, or "" when color is off.
func (a *ANSI) Escape(c metric.Color) string {
	if !a.color || c == metric.ColorNone {
		return ""
	}
	return escapes[c]
}

// Reset returns the reset escape, or "" when color is off.
func (a *ANSI) Reset() string {
	if !a.color {
		return ""
	}
	return resetSeq
}

// Colorize wraps text in the given colors and a trailing reset. With color
// off it returns text unchanged.
func (a *ANSI) Colorize(text string, colors ...metric.Color) string {
	if !a.color || len(colors) == 0 {
		return text
	}
	var b []byte
	for _, c := range colors {
		if c == metric.ColorNone {
			continue
		}
		b = append(b, escapes[c]...)
	}
	b = append(b, text...)
	b = append(b, resetSeq...)
	return string(b)
}
