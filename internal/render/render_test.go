package render

import (
	"strings"
	"testing"

	"orzdba/internal/metric"
)

func TestFormatBytesRate(t *testing.T) {
	cases := []struct {
		rate float64
		want string
	}{
		{0, "      0"},       // raw int
		{512, "    512"},     // <1KiB raw
		{2048, "     2k"},    // >1KiB → 2k (int(2+0.5))
		{1048576, "  1024k"}, // exactly 1.0MiB → not >1 → k branch
		{1572864, "   1.5m"}, // 1.5MiB → m
	}
	for _, c := range cases {
		if got := FormatBytesRate(c.rate); got != c.want {
			t.Errorf("FormatBytesRate(%v) = %q, want %q", c.rate, got, c.want)
		}
	}
}

// FormatBytesKM is the configurable-width variant used by innodb_data (5,6),
// innodb_log (6,7), and innodb_status (5,6 / 6,7).
func TestFormatBytesKM(t *testing.T) {
	// innodb_data read uses (mWidth=5, sWidth=6): "%5.1fm" / "%6s"
	if got := FormatBytesKM(5<<20, 5, 6); got != "  5.0m" {
		t.Errorf("innodb_data 5MiB = %q, want \"  5.0m\"", got)
	}
	if got := FormatBytesKM(2048, 5, 6); got != "    2k" {
		t.Errorf("innodb_data 2KiB = %q, want \"    2k\"", got)
	}
	// innodb_log uses (6,7): "%6.1fm" / "%7s"
	if got := FormatBytesKM(2<<20, 6, 7); got != "   2.0m" {
		t.Errorf("innodb_log 2MiB = %q, want \"   2.0m\"", got)
	}
	// raw int path
	if got := FormatBytesKM(500, 5, 6); got != "   500" {
		t.Errorf("raw 500 = %q, want \"   500\"", got)
	}
}

// FormatBytesAutoG is used by the title SHOW VARIABLES section (G/M/raw).
func TestFormatBytesAutoG(t *testing.T) {
	cases := []struct {
		bytes float64
		want  string
	}{
		{1 << 30, "1G"},      // exactly 1 GiB → G
		{1 << 20, "1048576"}, // exactly 1 MiB: /1024/1024=1 not >1 (strict M) → raw integer
		{128 << 20, "128M"},  // 128 MiB → M
		{2 << 30, "2G"},      // 2 GiB → G
		{500, "500"},         // <1MiB raw
	}
	for _, c := range cases {
		if got := FormatBytesAutoG(c.bytes); got != c.want {
			t.Errorf("FormatBytesAutoG(%v) = %q, want %q", c.bytes, got, c.want)
		}
	}
}

func TestANSINocolorZeroBytes(t *testing.T) {
	a := NewANSI(false)
	// nocolor must emit ZERO ANSI bytes for every path (plan §7.3, P1-13 fix).
	if got := a.Colorize("x", metric.Red, metric.Bold); got != "x" {
		t.Errorf("nocolor Colorize = %q, want %q", got, "x")
	}
	if a.Escape(metric.Red) != "" {
		t.Errorf("nocolor Escape(Red) = %q, want empty", a.Escape(metric.Red))
	}
	if a.Reset() != "" {
		t.Errorf("nocolor Reset = %q, want empty", a.Reset())
	}
}

func TestANSIColorEmits(t *testing.T) {
	a := NewANSI(true)
	got := a.Colorize("x", metric.Red)
	if !strings.Contains(got, "\x1b[31m") || !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("color Colorize = %q, want red+reset wrapping", got)
	}
}

// stubCol is a minimal Collector for renderer tests.
type stubCol struct {
	h1, h2 string
	cells  []metric.Cell
}

func (s *stubCol) Name() string               { return "stub" }
func (s *stubCol) Headline() (string, string) { return s.h1, s.h2 }
func (s *stubCol) Collect() []metric.Cell     { return s.cells }

func TestRendererHeaderNocolor(t *testing.T) {
	r := NewRenderer(false, 15)
	r.AddSys(&stubCol{h1: "AAA ", h2: "a|"})
	r.AddMySQL(&stubCol{h1: "BBB", h2: "b|"})
	out := r.Header()
	// No ANSI bytes in nocolor mode.
	if strings.ContainsAny(out, "\x1b") {
		t.Errorf("nocolor header has ANSI: %q", out)
	}
	want := "AAA BBB\na|b|\n"
	if out != want {
		t.Errorf("header = %q, want %q", out, want)
	}
}

