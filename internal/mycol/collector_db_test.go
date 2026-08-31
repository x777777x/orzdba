package mycol

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"orzdba/internal/metric"
)

// ---- StatusSource.Fetch: cur/prev shift + raw map + ok flag ----

func TestFetchShiftsCurPrev(t *testing.T) {
	resetMock()
	mockDrv.rowsByQuery["SHOW GLOBAL STATUS"] = newRows(
		[]string{"Variable_name", "Value"},
		[]string{"Com_select", "300"},
		[]string{"Threads_running", "5"},
	)
	s := NewStatusSource(mockDB(), 1, time.Second)

	s.Fetch() // tick 1
	if s.HasPrev() {
		t.Error("after 1 fetch, HasPrev should be false (first tick)")
	}
	if got := s.Cur("Com_select"); got != 300 {
		t.Errorf("Cur(Com_select) = %d, want 300", got)
	}
	if got := s.CurRaw("Com_select"); got != "300" {
		t.Errorf("CurRaw(Com_select) = %q, want \"300\"", got)
	}

	// Second tick: new values.
	mockDrv.rowsByQuery["SHOW GLOBAL STATUS"] = newRows(
		[]string{"Variable_name", "Value"},
		[]string{"Com_select", "500"},
		[]string{"Threads_running", "7"},
	)
	s.Fetch() // tick 2
	if !s.HasPrev() {
		t.Error("after 2 fetches, HasPrev should be true")
	}
	if got := s.Delta("Com_select"); got != 200 {
		t.Errorf("Delta(Com_select) = %d, want 200 (500-300)", got)
	}
	if got := s.Rate("Com_select"); got != 200 {
		t.Errorf("Rate(Com_select) = %v, want 200 (interval=1)", got)
	}
	if got := s.Cur("Threads_running"); got != 7 {
		t.Errorf("Cur(Threads_running) = %d, want 7", got)
	}
}

func TestFetchErrorDegrades(t *testing.T) {
	resetMock()
	mockDrv.errByQuery["SHOW GLOBAL STATUS"] = errors.New("connection lost")
	s := NewStatusSource(mockDB(), 1, time.Second)
	// Prime a previous sample so the failure is observable as a degradation.
	mockDrv.errByQuery = nil
	mockDrv.rowsByQuery["SHOW GLOBAL STATUS"] = newRows(
		[]string{"Variable_name", "Value"}, []string{"Com_select", "100"})
	s.Fetch()

	// Now make the query fail.
	resetMock()
	mockDrv.errByQuery["SHOW GLOBAL STATUS"] = errors.New("connection lost")
	s.Fetch()
	if s.HasPrev() {
		t.Error("after a failed fetch, HasPrev should be false (degraded)")
	}
	if got := s.Cur("Com_select"); got != 0 {
		t.Errorf("Cur after failed fetch = %d, want 0", got)
	}
	if got := s.Delta("Com_select"); got != 0 {
		t.Errorf("Delta after failed fetch = %d, want 0", got)
	}
}

// ---- SlaveStatus: column scanning + NULL + no-rows + error ----

func TestSlaveStatusReplica(t *testing.T) {
	resetMock()
	mockDrv.rowsByQuery["SHOW SLAVE STATUS"] = newRows(
		[]string{"Master_Host", "Read_Master_Log_Pos", "Exec_Master_Log_Pos", "Seconds_Behind_Master"},
		[]string{"10.0.0.2", "1000", "800", "10"},
	)
	s := NewStatusSource(mockDB(), 1, time.Second)
	m, ok := s.SlaveStatus()
	if !ok {
		t.Fatal("replica row present, ok should be true")
	}
	if m["Read_Master_Log_Pos"] != "1000" || m["Exec_Master_Log_Pos"] != "800" || m["Seconds_Behind_Master"] != "10" {
		t.Errorf("slave map mis-scanned: %+v", m)
	}
}

