package mysqlc

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCNFContent(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "my.cnf")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestLoadCNFMergesMultipleFiles verifies the cumulative default-search merge:
// credentials spread across /etc/my.cnf and ~/.my.cnf must combine (matching
// the mysql client). Previously loadCNF returned only the first existing file,
// silently dropping ~/.my.cnf when /etc/my.cnf lacked a [client] group.
func TestLoadCNFMergesMultipleFiles(t *testing.T) {
	etc := writeCNFContent(t, "[client]\nhost=10.0.0.1\nport=3307\n")
	home := writeCNFContent(t, "[client]\nuser=root\npassword=secret\n")
	old := DefaultCNFSearch
	DefaultCNFSearch = []string{etc, home}
	defer func() { DefaultCNFSearch = old }()

	src := loadCNF("", "client")
	if src == nil {
		t.Fatal("loadCNF returned nil, want merged source")
	}
	if src.Host != "10.0.0.1" || src.Port != 3307 {
		t.Errorf("host/port from first file lost: %q/%d", src.Host, src.Port)
	}
	if src.User != "root" || src.Password != "secret" {
		t.Errorf("user/pass from second file lost: %q/%q", src.User, src.Password)
	}
}

// TestLoadCNFSkipsFileWithoutClientSection: the real-world case on this dev
// machine — /etc/my.cnf has only [mysqld] (no [client]), ~/.my.cnf has [client]
// with creds. The merge must still pick up ~/.my.cnf.
func TestLoadCNFSkipsFileWithoutClientSection(t *testing.T) {
	etc := writeCNFContent(t, "[mysqld]\ndatadir=/var/lib/mysql\n")
	home := writeCNFContent(t, "[client]\nuser=root\npassword=secret\n")
	old := DefaultCNFSearch
	DefaultCNFSearch = []string{etc, home}
	defer func() { DefaultCNFSearch = old }()

	src := loadCNF("", "client")
	if src == nil {
		t.Fatal("loadCNF returned nil; /etc/my.cnf without [client] must not mask ~/.my.cnf")
	}
	if src.User != "root" {
		t.Errorf("User = %q, want root (from ~/.my.cnf [client])", src.User)
	}
}

func TestLoadCNFLastWins(t *testing.T) {
	first := writeCNFContent(t, "[client]\nuser=first\n")
	second := writeCNFContent(t, "[client]\nuser=second\n")
	old := DefaultCNFSearch
	DefaultCNFSearch = []string{first, second}
	defer func() { DefaultCNFSearch = old }()

	src := loadCNF("", "client")
	if src.User != "second" {
		t.Errorf("User = %q, want second (last-wins, matching mysql client)", src.User)
	}
}

func TestLoadCNFNoFiles(t *testing.T) {
	old := DefaultCNFSearch
	DefaultCNFSearch = []string{"/nonexistent/a.cnf", "/nonexistent/b.cnf"}
	defer func() { DefaultCNFSearch = old }()

	if src := loadCNF("", "client"); src != nil {
		t.Errorf("loadCNF with no existing files = %+v, want nil", src)
	}
}

func TestLoadCNFExplicitFile(t *testing.T) {
	// An explicit --mysql-defaults-file bypasses the default search entirely.
	etc := writeCNFContent(t, "[mysqld]\n") // would be ignored by explicit path
	explicit := writeCNFContent(t, "[client]\nuser=explicit\n")
	old := DefaultCNFSearch
	DefaultCNFSearch = []string{etc}
	defer func() { DefaultCNFSearch = old }()

	src := loadCNF(explicit, "client")
	if src == nil || src.User != "explicit" {
		t.Errorf("explicit file not used: %+v", src)
	}
}
