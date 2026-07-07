// Package main is the orzdba CLI entrypoint.
//
// It parses arguments (pflag), expands composite flags (-sys/-mysql/-innodb/
// -lazy) BEFORE the first sample (plan §7.10, fixing orzdba-go P1-10), prints
// the title, and runs the polling loop. System collectors (M2) are wired
// here; MySQL collectors (M4+) attach to the same loop.
//
// The Perl original accepts single-dash long flags like -sys/-mysql. pflag
// parses those as shorthand bundles (-s -y -s), so normalizeArgs rewrites
// single-dash multi-character flags to --form before pflag sees them
// (plan §14 risk mitigation).
package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"orzdba/internal/logsink"
	"orzdba/internal/metric"
	"orzdba/internal/mycol"
	"orzdba/internal/mysqlc"
	"orzdba/internal/render"
	"orzdba/internal/rtcol"
	"orzdba/internal/syscol"
)

// version is the build version. Overridden via -ldflags "-X main.version=...".
var version = "0.1.0-dev"

func main() {
	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if cfg.help {
		usage(os.Stdout)
		os.Exit(0)
	}

	// umask 077 so any created files (logs, tcprstat output) are 0600-by-default
	// (plan §8.4).
	syscall.Umask(0o077)

	// color: disabled by -nocolor or -L (plan §6: -L implies nocolor).
	color := !cfg.nocolor && cfg.logfile == ""

	ncpu := detectCPU()
	renderer := render.NewRenderer(color, cfg.headerPeriod)

	// System collectors. Created in Perl's column order: time, load, cpu,
	// swap, net, disk. CPU must precede disk in the renderer so BuildRow calls
	// cpu.Collect before disk.Collect (disk reads cpu's jiffies diffs); cpu is
	// also sampled once per tick in runLoop before BuildRow.
	var cpu *syscol.CPU
	needCPU := cfg.cpu || cfg.disk != ""
	if needCPU {
		cpu = syscol.NewCPU(ncpu, cfg.cpu)
	}
	if cfg.time {
		renderer.AddSys(&timeCol{})
	}
	if cfg.load {
		renderer.AddSys(syscol.NewLoad(ncpu))
	}
	if cfg.cpu {
		renderer.AddSys(cpu)
	}
	if cfg.swap {
		renderer.AddSys(syscol.NewSwap(cfg.interval))
	}
	if cfg.net != "" {
		if err := validateDevFlag("net", "n", cfg.net); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		renderer.AddSys(syscol.NewNet(cfg.net, cfg.interval))
	}
	if cfg.disk != "" {
		if err := validateDevFlag("disk", "d", cfg.disk); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		// Verify the device exists in /proc/diskstats (plan §11.1 startup error).
		// Only when /proc/diskstats is readable — on non-Linux dev hosts it's
		// absent, so we skip the check and let Collect degrade to zeros at
		// runtime (plan §9.7). This keeps the tool Linux-only for production
		// without false-positiving on macOS.
		if err := checkDiskDevice(cfg.disk); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}
		renderer.AddSys(syscol.NewDisk(cpu, cfg.disk, ncpu))
	}

	// MySQL collectors. Open one long-lived connection (plan §9.3) and share a
	// StatusSource so all -mysql submodules read from a single SHOW GLOBAL STATUS
	// query per tick (plan §7.9). Only com/hit/threads/bytes are wired so far
	// (M4 subset); innodb/slave/semi arrive in M5/M6.
	var status *mycol.StatusSource
	if cfg.mysql {
		mc := mysqlc.ResolveCredentials(mysqlc.ResolveOpts{
			CLIUser: cfg.mysqlUser, CLIPass: cfg.mysqlPass,
			CLIHost: cfg.host, CLIPort: cfg.port, CLISocket: cfg.socket,
			DefaultsFile: cfg.mysqlDefaultsFile, DefaultsGroup: cfg.mysqlDefaultsGrp,
			Timeout: cfg.mysqlTimeout, TLS: cfg.mysqlTLS,
		})
		fmt.Fprintf(os.Stderr, "connecting to %s ...\n", mc.SafeDSN())
		db, err := mysqlc.Open(&mc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: cannot connect to MySQL (%s): %v\n", mc.SafeDSN(), err)
			os.Exit(1)
		}
		status = mycol.NewStatusSource(db, cfg.interval, cfg.mysqlTimeout)
		// Collectors in Perl column order: com, hit, innodb_rows, innodb_pages,
		// innodb_data, innodb_log, innodb_status, threads, bytes, slave, semi.
		if cfg.com {
			renderer.AddMySQL(mycol.NewCom(status))
		}
		if cfg.hit != "" {
			renderer.AddMySQL(mycol.NewHit(status, cfg.hit == "full"))
		}
		if cfg.innodbRows {
			renderer.AddMySQL(mycol.NewInnodbRows(status))
		}
		if cfg.innodbPages {
			renderer.AddMySQL(mycol.NewInnodbPages(status))
		}
		if cfg.innodbData {
			renderer.AddMySQL(mycol.NewInnodbData(status))
		}
		if cfg.innodbLog {
			renderer.AddMySQL(mycol.NewInnodbLog(status))
		}
		if cfg.innodbStatus {
			renderer.AddMySQL(mycol.NewInnodbStatus(status))
		}
		if cfg.threads {
			renderer.AddMySQL(mycol.NewThreads(status))
		}
		if cfg.bytes {
			renderer.AddMySQL(mycol.NewBytes(status))
		}
		if cfg.slave {
			renderer.AddMySQL(mycol.NewSlave(status))
		}
		if cfg.semi {
			renderer.AddMySQL(mycol.NewSemi(status))
		}
	}

	// tcprstat RT collector (M7). -rt implies mysql=true (Perl) so the MySQL
	// connection/title above are already open. The subprocess is started once
	// and tracked by PID for precise cleanup (plan §9.5).
	var rtCol *rtcol.Collector
	if cfg.rt {
		rtCol = rtcol.New(cfg.port, primaryIP())
		if err := rtCol.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}
		renderer.AddMySQL(rtCol)
	}

	sink, err := logsink.New(cfg.logfile, cfg.logfileByDay)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot open logfile %q: %v\n", cfg.logfile, err)
		os.Exit(1)
	}
	// writeTitle prints the full title block (banner + host/IP + DB name + Var
	// lines). Called at startup and on daily-log rotation.
	writeTitle := func(w io.Writer) {
		fmt.Fprint(w, buildTitle(color))
		if status != nil {
			fmt.Fprint(w, mysqlTitleLine(status, color))
			fmt.Fprint(w, mysqlVarsLines(status, color))
		}
	}
	writeTitle(sink)

	// Signal handling: SIGINT/SIGTERM → cleanup + exit 0 (plan §11.3).
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		if rtCol != nil {
			rtCol.Stop()
		}
		_ = sink.Close()
		if color {
			fmt.Fprint(os.Stderr, "\x1b[31m\nExit Now...\n\n\x1b[0m")
		} else {
			fmt.Fprint(os.Stderr, "\nExit Now...\n\n")
		}
		os.Exit(0)
	}()

	runLoop(cfg, renderer, cpu, needCPU, status, sink, writeTitle)
}

