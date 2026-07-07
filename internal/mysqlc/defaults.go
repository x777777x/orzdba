// Package mysqlc — credential resolution helpers.
//
// The my.cnf parser is self-implemented (no third-party INI library) to keep
// the credential-handling attack surface minimal (plan §8.2). It supports
// section headers, key=value / key = value, comments (# and ;), quote
// stripping, and returns only user/password/host/port/socket for the requested
// group. !include/!includedir are recognized but not yet followed (TODO:
// recursion-bounded include support per plan §8.2).
package mysqlc

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// CNFSource holds the credentials read from one my.cnf group.
type CNFSource struct {
	User     string
	Password string
	Host     string
	Socket   string
	Port     int
	Found    bool
}

// ParseMySQLDefaults parses path and returns the key=value pairs found under
// the named group (e.g. "client" or "mysql"). Only credential-relevant keys
// are captured; others are ignored.
func ParseMySQLDefaults(path, group string) (*CNFSource, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	src := &CNFSource{Port: 0}
	if err := parseCNF(f, group, src); err != nil {
		return nil, err
	}
	return src, nil
}

// parseCNF reads an INI-style stream into src for the given group.
func parseCNF(r io.Reader, group string, src *CNFSource) error {
	section := ""
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4*1024), 256*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if strings.HasPrefix(line, "!") {
			continue // !include / !includedir — not yet followed
		}
		if line[0] == '[' && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if section != group {
			continue
		}
		key, val, ok := splitKV(line)
		if !ok {
			continue
		}
		val = unquote(val)
		switch key {
		case "user":
			src.User = val
			src.Found = true
		case "password":
			src.Password = val
			src.Found = true
		case "host":
			src.Host = val
			src.Found = true
		case "socket":
			src.Socket = val
			src.Found = true
		case "port":
			if p, err := strconv.Atoi(val); err == nil {
				src.Port = p
				src.Found = true
			}
		}
	}
	return sc.Err()
}

// splitKV splits "key = value" or "key=value" on the first '='. A line without
// '=' (a boolean key like skip-networking) returns ok=false.
func splitKV(line string) (string, string, bool) {
	i := strings.IndexByte(line, '=')
	if i < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

// unquote strips one matched pair of surrounding single/double quotes.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// CheckFileMode warns (via the returned string) if the file is readable by
// group/other — a credential-exposure risk (plan §8.4).
func CheckFileMode(path string) (warning string) {
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if fi.Mode()&0o077 != 0 {
		return fmt.Sprintf("warn: %s is group/other accessible (mode %o) — credentials may leak", path, fi.Mode().Perm())
	}
	return ""
}
