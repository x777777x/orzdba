package mysqlc

import (
	"os"
	"path/filepath"
	"testing"
)

func cnfPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "mycnf", "sample.cnf")
}

func TestParseClientSection(t *testing.T) {
	src, err := ParseMySQLDefaults(cnfPath(t), "client")
	if err != nil {
		t.Fatalf("ParseMySQLDefaults: %v", err)
	}
	if !src.Found {
		t.Fatal("Found=false, want true")
	}
	if src.User != "root" {
		t.Errorf("User = %q, want root", src.User)
	}
	if src.Password != "p@ss word" {
		t.Errorf("Password = %q, want \"p@ss word\" (quotes stripped, space kept)", src.Password)
	}
	if src.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want 127.0.0.1", src.Host)
	}
	if src.Port != 3306 {
		t.Errorf("Port = %d, want 3306", src.Port)
	}
	if src.Socket != "/tmp/mysql.sock" {
		t.Errorf("Socket = %q, want /tmp/mysql.sock", src.Socket)
	}
}

func TestParseMysqlSection(t *testing.T) {
	src, err := ParseMySQLDefaults(cnfPath(t), "mysql")
	if err != nil {
		t.Fatalf("ParseMySQLDefaults: %v", err)
	}
	if !src.Found {
		t.Fatal("Found=false, want true")
	}
	if src.User != "root" {
		t.Errorf("User = %q, want root", src.User)
	}
	if src.Password != "fakepass" {
		t.Errorf("Password = %q, want fakepass", src.Password)
	}
	// host/port/socket not in [mysql] section → zero values, but Found=true.
	if src.Host != "" || src.Port != 0 || src.Socket != "" {
		t.Errorf("expected empty host/port/socket, got host=%q port=%d socket=%q", src.Host, src.Port, src.Socket)
	}
}

func TestParseMissingSection(t *testing.T) {
	src, err := ParseMySQLDefaults(cnfPath(t), "nonexistent")
	if err != nil {
		t.Fatalf("ParseMySQLDefaults: %v", err)
	}
	if src.Found {
		t.Error("Found=true for nonexistent section, want false")
	}
}

func TestParseSkipsCommentsAndBoolKeys(t *testing.T) {
	// [mysqld] has a boolean key (skip-networking, no '=') and should not error
	// and not populate credential fields.
	src, err := ParseMySQLDefaults(cnfPath(t), "mysqld")
	if err != nil {
		t.Fatalf("ParseMySQLDefaults: %v", err)
	}
	if src.Found {
		t.Error("Found=true for mysqld section (no credential keys), want false")
	}
}

func TestCheckFileMode(t *testing.T) {
	dir := t.TempDir()
	strict := filepath.Join(dir, "strict.cnf")
	if err := os.WriteFile(strict, []byte("[client]\nuser=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if w := CheckFileMode(strict); w != "" {
		t.Errorf("0600 file warned, want none: %s", w)
	}
	loose := filepath.Join(dir, "loose.cnf")
	if err := os.WriteFile(loose, []byte("[client]\nuser=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if w := CheckFileMode(loose); w == "" {
		t.Error("0644 file did not warn, want a credential-leak warning")
	}
}