// runLoop is the polling loop. It mirrors the Perl original's ordering:
// day-rollover check, exit-check, header (every period), increment, collect,
// render, sleep.
func runLoop(cfg *config, r *render.Renderer, cpu *syscol.CPU, needCPU bool, status *mycol.StatusSource, sink logsink.Sink, writeTitle func(io.Writer)) {
	var mycount int
	// remaining is the local -C budget; day-rollover subtracts mycount from it
	// (Perl §7.13: `count -= mycount`), so we don't mutate cfg.count.
	remaining, countSet := cfg.count, cfg.countSet
	for {
		// -logfile_by_day: rotate at midnight, reprint title, reset counters.
		if rs, ok := sink.(logsink.RotateSink); ok && rs.MaybeRotate(time.Now()) {
			writeTitle(sink)
			if countSet {
				remaining -= mycount
			}
			mycount = 0
			r.ResetHeaderCounter()
		}
		// -C: exit when mycount > count. This mirrors the Perl original's
		// `while(1){ if ($mycount > $count) { exit } ... $mycount++; print }`
		// exactly — including its off-by-one (so -C N emits N+1 rows). Kept
		// faithful because plan §15.1 demands line-by-line parity with Perl.
		if countSet && mycount > remaining {
			return
		}
		if mycount%cfg.headerPeriod == 0 {
			fmt.Fprint(sink, r.Header())
		}
		mycount++
		if needCPU {
			cpu.Sample()
		}
		if status != nil {
			status.Fetch()
		}
		fmt.Fprint(sink, r.BuildRow())
		time.Sleep(time.Duration(cfg.interval) * time.Second)
	}
}