func TestSlaveStatusNullColumn(t *testing.T) {
	resetMock()
	// Seconds_Behind_Master is NULL when replication is stopped — the driver
	// returns nil; the scan into NullString must mark it invalid (not crash).
	mockDrv.rowsByQuery["SHOW SLAVE STATUS"] = newRowsWithNull(
		[]string{"Read_Master_Log_Pos", "Exec_Master_Log_Pos", "Seconds_Behind_Master"},
		[]interface{}{"100", "100", nil},
	)
	s := NewStatusSource(mockDB(), 1, time.Second)
	m, ok := s.SlaveStatus()
	if !ok {
		t.Fatal("replica row present, ok should be true")
	}
	if _, present := m["Seconds_Behind_Master"]; present {
		t.Errorf("NULL column should be absent from map, got %q", m["Seconds_Behind_Master"])
	}
	if m["Read_Master_Log_Pos"] != "100" {
		t.Errorf("non-NULL column mis-scanned: %+v", m)
	}
}

func TestSlaveStatusNoRows(t *testing.T) {
	resetMock()
	mockDrv.rowsByQuery["SHOW SLAVE STATUS"] = newRows([]string{"Master_Host"}) // empty
	s := NewStatusSource(mockDB(), 1, time.Second)
	if _, ok := s.SlaveStatus(); ok {
		t.Error("non-replica (no rows) should return ok=false")
	}
}

func TestSlaveStatusError(t *testing.T) {
	resetMock()
	mockDrv.errByQuery["SHOW SLAVE STATUS"] = errors.New("access denied")
	s := NewStatusSource(mockDB(), 1, time.Second)
	if _, ok := s.SlaveStatus(); ok {
		t.Error("query error should return ok=false")
	}
}

// ---- Databases: system-DB filtering ----

func TestDatabasesFiltering(t *testing.T) {
	resetMock()
	mockDrv.rowsByQuery["SHOW DATABASES"] = newRows([]string{"Database"},
		[]string{"information_schema"}, []string{"mysql"}, []string{"performance_schema"},
		[]string{"sbtest"}, []string{"sys"}, []string{"test"},
	)
	s := NewStatusSource(mockDB(), 1, time.Second)
	got := s.Databases()
	// Excludes information_schema, mysql, test (Perl grep -iwvE filter).
	want := []string{"performance_schema", "sbtest", "sys"}
	if len(got) != len(want) {
		t.Fatalf("Databases = %v, want %v", got, want)
	}
	for i, d := range want {
		if got[i] != d {
			t.Errorf("Databases[%d] = %q, want %q", i, got[i], d)
		}
	}
}

// ---- ShowVariables ----

func TestShowVariables(t *testing.T) {
	resetMock()
	mockDrv.rowsByQuery["SHOW VARIABLES"] = newRows(
		[]string{"Variable_name", "Value"},
		[]string{"max_connections", "151"},
		[]string{"innodb_buffer_pool_size", "134217728"},
	)
	s := NewStatusSource(mockDB(), 1, time.Second)
	vals, err := s.ShowVariables([]string{"max_connections", "innodb_buffer_pool_size"})
	if err != nil {
		t.Fatal(err)
	}
	if vals["max_connections"] != "151" {
		t.Errorf("max_connections = %q, want 151", vals["max_connections"])
	}
	if vals["innodb_buffer_pool_size"] != "134217728" {
		t.Errorf("innodb_buffer_pool_size = %q", vals["innodb_buffer_pool_size"])
	}
	// A variable absent from the result is just missing from the map.
	if _, ok := vals["binlog_format"]; ok {
		t.Error("absent variable should not be in the map")
	}
}

// ---- InnodbStatus: parse from the query result ----

func TestInnodbStatusFromQuery(t *testing.T) {
	resetMock()
	sample := `History list length 23
Log sequence number 6712509083974
Log flushed up to   6712508972870
Last checkpoint at  6709615343735
2 queries inside InnoDB, 0 queries in queue
2 read views open inside InnoDB
`
	mockDrv.rowsByQuery["SHOW ENGINE INNODB STATUS"] = newRows(
		[]string{"Type", "Name", "Status"},
		[]string{"InnoDB", "Status", sample},
	)
	s := NewStatusSource(mockDB(), 1, time.Second)
	v := s.InnodbStatus()
	if !v.OK {
		t.Fatal("OK=false, want true")
	}
	if v.HistoryList != 23 {
		t.Errorf("HistoryList = %d, want 23", v.HistoryList)
	}
	if v.UnflushedLog != 111104 {
		t.Errorf("UnflushedLog = %d, want 111104", v.UnflushedLog)
	}
	if v.UncheckpointedBytes != 2893740239 {
		t.Errorf("UncheckpointedBytes = %d, want 2893740239", v.UncheckpointedBytes)
	}
	if v.ReadViews != 2 || v.QueriesInside != 2 || v.QueriesQueued != 0 {
		t.Errorf("views/inside/queued = %d/%d/%d, want 2/2/0", v.ReadViews, v.QueriesInside, v.QueriesQueued)
	}
}

