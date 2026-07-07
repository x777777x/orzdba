package mycol

import (
	"fmt"
	"testing"
	"time"

	"orzdba/internal/metric"
)

// newTestSource builds a StatusSource with injected cur/prev maps so collectors
// can be tested without a database. tick=2 makes HasPrev() true (second tick).
func newTestSource(cur, prev map[string]int64, raw map[string]string) *StatusSource {
	s := NewStatusSource(nil, 1, time.Second)
	s.cur = cur
	s.prev = prev
	s.curRaw = raw
	s.tick = 2
	s.ok = true
	return s
}

func TestStatusSourceRateDeltaCur(t *testing.T) {
	cur := map[string]int64{"Com_select": 300, "Threads_running": 5}
	prev := map[string]int64{"Com_select": 100, "Threads_running": 2}
	s := newTestSource(cur, prev, nil)
	// interval=1 → Rate == Delta
	if d := s.Delta("Com_select"); d != 200 {
		t.Errorf("Delta = %d, want 200", d)
	}
	if r := s.Rate("Com_select"); r != 200 {
		t.Errorf("Rate = %v, want 200", r)
	}
	if c := s.Cur("Threads_running"); c != 5 {
		t.Errorf("Cur = %d, want 5", c)
	}
	// missing var → 0
	if s.Cur("nope") != 0 || s.Delta("nope") != 0 {
		t.Error("missing var should read 0")
	}
}

func TestStatusSourceHasPrev(t *testing.T) {
	s := NewStatusSource(nil, 1, time.Second)
	// tick 0 → no prev
	if s.HasPrev() {
		t.Error("fresh source should have no prev")
	}
	s.tick = 2
	s.ok = true
	if !s.HasPrev() {
		t.Error("tick=2 ok=true should have prev")
	}
	// a failed fetch (ok=false) should report no prev even with tick>1
	s.ok = false
	if s.HasPrev() {
		t.Error("failed fetch should report no prev")
	}
}

// ---- com (QPS/TPS) ----

func TestComCollect(t *testing.T) {
	cur := map[string]int64{"Com_insert": 40, "Com_update": 15, "Com_delete": 5, "Com_select": 300}
	prev := map[string]int64{"Com_insert": 10, "Com_update": 5, "Com_delete": 0, "Com_select": 100}
	c := NewCom(newTestSource(cur, prev, nil))
	cells := c.Collect()
	// ins=30 upd=10 del=5 sel=200 tps=45
	want := fmt.Sprintf("%5d %5d %5d", 30, 10, 5)
	if cells[0].Text != want || cells[0].Color != metric.White {
		t.Errorf("ins/upd/del = %q/%v, want %q/White", cells[0].Text, cells[0].Color, want)
	}
	if cells[1].Text != "    200" || cells[1].Color != metric.Yellow {
		t.Errorf("sel = %q/%v, want \"    200\"/Yellow", cells[1].Text, cells[1].Color)
	}
	if cells[2].Text != "    45" || cells[2].Color != metric.Yellow {
		t.Errorf("tps = %q/%v, want \"    45\"/Yellow", cells[2].Text, cells[2].Color)
	}
}

func TestComFirstTickZeros(t *testing.T) {
	s := NewStatusSource(nil, 1, time.Second) // tick 0, no prev
	cells := NewCom(s).Collect()
	if cells[0].Text != "    0     0     0" {
		t.Errorf("first-tick com = %q, want zeros", cells[0].Text)
	}
}

// ---- hit (1-column) ----

func TestHitOneColumn(t *testing.T) {
	cur := map[string]int64{"Innodb_buffer_pool_read_requests": 150000, "Innodb_buffer_pool_reads": 5200}
	prev := map[string]int64{"Innodb_buffer_pool_read_requests": 100000, "Innodb_buffer_pool_reads": 5000}
	cells := NewHit(newTestSource(cur, prev, nil), false).Collect()
	// rr=50000, rd=200, hit=(50000-200)/50000*100 = 99.6 → Green
	if cells[0].Text != "   50000" {
		t.Errorf("lor = %q, want \"   50000\"", cells[0].Text)
	}
	if cells[1].Text != "  99.60" {
		t.Errorf("hit = %q, want \"  99.60\"", cells[1].Text)
	}
	if cells[1].Color != metric.Green {
		t.Errorf("hit color = %v, want Green (99.6>99)", cells[1].Color)
	}
}

