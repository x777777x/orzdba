package logsink

import (
	"io"
	"os"
)

// File is a single, long-held log file (no rotation). Mode is forced 0600
// with a Chmod fallback (plan §9.6, fixing orzdba-go P0-4). Writes append
// (P1-3) so restarts preserve history. Fresh reports whether the file was
// empty at open time — the caller prints the title block only then.
type File struct {
	f     *os.File
	path  string
	fresh bool // true when opened a brand-new (empty) file
}

func newFile(path string) (*File, error) {
	f, fresh, err := openFile(path)
	if err != nil {
		return nil, err
	}
	return &File{f: f, path: path, fresh: fresh}, nil
}

// Fresh reports whether this file was empty at open time (title needed).
func (s *File) Fresh() bool { return s.fresh }

func (s *File) Write(p []byte) (int, error) { return s.f.Write(p) }
func (s *File) Close() error                { return s.f.Close() }

// Reopen is used by DailyFile; kept here to share the openFile helper.
func (s *File) reopen() error {
	if err := s.f.Close(); err != nil {
		return err
	}
	f, fresh, err := openFile(s.path)
	if err != nil {
		return err
	}
	s.f = f
	s.fresh = fresh
	return nil
}

// Compile-time interface checks.
var (
	_ Sink      = (*Stdout)(nil)
	_ Sink      = (*File)(nil)
	_ io.Writer = (*File)(nil)
)
