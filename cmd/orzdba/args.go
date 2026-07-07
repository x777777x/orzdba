package main

import (
	"fmt"
	"strings"
	"time"

	flag "github.com/spf13/pflag"
)

// config holds the parsed, composite-expanded flags.
type config struct {
	help bool

	interval     int
	count        int
	countSet     bool
	headerPeriod int
	nocolor      bool
	time         bool
	load         bool
	cpu          bool
	swap         bool
	disk         string
	net          string

	// Composite flags (tracked so we only expand them when explicitly passed).
	sys    bool
	lazy   bool
	innodb bool

	// MySQL (M3/M4 — collected now, wired later).
	port              int
	socket            string
	host              string
	mysqlUser         string
	mysqlPass         string
	mysqlDefaultsFile string
	mysqlDefaultsGrp  string
	mysqlTimeout      time.Duration
	mysqlTLS          bool
	mysql             bool // any mysql submodule enabled
	com               bool
	hit               string // "" off; "1" on (1-col); "full" 5-col extended
	innodbRows        bool
	innodbPages       bool
	innodbData        bool
	innodbLog         bool
	innodbStatus      bool
	threads           bool
	bytes             bool
	rt                bool
	slave             bool
	semi              bool
	tpsMode           string

	logfile      string
	logfileByDay bool
}

// parseArgs parses argv (already shell-split), applying the single-dash long
// flag shim, pflag parsing, and composite flag expansion. Composite expansion
// happens here — before any sample is taken (plan §7.10, fixing orzdba-go
// P1-10 where first-sample diffs exploded because -mysql was expanded too late).
func parseArgs(argv []string) (*config, error) {
	argv = normalizeArgs(argv)

	fs := flag.NewFlagSet("orzdba", flag.ContinueOnError)
	fs.SetOutput(nil)
	fs.SortFlags = false

	c := &config{interval: 1, port: 3306, headerPeriod: 15, mysqlTimeout: time.Second, tpsMode: "iud", host: "127.0.0.1", mysqlDefaultsGrp: "client"}

	fs.BoolVarP(&c.help, "help", "h", false, "")
	fs.IntVarP(&c.interval, "interval", "i", 1, "")
	fs.IntVarP(&c.count, "count", "C", 0, "")
	fs.BoolVarP(&c.time, "time", "t", false, "")
	fs.BoolVar(&c.nocolor, "nocolor", false, "")

	fs.BoolVarP(&c.load, "load", "l", false, "")
	fs.BoolVarP(&c.cpu, "cpu", "c", false, "")
	fs.BoolVarP(&c.swap, "swap", "s", false, "")
	fs.StringVarP(&c.disk, "disk", "d", "", "")
	fs.StringVarP(&c.net, "net", "n", "", "")

	fs.BoolVar(&c.com, "com", false, "")
	// -hit takes an optional value: bare "-hit" → "1" (1-column), "-hit full"
	// → "full" (5-column extended). NoOptDefVal lets pflag accept bare -hit.
	fs.StringVar(&c.hit, "hit", "", "")
	fs.Lookup("hit").NoOptDefVal = "1"
	fs.BoolVar(&c.innodbRows, "innodb_rows", false, "")
	fs.BoolVar(&c.innodbPages, "innodb_pages", false, "")
	fs.BoolVar(&c.innodbData, "innodb_data", false, "")
	fs.BoolVar(&c.innodbLog, "innodb_log", false, "")
	fs.BoolVar(&c.innodbStatus, "innodb_status", false, "")
	fs.BoolVarP(&c.threads, "threads", "T", false, "")
	fs.BoolVarP(&c.bytes, "bytes", "B", false, "")
	fs.BoolVar(&c.rt, "rt", false, "")
	fs.BoolVar(&c.slave, "slave", false, "")
	fs.BoolVar(&c.semi, "semi", false, "")

	fs.BoolVar(&c.mysql, "mysql", false, "")
	fs.BoolVar(&c.innodb, "innodb", false, "")
	fs.BoolVar(&c.sys, "sys", false, "")
	fs.BoolVar(&c.lazy, "lazy", false, "")

	fs.IntVarP(&c.port, "port", "P", 3306, "")
	fs.StringVarP(&c.socket, "socket", "S", "", "")
	fs.StringVarP(&c.host, "host", "H", "127.0.0.1", "")
	fs.StringVar(&c.mysqlUser, "mysql-user", "", "")
	fs.StringVar(&c.mysqlPass, "mysql-pass", "", "")
	fs.StringVar(&c.mysqlDefaultsFile, "mysql-defaults-file", "", "")
	fs.StringVar(&c.mysqlDefaultsGrp, "mysql-defaults-group", "client", "")
	fs.DurationVar(&c.mysqlTimeout, "mysql-timeout", time.Second, "")
	fs.BoolVar(&c.mysqlTLS, "mysql-tls", false, "")
	fs.StringVar(&c.tpsMode, "tps-mode", "iud", "")
	fs.IntVar(&c.headerPeriod, "header-period", 15, "")

	fs.StringVarP(&c.logfile, "logfile", "L", "", "")
	fs.BoolVar(&c.logfileByDay, "logfile_by_day", false, "")

	if err := fs.Parse(argv); err != nil {
		return nil, friendlyParseErr(err)
	}

	// No arguments → usage (Perl: if (!scalar(%opt)) print_usage).
	if len(argv) == 0 {
		c.help = true
		return c, nil
	}

	// Composite expansion uses fs.Changed so a leaf like -com (which sets the
	// mysql variable) does NOT trigger the -mysql composite (which would also
	// force -hit/-T/-B). This mirrors Perl's $opt{'mysql'} being true only when
	// -mysql is explicitly on the command line.
	c.expand(fs.Changed)
	c.countSet = fs.Changed("count")
	return c, nil
}