func TestHitOneColumnLowHit(t *testing.T) {
	// rr=1000, rd=500 → hit=50% → Red
	cur := map[string]int64{"Innodb_buffer_pool_read_requests": 2000, "Innodb_buffer_pool_reads": 1000}
	prev := map[string]int64{"Innodb_buffer_pool_read_requests": 1000, "Innodb_buffer_pool_reads": 500}
	cells := NewHit(newTestSource(cur, prev, nil), false).Collect()
	if cells[1].Color != metric.Red {
		t.Errorf("hit=50%% color = %v, want Red", cells[1].Color)
	}
}

func TestHitOneColumnNoReads(t *testing.T) {
	// rr delta = 0 → hit 100.00 (Perl: avoids divide-by-zero)
	cur := map[string]int64{"Innodb_buffer_pool_read_requests": 100, "Innodb_buffer_pool_reads": 0}
	prev := map[string]int64{"Innodb_buffer_pool_read_requests": 100, "Innodb_buffer_pool_reads": 0}
	cells := NewHit(newTestSource(cur, prev, nil), false).Collect()
	if cells[1].Text != " 100.00" || cells[1].Color != metric.Green {
		t.Errorf("no-reads hit = %q/%v, want \" 100.00\"/Green", cells[1].Text, cells[1].Color)
	}
}

// ---- hit full (5-column) ----

func TestHitFullColumns(t *testing.T) {
	// Construct cur/prev so every hit% is a clean value.
	cur := map[string]int64{
		"Key_read_requests": 100, "Key_reads": 5, "Key_write_requests": 100, "Key_writes": 10,
		"Handler_read_first": 10, "Handler_read_key": 100, "Handler_read_next": 200, "Handler_read_prev": 5,
		"Handler_read_rnd": 5, "Handler_read_rnd_next": 20,
		"Qcache_hits": 80, "Com_select": 100,
		"Innodb_buffer_pool_read_requests": 200, "Innodb_buffer_pool_reads": 2,
	}
	prev := map[string]int64{
		"Key_read_requests": 0, "Key_reads": 0, "Key_write_requests": 0, "Key_writes": 0,
		"Handler_read_first": 0, "Handler_read_key": 0, "Handler_read_next": 0, "Handler_read_prev": 0,
		"Handler_read_rnd": 0, "Handler_read_rnd_next": 0,
		"Qcache_hits": 0, "Com_select": 0,
		"Innodb_buffer_pool_read_requests": 0, "Innodb_buffer_pool_reads": 0,
	}
	cells := NewHit(newTestSource(cur, prev, nil), true).Collect()
	if len(cells) != 7 {
		t.Fatalf("full hit produced %d cells, want 7", len(cells))
	}
	// keyReadHit = 1-5/100 = 0.95 → 95.00 Red
	if cells[0].Text != " 95.00" || cells[0].Color != metric.Red {
		t.Errorf("keyRead = %q/%v, want \" 95.00\"/Red", cells[0].Text, cells[0].Color)
	}
	// qcache = 80/(80+100) = 0.4444 → 44.44 (cell uses %7.2f)
	if cells[4].Text != "  44.44" || cells[4].Color != metric.Red {
		t.Errorf("qcache = %q/%v, want \"  44.44\"/Red", cells[4].Text, cells[4].Color)
	}
	// innodb = 1-2/200 = 0.99 → 99.00 Green (>99? no — 99.00 not >99 → Red!)
	//   hitColor(99.00): 99.00 > 99 is false → Red.
	if cells[6].Color != metric.Red {
		t.Errorf("innodb hit=99.00 color = %v, want Red (threshold is strictly >99)", cells[6].Color)
	}
}

// ---- threads + thread cache hit ----

