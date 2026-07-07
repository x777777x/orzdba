package mycol

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
)

// mockRows implements driver.Rows for canned query results. Returned values
// are []byte (mimicking the go-sql-driver/mysql text-column convention) so the
// database/sql convertAssign path exercised is the same as in production.
type mockRows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func newRows(cols []string, rows ...[]string) *mockRows {
	data := make([][]driver.Value, len(rows))
	for i, r := range rows {
		v := make([]driver.Value, len(r))
		for j, s := range r {
			v[j] = []byte(s) // text columns arrive as []byte from the mysql driver
		}
		data[i] = v
	}
	return &mockRows{cols: cols, data: data}
}

// nullRows is like newRows but lets a cell be NULL (nil) by index.
func newRowsWithNull(cols []string, rows ...[]interface{}) *mockRows {
	data := make([][]driver.Value, len(rows))
	for i, r := range rows {
		v := make([]driver.Value, len(r))
		for j, s := range r {
			if s == nil {
				v[j] = nil
			} else {
				v[j] = []byte(s.(string))
			}
		}
		data[i] = v
	}
	return &mockRows{cols: cols, data: data}
}

func (r *mockRows) Columns() []string { return r.cols }
func (r *mockRows) Close() error      { return nil }
func (r *mockRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}

// mockDriver returns canned rows (keyed by a query substring) or a canned
// error. It's a package singleton configured per-test (tests run sequentially).
type mockDriver struct {
	rowsByQuery map[string]driver.Rows
	errByQuery  map[string]error
}

var mockDrv = &mockDriver{rowsByQuery: map[string]driver.Rows{}, errByQuery: map[string]error{}}

func init() {
	sql.Register("mock", mockDrv)
}

func (d *mockDriver) Open(name string) (driver.Conn, error) { return &mockConn{d: d}, nil }

type mockConn struct{ d *mockDriver }

func (c *mockConn) Close() error              { return nil }
func (c *mockConn) Begin() (driver.Tx, error) { return nil, fmt.Errorf("tx not supported") }
func (c *mockConn) Prepare(q string) (driver.Stmt, error) {
	return nil, fmt.Errorf("prepare not supported")
}

// QueryContext satisfies driver.QueryerContext (the one-round-trip path
// sql.DB.QueryContext prefers). Query selection is by substring match.
func (c *mockConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	for sub, err := range c.d.errByQuery {
		if strings.Contains(q, sub) {
			return nil, err
		}
	}
	for sub, rows := range c.d.rowsByQuery {
		if strings.Contains(q, sub) {
			return rows, nil
		}
	}
	return &mockRows{}, nil // empty result set
}

// resetMock clears canned rows/errors between tests.
func resetMock() {
	mockDrv.rowsByQuery = map[string]driver.Rows{}
	mockDrv.errByQuery = map[string]error{}
}

// mockDB opens a *sql.DB backed by the mock driver.
func mockDB() *sql.DB {
	db, _ := sql.Open("mock", "")
	return db
}
