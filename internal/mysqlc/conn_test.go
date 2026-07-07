package mysqlc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeCNF(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "my.cnf")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDSNTCP(t *testing.T) {
	c := &Config{User: "root", Password: "pw", Host: "127.0.0.1", Port: 3307, Timeout: time.Second}
	d := c.DSN()
	if !strings.HasPrefix(d, "root:pw@tcp(127.0.0.1:3307)/") {
		t.Errorf("TCP DSN = %q, want root:pw@tcp(127.0.0.1:3307)/...", d)
	}
	if !strings.Contains(d, "parseTime=true") || !strings.Contains(d, "timeout=1s") {
		t.Errorf("DSN missing params: %q", d)
	}
}

func TestDSNUnix(t *testing.T) {
	c := &Config{User: "root", Password: "pw", Socket: "/tmp/mysql.sock", Timeout: time.Second}
	d := c.DSN()
	if !strings.HasPrefix(d, "root:pw@unix(/tmp/mysql.sock)/") {
		t.Errorf("Unix DSN = %q, want root:pw@unix(/tmp/mysql.sock)/...", d)
	}
}

func TestDSNUnixPrecedence(t *testing.T) {
	// When both socket and host are set, socket wins (plan §8.4: socket preferred).
	c := &Config{User: "u", Password: "p", Host: "1.2.3.4", Port: 3306, Socket: "/tmp/x.sock", Timeout: time.Second}
	if !strings.Contains(c.DSN(), "unix(/tmp/x.sock)") {
		t.Errorf("socket should take precedence over host:port: %q", c.DSN())
	}
}

func TestSafeDSNMasksPassword(t *testing.T) {
	c := &Config{User: "root", Password: "bighead67", Host: "127.0.0.1", Port: 3306}
	if s := c.SafeDSN(); !strings.Contains(s, "***") || strings.Contains(s, "bighead67") {
		t.Errorf("SafeDSN leaked password: %q", s)
	}
}

// ---- credential priority (plan §8.1) ----

func TestResolveCLIOverridesCNF(t *testing.T) {
	cnf := writeCNF(t, "[client]\nuser=cnfuser\npassword=cnfpass\nhost=10.0.0.1\nport=3307\n")
	c := ResolveCredentials(ResolveOpts{
		CLIUser: "cliuser", DefaultsFile: cnf, DefaultsGroup: "client", Timeout: time.Second,
	})
	if c.User != "cliuser" {
		t.Errorf("User = %q, want cliuser (CLI wins)", c.User)
	}
	if c.Password != "cnfpass" {
		t.Errorf("Password = %q, want cnfpass (from my.cnf, CLI pass empty)", c.Password)
	}
	if c.Host != "10.0.0.1" || c.Port != 3307 {
		t.Errorf("Host/Port = %q/%d, want from my.cnf", c.Host, c.Port)
	}
}

func TestResolveEnvOverridesCNF(t *testing.T) {
	cnf := writeCNF(t, "[client]\nuser=cnfuser\npassword=cnfpass\n")
	t.Setenv("ORZDBA_MYSQL_USER", "envuser")
	t.Setenv("ORZDBA_MYSQL_PASS", "envpass")
	c := ResolveCredentials(ResolveOpts{DefaultsFile: cnf, DefaultsGroup: "client", Timeout: time.Second})
	if c.User != "envuser" || c.Password != "envpass" {
		t.Errorf("env should win: got %q/%q", c.User, c.Password)
	}
}

func TestResolveInjectLowest(t *testing.T) {
	// Explicit defaults-file with no [client] creds → no default search →
	// env (unset) → compile-time inject.
	cnf := writeCNF(t, "[mysqld]\ndatadir=/var/lib/mysql\n")
	t.Setenv("ORZDBA_MYSQL_USER", "")
	t.Setenv("ORZDBA_MYSQL_PASS", "")
	c := ResolveCredentials(ResolveOpts{
		DefaultsFile: cnf, DefaultsGroup: "client",
		InjectUser: "builtuser", InjectPass: "builtpass", Timeout: time.Second,
	})
	if c.User != "builtuser" || c.Password != "builtpass" {
		t.Errorf("inject should be used: got %q/%q", c.User, c.Password)
	}
}

func TestResolveDefaults(t *testing.T) {
	// No creds anywhere (explicit empty file, no env, no inject) → defaults.
	cnf := writeCNF(t, "[mysqld]\n")
	c := ResolveCredentials(ResolveOpts{DefaultsFile: cnf, DefaultsGroup: "client", Timeout: time.Second})
	if c.Host != "127.0.0.1" || c.Port != 3306 {
		t.Errorf("defaults Host/Port = %q/%d, want 127.0.0.1/3306", c.Host, c.Port)
	}
	if c.User != "" {
		t.Errorf("User should be empty (peer-auth fallback), got %q", c.User)
	}
}