func TestThreadCacheHit(t *testing.T) {
	// Connections delta 100, Threads_created delta 1 → (1-1/100)*100 = 99.0
	cur := map[string]int64{"Connections": 1100, "Threads_created": 4}
	prev := map[string]int64{"Connections": 1000, "Threads_created": 3}
	s := newTestSource(cur, prev, nil)
	if got := threadCacheHit(s); got != 99.0 {
		t.Errorf("threadCacheHit = %v, want 99.0", got)
	}
	// No new connections → 100 (perfect cache)
	cur2 := map[string]int64{"Connections": 1000, "Threads_created": 4}
	prev2 := map[string]int64{"Connections": 1000, "Threads_created": 3}
	if got := threadCacheHit(newTestSource(cur2, prev2, nil)); got != 100 {
		t.Errorf("threadCacheHit (no new conns) = %v, want 100", got)
	}
}

func TestThreadsCollect(t *testing.T) {
	cur := map[string]int64{"Threads_running": 3, "Threads_connected": 6, "Threads_created": 4, "Threads_cached": 3, "Connections": 1100}
	prev := map[string]int64{"Threads_created": 3, "Connections": 1000}
	cells := NewThreads(newTestSource(cur, prev, nil)).Collect()
	// run=3 con=6 cre=1 cac=3 hit=99.0 → Yellow (>90, not >99)
	want := fmt.Sprintf("%4d %4d %4d %4d %6.2f", 3, 6, 1, 3, 99.0)
	if cells[0].Text != want {
		t.Errorf("threads = %q, want %q", cells[0].Text, want)
	}
	if cells[0].Color != metric.Yellow {
		t.Errorf("threads color = %v, want Yellow (hit 99.0)", cells[0].Color)
	}
}

// ---- innodb_rows / pages / data / log ----

func TestInnodbRows(t *testing.T) {
	cur := map[string]int64{"Innodb_rows_inserted": 200, "Innodb_rows_updated": 50, "Innodb_rows_deleted": 10, "Innodb_rows_read": 5000}
	prev := map[string]int64{"Innodb_rows_inserted": 100, "Innodb_rows_updated": 40, "Innodb_rows_deleted": 5, "Innodb_rows_read": 4000}
	cells := NewInnodbRows(newTestSource(cur, prev, nil)).Collect()
	want := fmt.Sprintf("%5d %5d %5d %6d", 100, 10, 5, 1000)
	if cells[0].Text != want {
		t.Errorf("innodb_rows = %q, want %q", cells[0].Text, want)
	}
}

func TestInnodbPages(t *testing.T) {
	cur := map[string]int64{"Innodb_buffer_pool_pages_data": 4096, "Innodb_buffer_pool_pages_free": 1024, "Innodb_buffer_pool_pages_dirty": 100, "Innodb_buffer_pool_pages_flushed": 60}
	prev := map[string]int64{"Innodb_buffer_pool_pages_flushed": 50}
	cells := NewInnodbPages(newTestSource(cur, prev, nil)).Collect()
	// data/free WHITE, dirty/flushed YELLOW (flushed delta=10)
	if cells[0].Text != "   4096   1024 " || cells[0].Color != metric.White {
		t.Errorf("pages data/free = %q/%v", cells[0].Text, cells[0].Color)
	}
	if cells[1].Text != "   100    10" || cells[1].Color != metric.Yellow {
		t.Errorf("pages dirty/flushed = %q/%v", cells[1].Text, cells[1].Color)
	}
}

func TestInnodbData(t *testing.T) {
	cur := map[string]int64{"Innodb_data_reads": 300, "Innodb_data_writes": 400, "Innodb_data_read": 5 << 20, "Innodb_data_written": 1 << 20}
	prev := map[string]int64{"Innodb_data_reads": 200, "Innodb_data_writes": 300, "Innodb_data_read": 0, "Innodb_data_written": 0}
	cells := NewInnodbData(newTestSource(cur, prev, nil)).Collect()
	// reads=100 writes=100; read delta=5MiB → "5.0m"? /1024/1024=5 >1 → "%5.1fm" of 5 = "  5.0m"
	if cells[0].Text != "   100    100 " || cells[0].Color != metric.White {
		t.Errorf("data reads/writes = %q/%v", cells[0].Text, cells[0].Color)
	}
	// read 5MiB → >9? no (5 not >9) → White. format >1MiB → "  5.0m"
	if cells[1].Text != "  5.0m" || cells[1].Color != metric.White {
		t.Errorf("data read = %q/%v, want \"  5.0m\"/White", cells[1].Text, cells[1].Color)
	}
}