// mysqlTitleLine returns the "DB  : <databases>" title line, listing non-system
// databases (excludes information_schema, mysql, test — matching Perl's grep
// filter). On error it returns an empty line so the title still renders.
func mysqlTitleLine(status *mycol.StatusSource, color bool) string {
	a := render.NewANSI(color)
	dbs := status.Databases()
	var b strings.Builder
	b.WriteString(a.Escape(metric.Red))
	b.WriteString("DB  : ")
	b.WriteString(a.Escape(metric.Yellow))
	b.WriteString(strings.Join(dbs, "|"))
	b.WriteString(a.Reset())
	b.WriteString("\n")
	return b.String()
}

// varGroup1/varGroup2 are the two SHOW VARIABLES groups the Perl print_title
// displays (appendix B). We print them in this hardcoded list order (a fixed,
// logical grouping), which differs from the Perl original's MySQL-sorted-by-
// name order — plan §15 explicitly allows this ordering difference.
var varGroup1 = []string{
	"sync_binlog", "max_connections", "max_user_connections", "max_connect_errors",
	"table_open_cache", "table_definition_cache", "thread_cache_size", "binlog_format",
	"open_files_limit", "max_binlog_size", "max_binlog_cache_size",
}
var varGroup2 = []string{
	"innodb_flush_log_at_trx_commit", "innodb_flush_method", "innodb_buffer_pool_size",
	"innodb_max_dirty_pages_pct", "innodb_log_buffer_size", "innodb_log_file_size",
	"innodb_log_files_in_group", "innodb_thread_concurrency", "innodb_file_per_table",
	"innodb_adaptive_hash_index", "innodb_open_files", "innodb_io_capacity",
	"innodb_read_io_threads", "innodb_write_io_threads", "innodb_adaptive_flushing",
	"innodb_lock_wait_timeout",
}

// varByteSized are the 5 variables printed with G/M auto-formatting (Perl
// print_vars lines 505-508).
var varByteSized = map[string]bool{
	"innodb_buffer_pool_size": true,
	"innodb_log_file_size":    true,
	"innodb_log_buffer_size":  true,
	"max_binlog_cache_size":   true,
	"max_binlog_size":         true,
}

// mysqlVarsLines renders the "Var : key[val] key[val] ..." title block (the
// print_vars section of Perl print_title). 3 vars per line, 6-space indent on
// continuation; two groups separated by a blank line. Byte-sized vars use G/M.
func mysqlVarsLines(status *mycol.StatusSource, color bool) string {
	a := render.NewANSI(color)
	var b strings.Builder
	b.WriteString(a.Escape(metric.Red))
	b.WriteString("Var : ")
	b.WriteString(a.Reset())
	emit := func(group []string) {
		vals, _ := status.ShowVariables(group)
		// Preserve MySQL's returned (sorted) order; iterate the group list and
		// print whichever exist.
		cnt := 0
		for _, name := range group {
			val, ok := vals[name]
			if !ok {
				continue
			}
			if varByteSized[name] {
				if f, err := strconv.ParseFloat(val, 64); err == nil {
					val = render.FormatBytesAutoG(f)
				}
			}
			b.WriteString(a.Escape(metric.Magenta))
			b.WriteString(name)
			b.WriteString(a.Escape(metric.White))
			b.WriteString("[")
			b.WriteString(val)
			b.WriteString("] ")
			b.WriteString(a.Reset())
			cnt++
			if cnt%3 == 0 {
				b.WriteString("\n      ")
			}
		}
	}
	emit(varGroup1)
	b.WriteString("\n\n      ")
	emit(varGroup2)
	b.WriteString("\n")
	return b.String()
}