func TestRendererBuildRow(t *testing.T) {
	r := NewRenderer(false, 15)
	r.AddSys(&stubCol{cells: []metric.Cell{{Text: "1"}, {Text: "2", Color: metric.Red}}})
	r.AddMySQL(&stubCol{cells: []metric.Cell{{Text: "3"}}})
	out := r.BuildRow()
	// sys cells + blue-bold sep, mysql cell + green sep. nocolor → bare text.
	want := "12|3|\n"
	if out != want {
		t.Errorf("row = %q, want %q", out, want)
	}
}

func TestRendererBuildRowColor(t *testing.T) {
	r := NewRenderer(true, 15)
	r.AddSys(&stubCol{cells: []metric.Cell{{Text: "x", Color: metric.Red}}})
	out := r.BuildRow()
	// red cell: \x1b[31mx\x1b[0m, then blue-bold "|": \x1b[34m\x1b[1m|\x1b[0m
	if !strings.Contains(out, "\x1b[31mx\x1b[0m") {
		t.Errorf("color row missing red cell: %q", out)
	}
	if !strings.Contains(out, "\x1b[34m\x1b[1m|\x1b[0m") {
		t.Errorf("color row missing blue-bold separator: %q", out)
	}
}

// TestRendererNoHeader verifies -noheader makes Header() return "".
func TestRendererNoHeader(t *testing.T) {
	r := NewRenderer(false, 15)
	r.SetHeaderOff(true)
	r.AddSys(&stubCol{h1: "AAA ", h2: "a|"})
	if out := r.Header(); out != "" {
		t.Errorf("noheader Header() = %q, want \"\"", out)
	}
}

// TestRendererCustomSep verifies --sep replaces the group-based separators
// with a single literal separator on data rows (headers are untouched).
// The separator appears between collectors (group boundaries); cells within
// one collector are concatenated directly.
func TestRendererCustomSep(t *testing.T) {
	r := NewRenderer(true, 15)
	r.SetSep(",")
	// One sys collector (red cell), one mysql collector.
	r.AddSys(&stubCol{cells: []metric.Cell{{Text: "1", Color: metric.Red}}})
	r.AddMySQL(&stubCol{cells: []metric.Cell{{Text: "3"}}})
	out := r.BuildRow()
	// The red cell keeps its color; the separators are the literal "," (no
	// color), replacing both the sys "|" and the mysql "|".
	want := "\x1b[31m1\x1b[0m,3,\n"
	if out != want {
		t.Errorf("custom-sep row = %q, want %q", out, want)
	}
}

// TestRendererCustomSepTab verifies --sep '\t' becomes a real tab.
func TestRendererCustomSepTab(t *testing.T) {
	r := NewRenderer(false, 15)
	r.SetSep(`\t`)
	// Two distinct collectors each contribute one cell, so the tab separator
	// lands between them (cells within one collector have no separator).
	r.AddSys(&stubCol{cells: []metric.Cell{{Text: "1"}}})
	r.AddSys(&stubCol{cells: []metric.Cell{{Text: "2"}}})
	if out := r.BuildRow(); out != "1\t2\t\n" {
		t.Errorf("tab-sep row = %q, want %q", out, "1\t2\t\n")
	}
}

func TestFormatBytesValueRaw(t *testing.T) {
	// Raw mode (ES-friendly): no suffix, integer bytes, right-aligned to the
	// wider of the two widths so adjacent raw columns stay separated.
	cases := []struct {
		in   float64
		want string
	}{
		{0, "      0"}, {512, "    512"}, {2048, "   2048"}, {1572864, "1572864"},
		{16777216000, "16777216000"},
	}
	for _, c := range cases {
		if got := FormatBytesValue(c.in, metric.UnitRaw, 6, 7); got != c.want {
			t.Errorf("FormatBytesValue(raw, %v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatBytesValueHuman(t *testing.T) {
	// Human mode delegates to FormatBytesKM (k/m suffixes).
	if got := FormatBytesValue(1572864, metric.UnitHuman, 6, 7); got != "   1.5m" {
		t.Errorf("FormatBytesValue(human, 1572864) = %q, want \"   1.5m\"", got)
	}
	if got := FormatBytesValue(2048, metric.UnitHuman, 5, 6); got != "    2k" {
		t.Errorf("FormatBytesValue(human, 2048) = %q, want \"    2k\"", got)
	}
}

func TestFormatPercentValue(t *testing.T) {
	// Raw keeps the float (ES-friendly); human rounds to int (Perl-style).
	if got := FormatPercentValue(99.6, metric.UnitRaw); got != "99.60" {
		t.Errorf("FormatPercentValue(raw, 99.6) = %q, want \"99.60\"", got)
	}
	if got := FormatPercentValue(99.6, metric.UnitHuman); got != "100" {
		t.Errorf("FormatPercentValue(human, 99.6) = %q, want \"100\"", got)
	}
}
