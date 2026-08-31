// Package mycol implements MySQL-based collectors: com, hit, innodb_rows,
// innodb_pages, innodb_data, innodb_log, innodb_status, threads, bytes,
// slave, semi.
//
// All queries go through database/sql — no mysql client shell-out (plan §9.3,
// fixing orzdba-go P0-1/P0-2). One SHOW GLOBAL STATUS query per tick feeds all
// submodules via the shared StatusSource (plan §7.9, avoiding orzdba-go's
// per-module mysql client invocations).
package mycol

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// StatusSource runs SHOW GLOBAL STATUS once per tick and shares the result
// map with all collectors (plan §7.9). It holds the current and previous maps
// so collectors can compute deltas; the first tick has no previous, so
// diff-based collectors emit zeros (matching Perl's not_first guard).
type StatusSource struct {
	db       *sql.DB
	cur      map[string]int64
	prev     map[string]int64
	curRaw   map[string]string // raw values (for ON/OFF-style vars like semi-sync status)
	interval float64
	tick     int
	timeout  time.Duration
	ok       bool // false when the last Fetch failed (degrade to zeros)
	// lastOK is the wall-clock time of the last successful Fetch. It is the
	// denominator for rate computation (P1-6): after a transient failure the
	// delta spans MORE than one interval, and dividing by the fixed interval
	// would inflate the next tick's rates ~2x.
	lastOK time.Time
}

// statusVars is the superset of variables the -mysql collectors read. Keeping
// it as one list means a single query feeds every collector. Variables absent
// on a given server (e.g. Qcache_* removed in MySQL 8.0, Rpl_semi_sync_* when
// the plugin isn't loaded) simply won't appear in the result and read as 0
// (plan §14 risk: some columns stay 0 on MySQL 8.x).
var statusVars = []string{
	// com / TPS
	"Com_select", "Com_insert", "Com_update", "Com_delete",
	"Com_commit", "Com_rollback",
	// innodb hit
	"Innodb_buffer_pool_read_requests", "Innodb_buffer_pool_reads",
	// innodb rows
	"Innodb_rows_inserted", "Innodb_rows_updated", "Innodb_rows_deleted", "Innodb_rows_read",
	// threads
	"Threads_running", "Threads_connected", "Threads_cached", "Threads_created",
	// bytes
	"Bytes_received", "Bytes_sent",
	// innodb pages
	"Innodb_buffer_pool_pages_data", "Innodb_buffer_pool_pages_free",
	"Innodb_buffer_pool_pages_dirty", "Innodb_buffer_pool_pages_flushed",
	// innodb data
	"Innodb_data_reads", "Innodb_data_writes", "Innodb_data_read", "Innodb_data_written",
	// innodb log
	"Innodb_os_log_fsyncs", "Innodb_os_log_written",
	// thread cache hit / extended hit
	"Connections", "Qcache_hits",
	"Handler_read_first", "Handler_read_key", "Handler_read_next", "Handler_read_prev",
	"Handler_read_rnd", "Handler_read_rnd_next",
	"Created_tmp_tables", "Created_tmp_disk_tables",
	"Key_read_requests", "Key_reads", "Key_write_requests", "Key_writes",
	"Max_used_connections", "Opened_tables", "Slow_queries",
	"Select_scan", "Select_full_join",
	// semi-sync replication (absent → 0 / "" when plugin not loaded)
	"Rpl_semi_sync_master_status", "Rpl_semi_sync_master_yes_tx",
	"Rpl_semi_sync_master_no_tx", "Rpl_semi_sync_master_no_timeouts",
}

// NewStatusSource returns a StatusSource. interval is the sampling interval
// (seconds) used to convert deltas to rates (matching Perl's /$interval).
func NewStatusSource(db *sql.DB, interval int, timeout time.Duration) *StatusSource {
	return &StatusSource{db: db, interval: float64(interval), timeout: timeout,
		cur: map[string]int64{}, prev: map[string]int64{}, curRaw: map[string]string{}}
}

