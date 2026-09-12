package mysqlc

import (
	"os"
	"path/filepath"
	"strings"
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

func TestParseIncludeNote(t *testing.T) {
	// !include directives are recognized but not followed; the parser must
	// surface the first one so callers can warn instead of silently dropping
	// credentials defined in included files (P1-7).
	dir := t.TempDir()
	p := filepath.Join(dir, "inc.cnf")
	content := "!includedir /etc/my.cnf.d\n\n[client]\nuser=x\npassword=y\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := ParseMySQLDefaults(p, "client")
	if err != nil {
		t.Fatal(err)
	}
	if src.IncludeNote != "!includedir /etc/my.cnf.d" {
		t.Errorf("IncludeNote = %q, want the first ! directive", src.IncludeNote)
	}
	if !src.Found || src.User != "x" || src.Password != "y" {
		t.Errorf("credentials after !include mis-parsed: %+v", src)
	}
}

func TestUnescapeCnf(t *testing.T) {
	// Only the six sequences MySQL documents for option-file values are
	// decoded; everything else stays verbatim.
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{`pa\ss`, "pa s"},          // \s = space (option-file meaning)
		{`a\tb`, "a\tb"},           // tab
		{`a\nb`, "a\nb"},           // newline
		{`a\rb`, "a\rb"},           // carriage return
		{`a\\b`, `a\b`},            // \\ → one backslash
		{`a\\sb`, `a\sb`},          // \\ then s → backslash + s (NOT space)
		{`C:\temp`, "C:\temp"},     // \t IS documented → tab (famous MySQL gotcha)
		{`C:\path`, `C:\path`},     // \p undocumented → verbatim
		{`trailing\`, `trailing\`}, // dangling backslash → verbatim
		{`no escape with "quotes"`, `no escape with "quotes"`},
	}
	for _, c := range cases {
		if got := unescapeCnf(c.in); got != c.want {
			t.Errorf("unescapeCnf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParsePasswordWithEscapes(t *testing.T) {
	// A password written the way the mysql client reads it (my.cnf semantics)
	// must resolve to the same value here.
	dir := t.TempDir()
	p := filepath.Join(dir, "esc.cnf")
	content := "[client]\npassword = pa\\ss\\\\word\n" // → "pa s\word"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := ParseMySQLDefaults(p, "client")
	if err != nil {
		t.Fatal(err)
	}
	if src.Password != "pa s\\word" {
		t.Errorf("Password = %q, want %q", src.Password, "pa s\\word")
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

// TestCheckStrictFile: the orzdba-owned credential file must be 0600 AND owned
// by the running user (or root); any violation is a refusal, not a warning.
func TestCheckStrictFile(t *testing.T) {
	dir := t.TempDir()

	ok := filepath.Join(dir, "ok.cnf")
	if err := os.WriteFile(ok, []byte("[client]\nuser=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckStrictFile(ok); err != nil {
		t.Errorf("0600 file refused: %v", err)
	}

	loose := filepath.Join(dir, "loose.cnf")
	if err := os.WriteFile(loose, []byte("[client]\nuser=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckStrictFile(loose); err == nil {
		t.Error("0644 file accepted, want refusal")
	} else if !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("0644 refusal should hint chmod 600, got: %v", err)
	}

	// Owner mismatch: a file owned by someone else must be refused (uid 0 is
	// always accepted). Chown to an impossible owner only when the platform
	// reports owners; otherwise skip — Windows cannot produce a mismatch.
	if _, ok := fileOwnerUID(ok); ok {
		other := filepath.Join(dir, "other.cnf")
		if err := os.WriteFile(other, []byte("[client]\nuser=x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		// Find any uid that is neither us nor root.
		uid := os.Geteuid()
		target := 1
		if uid == 1 {
			target = 2
		}
		if target != 0 && target != uid {
			if err := os.Chown(other, target, -1); err == nil {
				if err := CheckStrictFile(other); err == nil {
					t.Error("file owned by another uid accepted, want refusal")
				} else if !strings.Contains(err.Error(), "chown") {
					t.Errorf("owner refusal should hint chown, got: %v", err)
				}
			}
		}
	}
}

// TestStrictCNFPathHeadsSearch: /etc/orzdba.cnf must precede the shared my.cnf
// entries so orzdba's own credentials win the merge.
func TestStrictCNFPathHeadsSearch(t *testing.T) {
	if DefaultCNFSearch[0] != StrictCNFPath {
		t.Errorf("DefaultCNFSearch[0] = %q, want %q (orzdba.cnf wins)", DefaultCNFSearch[0], StrictCNFPath)
	}
}
