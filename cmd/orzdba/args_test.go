package main

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeArgsSingleDashLong(t *testing.T) {
	cases := []struct{ in, want string }{
		{"-sys", "--sys"},
		{"-mysql", "--mysql"},
		{"-logfile_by_day", "--logfile_by_day"},
		{"-nocolor", "--nocolor"},
		{"-innodb_rows", "--innodb_rows"},
	}
	for _, c := range cases {
		got := normalizeArgs([]string{c.in})
		if got[0] != c.want {
			t.Errorf("normalizeArgs(%q)[0] = %q, want %q", c.in, got[0], c.want)
		}
	}
}

func TestNormalizeArgsShortFlagsUntouched(t *testing.T) {
	// Single-char shorts must NOT be rewritten (they're real pflag shorthands).
	for _, a := range []string{"-d", "-n", "-C", "-i", "-t", "-l", "-c", "-s", "-T", "-B", "-P", "-S", "-L", "-h"} {
		got := normalizeArgs([]string{a})
		if got[0] != a {
			t.Errorf("normalizeArgs(%q) = %q, want unchanged", a, got[0])
		}
	}
}

func TestNormalizeArgsValuesUntouched(t *testing.T) {
	got := normalizeArgs([]string{"-d", "sda", "-n", "eth0", "-C", "5"})
	want := []string{"-d", "sda", "-n", "eth0", "-C", "5"}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNormalizeArgsHitFullMerge(t *testing.T) {
	// "-hit full" and "--hit full" must collapse to "--hit=full" so pflag's
	// NoOptDefVal on the hit flag doesn't treat "full" as a positional arg.
	for _, in := range [][]string{{"-hit", "full"}, {"--hit", "full"}} {
		got := normalizeArgs(in)
		if len(got) != 1 || got[0] != "--hit=full" {
			t.Errorf("normalizeArgs(%v) = %v, want [\"--hit=full\"]", in, got)
		}
	}
}

func TestNormalizeArgsBareHit(t *testing.T) {
	// Bare -hit (no "full" after) → --hit (NoOptDefVal="1" applies at parse).
	got := normalizeArgs([]string{"-hit", "-C", "5"})
	if got[0] != "--hit" {
		t.Errorf("bare -hit = %q, want --hit", got[0])
	}
}

// ---- expand (composite flags) ----

func TestExpandSys(t *testing.T) {
	c := &config{sys: true}
	c.expand(func(name string) bool { return name == "sys" })
	for _, b := range []struct {
		name string
		got  bool
	}{
		{"time", c.time}, {"load", c.load}, {"cpu", c.cpu}, {"swap", c.swap},
	} {
		if !b.got {
			t.Errorf("%s not set by -sys", b.name)
		}
	}
	if c.mysql {
		t.Error("-sys must not set mysql")
	}
}

func TestExpandMysqlComposite(t *testing.T) {
	c := &config{mysql: true}
	c.expand(func(name string) bool { return name == "mysql" })
	for _, b := range []struct {
		name string
		got  bool
	}{
		{"time", c.time}, {"com", c.com}, {"threads", c.threads}, {"bytes", c.bytes},
	} {
		if !b.got {
			t.Errorf("%s not set by -mysql", b.name)
		}
	}
	if c.hit != "1" {
		t.Errorf("-mysql should set hit=\"1\", got %q", c.hit)
	}
}

func TestExpandMysqlDoesNotClobberHitFull(t *testing.T) {
	// -mysql -hit full: the explicit "full" must survive the -mysql composite.
	c := &config{mysql: true, hit: "full"}
	c.expand(func(name string) bool { return name == "mysql" })
	if c.hit != "full" {
		t.Errorf("-mysql clobbered -hit full: hit=%q, want \"full\"", c.hit)
	}
}

func TestExpandLeafDoesNotTriggerMysqlComposite(t *testing.T) {
	// -com alone sets mysql=true (leaf) but must NOT force -hit/-T/-B (only the
	// explicit -mysql composite does that).
	c := &config{com: true}
	c.expand(func(name string) bool { return false }) // no composite flag was passed
	if !c.mysql {
		t.Error("-com should set mysql=true (leaf implies mysql)")
	}
	if c.hit != "" || c.threads || c.bytes || c.time {
		t.Errorf("-com alone must not expand to -hit/-T/-B/-t: hit=%q threads=%v bytes=%v time=%v", c.hit, c.threads, c.bytes, c.time)
	}
}

// ---- friendlyParseErr ----

func TestFriendlyParseErrFlagNeedsArg(t *testing.T) {
	err := friendlyParseErr(errors.New("flag needs an argument: 'd' in -d"))
	if !strings.Contains(err.Error(), "-d requires a value") || !strings.Contains(err.Error(), "-h") {
		t.Errorf("friendly error = %q, want -d hint + -h", err.Error())
	}
}

func TestFriendlyParseErrUnknownFlag(t *testing.T) {
	err := friendlyParseErr(errors.New("unknown flag: --bogus"))
	if !strings.Contains(err.Error(), "-h") {
		t.Errorf("unknown-flag error should hint -h: %q", err.Error())
	}
}

func TestFriendlyParseErrPassThrough(t *testing.T) {
	err := friendlyParseErr(errors.New("some other error"))
	if err.Error() != "some other error" {
		t.Errorf("passthrough error = %q, want unchanged", err.Error())
	}
}

// ---- validateDevFlag ----

func TestValidateDevFlag(t *testing.T) {
	if err := validateDevFlag("disk", "d", "sda"); err != nil {
		t.Errorf("valid disk name errored: %v", err)
	}
	if err := validateDevFlag("net", "n", "eth0"); err != nil {
		t.Errorf("valid net name errored: %v", err)
	}
	if err := validateDevFlag("disk", "d", "-n"); err == nil {
		t.Error("flag-like disk name should error")
	}
	if err := validateDevFlag("net", "n", "-C"); err == nil {
		t.Error("flag-like net name should error")
	}
}

// ---- end-to-end parseArgs ----

func TestParseArgsHitFull(t *testing.T) {
	cfg, err := parseArgs([]string{"-hit", "full"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.hit != "full" {
		t.Errorf("hit = %q, want \"full\"", cfg.hit)
	}
	if !cfg.mysql {
		t.Error("-hit should set mysql=true")
	}
}

func TestParseArgsMysqlHitFull(t *testing.T) {
	cfg, err := parseArgs([]string{"-mysql", "-hit", "full"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.hit != "full" {
		t.Errorf("hit = %q, want \"full\" (not clobbered by -mysql)", cfg.hit)
	}
	if !cfg.com || !cfg.threads || !cfg.bytes || !cfg.time {
		t.Errorf("-mysql composite missing members: com=%v threads=%v bytes=%v time=%v", cfg.com, cfg.threads, cfg.bytes, cfg.time)
	}
}

func TestParseArgsSysComposite(t *testing.T) {
	cfg, err := parseArgs([]string{"-sys"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.time || !cfg.load || !cfg.cpu || !cfg.swap {
		t.Errorf("-sys members: time=%v load=%v cpu=%v swap=%v", cfg.time, cfg.load, cfg.cpu, cfg.swap)
	}
	if cfg.mysql {
		t.Error("-sys should not enable mysql")
	}
}

func TestParseArgsNoArgsShowsHelp(t *testing.T) {
	cfg, err := parseArgs([]string{})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.help {
		t.Error("no args should set help=true")
	}
}

func TestParseArgsDMissingValue(t *testing.T) {
	_, err := parseArgs([]string{"-d"})
	if err == nil {
		t.Fatal("-d with no value should error")
	}
	if !strings.Contains(err.Error(), "-d requires a value") {
		t.Errorf("error = %q, want -d requires a value", err.Error())
	}
}