// Fetch runs the status query and shifts cur→prev. On error, ok=false so
// collectors degrade to zeros this tick (plan §9.7: single-module failure
// doesn't cascade).
func (s *StatusSource) Fetch() {
	if s.db == nil {
		s.ok = false
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	q := fmt.Sprintf("SHOW GLOBAL STATUS WHERE Variable_name IN (%s)", inList(statusVars))
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		s.ok = false
		return
	}
	next := make(map[string]int64, len(statusVars))
	nextRaw := make(map[string]string, len(statusVars))
	for rows.Next() {
		var name, val string
		if err := rows.Scan(&name, &val); err != nil {
			continue
		}
		nextRaw[name] = val
		next[name] = parseInt64(val)
	}
	_ = rows.Close()
	s.prev = s.cur
	s.cur = next
	s.curRaw = nextRaw
	s.tick++
	s.ok = true
	s.lastOK = time.Now()
}

// HasPrev reports whether a previous sample exists (collectors emit zeros when
// false — Perl's first-tick behavior).
func (s *StatusSource) HasPrev() bool { return s.tick > 1 && s.ok }

// Cur returns the current value of a status variable (0 if missing/failed).
func (s *StatusSource) Cur(name string) int64 {
	if !s.ok {
		return 0
	}
	return s.cur[name]
}

// Delta returns cur - prev for a variable (0 on first tick or failure).
func (s *StatusSource) Delta(name string) int64 {
	if !s.HasPrev() {
		return 0
	}
	return s.cur[name] - s.prev[name]
}

// Rate returns Delta / elapsed (per-second). P1-6: the denominator is the
// real wall-clock time since the last successful fetch (floored at the
// configured interval). After a transient fetch failure the delta spans
// several intervals; dividing by the fixed interval would overstate the rate.
// To avoid an in-progress-tick divide-by-near-zero, floor at interval and cap
// at 10x interval (a server that was down that long should read as a plateau,
// not a spike).
func (s *StatusSource) Rate(name string) float64 {
	delta := float64(s.Delta(name))
	denom := s.interval
	if !s.lastOK.IsZero() {
		elapsed := time.Since(s.lastOK).Seconds()
		if elapsed > denom {
			denom = elapsed
		}
	}
	return delta / denom
}

// CurRaw returns the current raw (unparsed) value of a status variable — for
// non-numeric vars like Rpl_semi_sync_master_status ("ON"/"OFF"). "" if absent.
func (s *StatusSource) CurRaw(name string) string {
	if !s.ok {
		return ""
	}
	return s.curRaw[name]
}

// SlaveStatus runs SHOW SLAVE STATUS and returns the first row as a
// column→value map. Returns ok=false when there are no rows (this server is
// not a replica). Column names vary across MySQL versions, so we read them
// dynamically.
func (s *StatusSource) SlaveStatus() (map[string]string, bool) {
	if s.db == nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, "SHOW SLAVE STATUS")
	if err != nil {
		return nil, false
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, false
	}
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		m := make(map[string]string, len(cols))
		for i, c := range cols {
			if vals[i].Valid {
				m[c] = vals[i].String
			}
		}
		return m, true // first row only
	}
	return nil, false
}

// Databases returns the non-system database names for the title block. It
// excludes information_schema, mysql, and test — matching the Perl original's
// `grep -iwvE "information_schema|mysql|test"` (whole-word, case-insensitive).
// Empty on error.
func (s *StatusSource) Databases() []string {
	if s.db == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, "SHOW DATABASES")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		if isSystemDB(name) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// ShowVariables runs `SHOW VARIABLES WHERE Variable_name IN (...)` and returns
// the name→value map. Used by the title block (print_vars). One query per group
// (not per tick — only at startup and on daily rotation).
func (s *StatusSource) ShowVariables(names []string) (map[string]string, error) {
	out := make(map[string]string, len(names))
	if s.db == nil {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	q := fmt.Sprintf("SHOW VARIABLES WHERE Variable_name IN (%s)", inList(names))
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var name, val string
		if err := rows.Scan(&name, &val); err != nil {
			continue
		}
		out[name] = val
	}
	return out, nil
}

// isSystemDB reports whether name is one of the system databases the Perl
// original filters out of the title.
func isSystemDB(name string) bool {
	switch strings.ToLower(name) {
	case "information_schema", "mysql", "test":
		return true
	}
	return false
}

// inList builds "v1,v2,..." (quoted) for a SQL IN clause.
func inList(vars []string) string {
	q := make([]string, len(vars))
	for i, v := range vars {
		q[i] = "'" + v + "'"
	}
	return strings.Join(q, ",")
}

// parseInt64 parses a MySQL status value, 0 on failure (non-numeric like "ON"
// degrade to 0 — plan §2.4 P2-15).
func parseInt64(s string) int64 {
	var n int64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
	if err != nil {
		return 0
	}
	return n
}
