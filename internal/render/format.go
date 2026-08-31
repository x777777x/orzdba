package render

import (
	"fmt"
	"math"

	"orzdba/internal/metric"
)

// FormatBytesRate formats a bytes-per-second rate as k/m/g, mirroring the
// Perl original's per-field branching (plan §7.4). It is used by the net and
// bytes collectors. Column widths are chosen to match Perl's printf widths so
// de-colored output aligns column-for-column with the original.
//
// Perl net/bytes branching (strict > thresholds):
//
//	>1MiB/s -> "%6.1fm"   (e.g. "  12.3m")
//	>1KiB/s -> "%7s"      with int(x/1024+0.5)+"k"  (e.g. "    45k")
//	else     -> "%7s"     raw int  (e.g. "      7")
//
// Perl uses strict > (not >=), so exactly 1.0 MiB/s falls into the k branch.
func FormatBytesRate(bps float64) string { return FormatBytesKM(bps, 6, 7) }

// FormatBytesKM formats a byte count as k/m with configurable printf widths,
// matching Perl's per-field variants: innodb_data uses 5.1fm/6s, innodb_log
// uses 6.1fm/7s, innodb_status unflushed uses 5.1fm/6s. Strict > thresholds.
func FormatBytesKM(b float64, mWidth, sWidth int) string {
	if b/1024/1024 > 1 {
		return fmt.Sprintf("%*.*fm", mWidth, 1, b/1024/1024)
	}
	if b/1024 > 1 {
		return fmt.Sprintf("%*s", sWidth, fmt.Sprintf("%dk", int(b/1024+0.5)))
	}
	return fmt.Sprintf("%*s", sWidth, fmt.Sprintf("%d", int(b)))
}

// FormatBytesAutoG formats an absolute byte count (not a rate) with k/m/g
// suffixes. Used for SHOW VARIABLES values like innodb_buffer_pool_size and
// for innodb status byte counters, matching Perl's print_vars/innodb_status
// branching: G when /1024/1024/1024 >= 1, M when /1024/1024 > 1 (strict), else
// the raw integer. The raw branch prints a decimal integer (not %g, which
// would render large values like 1048576 as "1.048576e+06" — Perl prints the
// integer directly).
func FormatBytesAutoG(b float64) string {
	if b/1024/1024/1024 >= 1 {
		return fmt.Sprintf("%gG", b/1024/1024/1024)
	}
	if b/1024/1024 > 1 {
		return fmt.Sprintf("%gM", b/1024/1024)
	}
	return fmt.Sprintf("%d", int64(b))
}

// FormatBytesValue formats a byte rate/absolute value according to mode.
// UnitRaw prints the raw integer (no suffix) — ES-friendly; UnitHuman delegates
// to FormatBytesKM (k/m suffixes, Perl-compatible). mWidth/sWidth are the
// FormatBytesKM widths. In UnitRaw mode the value is right-aligned to the
// wider of the two widths with at least one leading space, so adjacent raw
// columns stay visually separated even when a value overflows the width
// (without the leading space, raw columns concatenate into a run-on line).
func FormatBytesValue(b float64, mode metric.UnitMode, mWidth, sWidth int) string {
	if mode == metric.UnitRaw {
		w := mWidth
		if sWidth > w {
			w = sWidth
		}
		return fmt.Sprintf("%*d", w, int64(b))
	}
	return FormatBytesKM(b, mWidth, sWidth)
}

// FormatPercentValue formats a percentage according to mode. UnitRaw keeps the
// full float (e.g. "99.60") for ES; UnitHuman prints the integer (Perl-style).
func FormatPercentValue(p float64, mode metric.UnitMode) string {
	if mode == metric.UnitRaw {
		return fmt.Sprintf("%.2f", p)
	}
	return fmt.Sprintf("%d", int(math.Round(p)))
}

// RoundInt mirrors Perl's int(x+0.5) half-up rounding (plan §7.2, fixing
// orzdba-go P1-9 which used truncating integer division).
func RoundInt(x float64) int { return int(math.Round(x)) }