// timeCol emits the current time column (sys-group styled). The timestamp
// is formatted "YYYY-MM-DD HH:MM:SS" (19 chars) — a deviation from the Perl
// original's "HH:MM:SS" so each log line carries its full date (useful for
// multi-day log files). The header field is widened to match.
type timeCol struct{}

func (*timeCol) Name() string { return "time" }
func (*timeCol) Headline() (string, string) {
	const w = 19 // "2006-01-02 15:04:05"
	return strings.Repeat("-", w) + " ", "  time" + strings.Repeat(" ", w-6) + "|"
}
func (*timeCol) Collect() []metric.Cell {
	return []metric.Cell{
		{Text: time.Now().Format("2006-01-02 15:04:05"), Color: metric.Yellow},
	}
}

// detectCPU returns the logical CPU count from /proc/cpuinfo, falling back to
// runtime.NumCPU() when /proc is unavailable (non-Linux dev hosts). The
// fallback is dev-only; production runs on Linux with /proc.
func detectCPU() int {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err == nil {
		if n := syscol.CountCPU(strings.NewReader(string(data))); n > 0 {
			return n
		}
	}
	return runtime.NumCPU()
}

// validateDevFlag rejects device-name values that look like a flag (start with
// '-'). This catches the common mistake of writing `-d -n eth0` (forgetting
// the device name) — pflag would otherwise silently consume `-n` as -d's
// value, producing a confusing "device not found" error later. shortFlag is
// the short form (e.g. "d", "n") used in the hint.
func validateDevFlag(name, shortFlag, val string) error {
	if strings.HasPrefix(val, "-") {
		return fmt.Errorf("invalid %s value %q (looks like a flag, not a device name) — did you forget the device name after -%s?", name, val, shortFlag)
	}
	return nil
}

// checkDiskDevice verifies the device appears in /proc/diskstats. It returns
// nil when the device is found, OR when /proc/diskstats is unreadable (non-Linux
// dev hosts have no /proc) — in the latter case we can't check, so we skip the
// startup error and let the collector degrade to zeros at runtime (plan §9.7).
// It returns an error only when /proc/diskstats IS readable and the device is
// absent — a genuine bad device name on Linux (plan §11.1).
func checkDiskDevice(dev string) error {
	data, err := os.ReadFile("/proc/diskstats")
	if err != nil {
		return nil // /proc absent (non-Linux) — skip check, degrade at runtime
	}
	if !findDiskDevice(data, dev) {
		return fmt.Errorf("disk device %q not found in /proc/diskstats", dev)
	}
	return nil
}

// findDiskDevice reports whether dev appears as a device name (field[2]) in
// /proc/diskstats content. Pure so it can be tested with a sample.
func findDiskDevice(data []byte, dev string) bool {
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[2] == dev {
			return true
		}
	}
	return false
}