func TestInnodbLog(t *testing.T) {
	cur := map[string]int64{"Innodb_os_log_fsyncs": 30, "Innodb_os_log_written": 2 << 20}
	prev := map[string]int64{"Innodb_os_log_fsyncs": 20, "Innodb_os_log_written": 0}
	cells := NewInnodbLog(newTestSource(cur, prev, nil)).Collect()
	// fsyncs=10; written 2MiB → >1 → "%6.1fm" of 2 = "   2.0m" (7 wide), Red (>1)
	if cells[0].Text != "    10 " || cells[0].Color != metric.White {
		t.Errorf("log fsyncs = %q/%v", cells[0].Text, cells[0].Color)
	}
	if cells[1].Text != "   2.0m" || cells[1].Color != metric.Red {
		t.Errorf("log written = %q/%v, want \"   2.0m\"/Red", cells[1].Text, cells[1].Color)
	}
}

// ---- innodb_status parse ----

func TestParseInnodbStatus(t *testing.T) {
	sample := `=====================================
100212 TRANSACTION HISTORY
=====================================
Trx id counter 64AFBCC1B
Purge done for trx's n:o < 64AFBCAD4 undo n:o < 0 state: running but idle
History list length 23

------------
TRANSACTIONS
------------
Trx id counter 64AFBCC1B
---
LOG
---
Log sequence number 6712509083974
Log flushed up to   6712508972870
0 pending log writes, 0 pending chkp writes
Last checkpoint at  6709615343735
------------
ROW OPERATIONS
------------
2 queries inside InnoDB, 0 queries in queue
2 read views open inside InnoDB
`
	r := parseInnodbStatus(sample)
	if r.historyList != 23 {
		t.Errorf("historyList = %d, want 23", r.historyList)
	}
	if r.logWritten != 6712509083974 {
		t.Errorf("logWritten = %d, want 6712509083974", r.logWritten)
	}
	if r.logFlushed != 6712508972870 {
		t.Errorf("logFlushed = %d, want 6712508972870", r.logFlushed)
	}
	if r.lastCheckpoint != 6709615343735 {
		t.Errorf("lastCheckpoint = %d, want 6709615343735", r.lastCheckpoint)
	}
	if r.queriesInside != 2 || r.queriesQueued != 0 {
		t.Errorf("queries = %d/%d, want 2/0", r.queriesInside, r.queriesQueued)
	}
	if r.readViews != 2 {
		t.Errorf("readViews = %d, want 2", r.readViews)
	}
	// derived: unflushed = 111104, uncheckpointed = 2893510239
	v := InnodbStatusValues{}
	v.HistoryList = r.historyList
	v.UnflushedLog = r.logWritten - r.logFlushed
	v.UncheckpointedBytes = r.logWritten - r.lastCheckpoint
	if v.UnflushedLog != 111104 {
		t.Errorf("unflushed = %d, want 111104", v.UnflushedLog)
	}
	if v.UncheckpointedBytes != 2893740239 {
		t.Errorf("uncheckpointed = %d, want 2893740239", v.UncheckpointedBytes)
	}
}

func TestParseInnodbStatusMissingFields(t *testing.T) {
	// A minimal/empty status → all fields 0, no panic.
	r := parseInnodbStatus("no relevant lines here\n")
	if r.historyList != 0 || r.logWritten != 0 {
		t.Errorf("missing fields should parse as 0, got %+v", r)
	}
}

// ---- slave ----

func TestFormatSlaveReplica(t *testing.T) {
	m := map[string]string{
		"Read_Master_Log_Pos":   "1000",
		"Exec_Master_Log_Pos":   "800",
		"Seconds_Behind_Master": "10",
	}
	cells := formatSlave(m)
	// chk = 200, secBM = 10 (≤300 → Green)
	want := fmt.Sprintf("%11d%12d%8d", 1000, 800, 200)
	if cells[0].Text != want || cells[0].Color != metric.White {
		t.Errorf("slave read/exec/chk = %q/%v, want %q/White", cells[0].Text, cells[0].Color, want)
	}
	if cells[1].Text != "      10" || cells[1].Color != metric.Green {
		t.Errorf("slave SecBM = %q/%v, want \"      10\"/Green", cells[1].Text, cells[1].Color)
	}
}

