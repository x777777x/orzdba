// Package mysqlc wraps the MySQL connection and credential resolution.
//
// Credentials never appear in process argv: queries go through database/sql +
// the go-sql-driver/mysql native protocol, so the password lives only in the
// in-process DSN string (plan §9.3, fixing orzdba-go P0-1). Logged DSNs mask
// the password (SafeDSN).
package mysqlc

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// Config holds resolved MySQL connection parameters.
type Config struct {
	User     string
	Password string
	Host     string
	Port     int
	Socket   string
	Timeout  time.Duration
	TLS      bool
}

// ResolveOpts carries the inputs to credential resolution (plan §8.1
// priority: CLI > env > explicit defaults-file > default search > inject).
type ResolveOpts struct {
	// CLI explicit (highest priority).
	CLIUser, CLIPass string
	CLIHost          string
	CLIPort          int
	CLISocket        string
	// my.cnf.
	DefaultsFile  string // --mysql-defaults-file (explicit; skips default search)
	DefaultsGroup string // --mysql-defaults-group (default "client")
	// Compile-time injection (lowest priority).
	InjectUser, InjectPass string
	// Connection.
	Timeout time.Duration
	TLS     bool
}

// DefaultCNFSearch is the my.cnf search order when no explicit defaults-file is
// given (plan §8.1).
var DefaultCNFSearch = []string{"/etc/my.cnf", "/etc/mysql/my.cnf", expandHome("~/.my.cnf")}

// ResolveCredentials merges sources low→high so higher priority wins per
// field (plan §8.1). host/port/socket come from my.cnf or CLI; user/password
// from any source. Empty CLI fields don't override my.cnf.
func ResolveCredentials(o ResolveOpts) Config {
	c := Config{Timeout: o.Timeout, TLS: o.TLS}

	// 5. compile-time inject (lowest).
	c.User, c.Password = o.InjectUser, o.InjectPass

	// 4/3. my.cnf — explicit file, else default search (first existing).
	if cnf := loadCNF(o.DefaultsFile, o.DefaultsGroup); cnf != nil && cnf.Found {
		if cnf.User != "" {
			c.User = cnf.User
		}
		if cnf.Password != "" {
			c.Password = cnf.Password
		}
		if cnf.Host != "" {
			c.Host = cnf.Host
		}
		if cnf.Port != 0 {
			c.Port = cnf.Port
		}
		if cnf.Socket != "" {
			c.Socket = cnf.Socket
		}
	}

	// 2. env.
	if u := os.Getenv("ORZDBA_MYSQL_USER"); u != "" {
		c.User = u
	}
	if p := os.Getenv("ORZDBA_MYSQL_PASS"); p != "" {
		c.Password = p
	}

	// 1. CLI explicit (highest).
	if o.CLIUser != "" {
		c.User = o.CLIUser
	}
	if o.CLIPass != "" {
		c.Password = o.CLIPass
	}
	if o.CLIHost != "" {
		c.Host = o.CLIHost
	}
	if o.CLIPort != 0 {
		c.Port = o.CLIPort
	}
	if o.CLISocket != "" {
		c.Socket = o.CLISocket
	}

	// Defaults.
	if c.Host == "" && c.Socket == "" {
		c.Host = "127.0.0.1"
	}
	if c.Port == 0 {
		c.Port = 3306
	}
	if c.Timeout == 0 {
		c.Timeout = time.Second
	}
	return c
}

// loadCNF parses an explicit file, else searches DefaultCNFSearch for the
// first existing file and parses that. Returns nil if none parse.
func loadCNF(explicit, group string) *CNFSource {
	if explicit != "" {
		src, err := ParseMySQLDefaults(explicit, group)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: cannot parse %s: %v\n", explicit, err)
			return nil
		}
		if w := CheckFileMode(explicit); w != "" {
			fmt.Fprintln(os.Stderr, w)
		}
		return src
	}
	for _, p := range DefaultCNFSearch {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			continue
		}
		src, err := ParseMySQLDefaults(p, group)
		if err != nil {
			continue
		}
		if w := CheckFileMode(p); w != "" {
			fmt.Fprintln(os.Stderr, w)
		}
		return src
	}
	return nil
}

// DSN returns the go-sql-driver/mysql DSN. Socket takes precedence over
// host:port (plan §8.4). No default database — SHOW commands don't need one.
func (c *Config) DSN() string {
	args := []string{"timeout=" + c.Timeout.String(), "parseTime=true"}
	if c.TLS {
		args = append(args, "tls=true")
	}
	params := "?" + strings.Join(args, "&")
	if c.Socket != "" {
		return fmt.Sprintf("%s:%s@unix(%s)/%s", c.User, c.Password, c.Socket, params)
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", c.User, c.Password, c.Host, c.Port, params)
}

// SafeDSN returns a DSN with the password masked, for logging/stderr
// (plan §8.4: password never printed).
func (c *Config) SafeDSN() string {
	if c.Socket != "" {
		return fmt.Sprintf("%s:***@unix(%s)", c.User, c.Socket)
	}
	return fmt.Sprintf("%s:***@tcp(%s:%d)", c.User, c.Host, c.Port)
}

// Open validates the config and returns a long-lived single connection
// (plan §9.3: single connection, no pool). A 2s dial timeout gates startup.
func Open(c *Config) (*sql.DB, error) {
	db, err := sql.Open("mysql", c.DSN())
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// expandHome replaces a leading ~ with $HOME (for ~/.my.cnf in the search list).
func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}