// usage prints the custom help (plan §2.4 P2-23: orzdba-go had none). The text
// mirrors the Perl original's usage block plus the new --long flags.
func usage(w *os.File) {
	fmt.Fprint(w, `
==========================================================================================
Info  :
        orzdba `+version+` — Go rewrite of the Taobao DBA orzdba.
Usage :
Command line options :

   -h,--help           Print Help Info.
   -i,--interval       Time(second) Interval. (default 1)
   -C,--count          Times.
   -t,--time           Print The Current Time.
   -nocolor            Print NO Color.

   -l,--load           Print Load Info.
   -c,--cpu            Print Cpu  Info.
   -s,--swap           Print Swap Info.
   -d,--disk           Print Disk Info.
   -n,--net            Print Net  Info.

   -P,--port           Port number to use for mysql connection(default 3306).
   -S,--socket         Socket file to use for mysql connection.
   -H,--host           MySQL host (default 127.0.0.1).
   --mysql-user        MySQL user.
   --mysql-pass        MySQL password (plaintext, debug only).
   --mysql-defaults-file  Path to a my.cnf to read instead of the default search.
   --mysql-defaults-group my.cnf section (default client).
   --mysql-timeout     SQL/connect timeout (default 1s).
   --mysql-tls         Enable TLS.

   -com                Print MySQL Status(Com_select,Com_insert,Com_update,Com_delete).
   -hit                Print Innodb Hit%. (--hit full for 5-column extended hit)
   -innodb_rows        Print Innodb Rows Status.
   -innodb_pages       Print Innodb Buffer Pool Pages Status.
   -innodb_data        Print Innodb Data Status.
   -innodb_log         Print Innodb Log  Status.
   -innodb_status      Print Innodb Status from 'Show Engine Innodb Status'.
   -T,--threads        Print Threads Status(inc. thread cache hit%).
   -B,--bytes          Print Bytes received from/send to MySQL.
   -rt                 Print MySQL DB RT(us).

   -mysql              Print MySQLInfo (include -t,-com,-hit,-T,-B).
   -innodb             Print InnodbInfo(include -t,-innodb_pages,-innodb_data,-innodb_log,-innodb_status)
   -sys                Print SysInfo   (include -t,-l,-c,-s).
   -lazy               Print Info      (include -t,-l,-c,-s,-com,-hit).
   -slave              Print SHOW SLAVE STATUS.
   -semi               Print semi-sync replication status.
   --tps-mode          iud (default) | commit.
   --header-period     Header repeat period (default 15).

   -L,--logfile        Print to Logfile. (implies -nocolor)
   -logfile_by_day     One day a logfile, suffix 'yyyy-mm-dd'; valid with -L.

Sample :
   shell> nohup ./orzdba -lazy -d sda -C 5 -i 2 -L /tmp/orzdba.log  > /dev/null 2>&1 &
==========================================================================================
`)
}

// buildTitle returns the non-DB title block (banner + host + IP). The DB
// section (SHOW DATABASES / SHOW VARIABLES) is added in M3.
func buildTitle(color bool) string {
	a := render.NewANSI(color)
	hostname, _ := os.Hostname()
	ip := primaryIP()
	date := time.Now().Format("2006-01-02")

	var b strings.Builder
	// Banner: GREEN, heredoc, then "'=============== Date : ... ==============='".
	b.WriteString(a.Escape(metric.Green))
	b.WriteString("\n.=================================================.\n")
	b.WriteString("|       Welcome to use the orzdba tool !          | \n")
	b.WriteString("|          Yep...Chinese English~                 |\n")
	b.WriteString(a.Escape(metric.Green))
	b.WriteString("'=============== ")
	b.WriteString(a.Escape(metric.Red))
	b.WriteString("Date : ")
	b.WriteString(date)
	b.WriteString(a.Escape(metric.Green))
	b.WriteString(" ==============='\n\n")
	b.WriteString(a.Reset())
	// HOST/IP line.
	b.WriteString(a.Escape(metric.Red))
	b.WriteString("HOST: ")
	b.WriteString(a.Escape(metric.Yellow))
	b.WriteString(hostname)
	b.WriteString(a.Escape(metric.Red))
	b.WriteString("   IP: ")
	b.WriteString(a.Escape(metric.Yellow))
	b.WriteString(ip)
	b.WriteString(a.Reset())
	b.WriteString("\n")
	return b.String()
}

// primaryIP returns the first non-loopback IPv4 address, or "?" if none.
func primaryIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "?"
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if v4 := ipnet.IP.To4(); v4 != nil {
			return v4.String()
		}
	}
	return "?"
}