// expand applies composite-flag expansion, mirroring Perl's get_options.
func (c *config) expand(changed func(string) bool) {
	// Leaf MySQL modules each imply mysql=true (the variable).
	if c.com || c.hit != "" || c.threads || c.bytes || c.rt ||
		c.innodbRows || c.innodbPages || c.innodbData || c.innodbLog || c.innodbStatus ||
		c.slave || c.semi {
		c.mysql = true
	}

	// -sys: -t -l -c -s (no mysql).
	if changed("sys") {
		c.time = true
		c.load = true
		c.cpu = true
		c.swap = true
	}
	// -lazy: -t -l -c -s -com -hit (mysql).
	if changed("lazy") {
		c.time = true
		c.load = true
		c.cpu = true
		c.swap = true
		c.com = true
		if c.hit == "" {
			c.hit = "1"
		}
		c.mysql = true
	}
	// -mysql: -t -com -hit -T -B.
	if changed("mysql") {
		c.time = true
		c.com = true
		if c.hit == "" {
			c.hit = "1" // don't clobber an explicit "-hit full"
		}
		c.threads = true
		c.bytes = true
		c.mysql = true
	}
	// -innodb: -t -innodb_pages -innodb_data -innodb_log -innodb_status.
	if changed("innodb") {
		c.time = true
		c.innodbPages = true
		c.innodbData = true
		c.innodbLog = true
		c.innodbStatus = true
		c.mysql = true
	}
}

// normalizeArgs rewrites single-dash multi-character flags (e.g. -sys, -mysql,
// -logfile_by_day, -nocolor) to double-dash form so pflag — which treats -sys
// as the shorthand bundle -s -y -s — parses them as long flags. Single-char
// shorts (-t, -d, -C, -i, ...) and already-double-dashed args are untouched.
//
// It also merges "-hit full" (the orzdba-go extended-hit syntax, space-
// separated) into "--hit=full", because pflag's NoOptDefVal on the hit flag
// only accepts "="-attached values, not space-separated ones.
func normalizeArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		// -hit full / --hit full → --hit=full
		if (a == "-hit" || a == "--hit") && i+1 < len(args) && args[i+1] == "full" {
			out = append(out, "--hit=full")
			i++ // consume "full"
			continue
		}
		if len(a) > 2 && a[0] == '-' && a[1] != '-' && isLongName(a[1:]) {
			a = "-" + a // -sys -> --sys
		}
		out = append(out, a)
	}
	return out
}

// isLongName reports whether s is a plausible long-flag name: 2+ chars of
// [A-Za-z0-9_]. This excludes "-d=sda" (= is not a name char) and "-5".
func isLongName(s string) bool {
	if len(s) < 2 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return false
		}
	}
	return true
}

// friendlyParseErr rewrites pflag's cryptic parse errors into a hint with a
// concrete example. Without this, `-d` with no value yields "flag needs an
// argument: 'd' in -d", which doesn't tell the user what -d expects.
func friendlyParseErr(err error) error {
	msg := err.Error()
	if strings.HasPrefix(msg, "flag needs an argument:") {
		// pflag format: "flag needs an argument: 'X' in -X" — extract the char.
		if i := strings.IndexByte(msg, '\''); i >= 0 && i+1 < len(msg) {
			ch := msg[i+1]
			return fmt.Errorf("-%c requires a value (e.g. -%c %s); run 'orzdba -h' for usage",
				ch, ch, exampleValue(ch))
		}
	}
	if strings.HasPrefix(msg, "unknown flag") || strings.HasPrefix(msg, "unknown shorthand") {
		return fmt.Errorf("%s; run 'orzdba -h' for usage", msg)
	}
	return err
}

// exampleValue returns a representative value for a value-taking flag, used in
// parse-error hints.
func exampleValue(ch byte) string {
	switch ch {
	case 'd':
		return "sda"
	case 'n':
		return "eth0"
	case 'P':
		return "3306"
	case 'S':
		return "/tmp/mysql.sock"
	case 'H':
		return "127.0.0.1"
	case 'L':
		return "/tmp/orzdba.log"
	case 'i':
		return "1"
	case 'C':
		return "5"
	default:
		return "<value>"
	}
}