func TestFormatSlaveLagRed(t *testing.T) {
	m := map[string]string{"Read_Master_Log_Pos": "100", "Exec_Master_Log_Pos": "0", "Seconds_Behind_Master": "400"}
	cells := formatSlave(m)
	if cells[1].Color != metric.Red {
		t.Errorf("SecBM=400 color = %v, want Red (>300)", cells[1].Color)
	}
}

func TestFormatSlaveNullLag(t *testing.T) {
	// Seconds_Behind_Master is NULL when replication is stopped/unknown.
	m := map[string]string{"Read_Master_Log_Pos": "100", "Exec_Master_Log_Pos": "100", "Seconds_Behind_Master": "NULL"}
	cells := formatSlave(m)
	if cells[1].Text != "       0" || cells[1].Color != metric.Green {
		t.Errorf("NULL SecBM = %q/%v, want \"       0\"/Green", cells[1].Text, cells[1].Color)
	}
}

// ---- semi ----

func TestSemiOn(t *testing.T) {
	cur := map[string]int64{
		"Rpl_semi_sync_master_yes_tx": 100, "Rpl_semi_sync_master_no_tx": 0,
		"Rpl_semi_sync_master_no_timeouts": 0,
	}
	raw := map[string]string{"Rpl_semi_sync_master_status": "ON"}
	cells := NewSemi(newTestSource(cur, nil, raw)).Collect()
	// on=1, yesTx=100, noTx=0, noTimes=0 → Green
	if cells[0].Text != "     1     100      0        0" || cells[0].Color != metric.Green {
		t.Errorf("semi ON = %q/%v, want Green", cells[0].Text, cells[0].Color)
	}
}

func TestSemiOffWithNoTx(t *testing.T) {
	cur := map[string]int64{"Rpl_semi_sync_master_yes_tx": 80, "Rpl_semi_sync_master_no_tx": 5, "Rpl_semi_sync_master_no_timeouts": 1}
	raw := map[string]string{"Rpl_semi_sync_master_status": "OFF"}
	cells := NewSemi(newTestSource(cur, nil, raw)).Collect()
	if cells[0].Color != metric.Red {
		t.Errorf("semi with no_tx>0 color = %v, want Red", cells[0].Color)
	}
	// on=0 (status OFF)
	if cells[0].Text[:6] != "     0" {
		t.Errorf("semi OFF on-bit = %q, want \"     0\"", cells[0].Text[:6])
	}
}

func TestSemiNotLoaded(t *testing.T) {
	// Plugin not installed → all vars absent → 0/""
	cells := NewSemi(newTestSource(map[string]int64{}, nil, map[string]string{})).Collect()
	if cells[0].Text != "     0       0      0        0" || cells[0].Color != metric.Green {
		t.Errorf("semi not-loaded = %q/%v, want zeros/Green", cells[0].Text, cells[0].Color)
	}
}

// ---- pct + hitColor helpers ----

func TestPct(t *testing.T) {
	cases := []struct {
		ratio, want float64
	}{
		{0.99, 99}, // normal
		{0.5, 50},  // normal
		{1.0, 100}, // exactly 1 → 100
		{1.5, 100}, // >1 clamped to 100
		{-0.1, 0},  // negative clamped to 0
	}
	for _, c := range cases {
		if got := pct(c.ratio); got != c.want {
			t.Errorf("pct(%v) = %v, want %v", c.ratio, got, c.want)
		}
	}
	// NaN (0/0) → 100
	if got := pct(zeroOverZero()); got != 100 {
		t.Errorf("pct(NaN) = %v, want 100", got)
	}
}

func zeroOverZero() float64 {
	var z float64
	return z / z // runtime 0/0 → NaN (constant 0/0 is a compile error)
}

func TestHitColor(t *testing.T) {
	if hitColor(99.01) != metric.Green {
		t.Error("99.01 should be Green")
	}
	if hitColor(99.0) != metric.Red {
		t.Error("99.0 should be Red (threshold strictly >99)")
	}
	if hitColor(50) != metric.Red {
		t.Error("50 should be Red")
	}
}