func TestInnodbStatusCollectDegraded(t *testing.T) {
	// HasPrev=true (second tick) but the ENGINE INNODB STATUS query fails → the
	// collector must degrade to zeros, not crash or emit stale values.
	resetMock()
	mockDrv.errByQuery["SHOW ENGINE INNODB STATUS"] = errors.New("PROCESS privilege required")
	s := NewStatusSource(mockDB(), 1, time.Second)
	s.tick = 2
	s.ok = true // HasPrev() true
	c := NewInnodbStatus(s, metric.UnitRaw)
	cells := c.Collect()
	want := fmt.Sprintf("%5d %6d %6d %5d %5d %5d", 0, 0, 0, 0, 0, 0)
	if cells[0].Text != want {
		t.Errorf("degraded innodb_status = %q, want %q", cells[0].Text, want)
	}
}

func TestInnodbStatusFirstTickZeros(t *testing.T) {
	s := NewStatusSource(mockDB(), 1, time.Second) // tick 0
	cells := NewInnodbStatus(s, metric.UnitRaw).Collect()
	if cells[0].Text != fmt.Sprintf("%5d %6d %6d %5d %5d %5d", 0, 0, 0, 0, 0, 0) {
		t.Errorf("first-tick innodb_status = %q, want zeros", cells[0].Text)
	}
}

// ---- first-tick zeros for innodb_data / innodb_log (audit P1) ----

func TestInnodbDataFirstTickZeros(t *testing.T) {
	s := NewStatusSource(mockDB(), 1, time.Second) // tick 0, HasPrev false
	cells := NewInnodbData(s, metric.UnitRaw).Collect()
	if cells[0].Text != fmt.Sprintf("%6d %6d %6d %6d", 0, 0, 0, 0) {
		t.Errorf("first-tick innodb_data = %q, want zeros", cells[0].Text)
	}
}

func TestInnodbLogFirstTickZeros(t *testing.T) {
	s := NewStatusSource(mockDB(), 1, time.Second)
	cells := NewInnodbLog(s, metric.UnitRaw).Collect()
	if cells[0].Text != fmt.Sprintf("%6d %7d", 0, 0) {
		t.Errorf("first-tick innodb_log = %q, want zeros", cells[0].Text)
	}
}

// ---- pure helpers ----

func TestParseInt64(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"123", 123},
		{"0", 0},
		{"  456  ", 456}, // surrounding whitespace
		{"ON", 0},        // non-numeric status value → 0 (Perl int() semantics)
		{"", 0},          // empty → 0
		{"9223372036854775807", 9223372036854775807}, // int64 max
		{"18446744073709551615", 0},                  // overflow (uint64 max) → 0
		{"-5", -5},
	}
	for _, c := range cases {
		if got := parseInt64(c.in); got != c.want {
			t.Errorf("parseInt64(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestIsSystemDB(t *testing.T) {
	system := []string{"information_schema", "mysql", "test", "MySQL", "TEST"}
	for _, d := range system {
		if !isSystemDB(d) {
			t.Errorf("isSystemDB(%q) = false, want true", d)
		}
	}
	nonSystem := []string{"sbtest", "performance_schema", "sys", "mydb"}
	for _, d := range nonSystem {
		if isSystemDB(d) {
			t.Errorf("isSystemDB(%q) = true, want false", d)
		}
	}
}

func TestInList(t *testing.T) {
	got := inList([]string{"Com_select", "Com_insert"})
	if got != "'Com_select','Com_insert'" {
		t.Errorf("inList = %q, want quoted comma-joined", got)
	}
	if inList([]string{}) != "" {
		t.Error("inList(empty) should be empty")
	}
	// Sanity: the statusVars list produces a non-empty IN clause.
	if s := inList(statusVars); !strings.HasPrefix(s, "'") || !strings.Contains(s, "'Com_select'") {
		t.Errorf("inList(statusVars) malformed: %q", s)
	}
}
