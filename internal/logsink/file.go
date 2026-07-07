package logsink

import (
	"io"
	"os"
)

// File is a single, long-held log file (no rotation). Mode is forced 0600
// with a Chmod fallback (plan §9.6, fixing orzdba-go P0-4).
type File struct {
	f    *os.File
	path string
}

func newFile(path string) (*File, error) {
	f, err := openFile(path)
	if err != nil {
		return nil, err
	}
	return &File{f: f, path: path}, nil
}

func (s *File) Write(p []byte) (int, error) { return s.f.Write(p) }
func (s *File) Close() error                { return s.f.Close() }

// Reopen is used by DailyFile; kept here to share the openFile helper.
func (s *File) reopen() error {
	if err := s.f.Close(); err != nil {
		return err
	}
	f, err := openFile(s.path)
	if err != nil {
		return err
	}
	s.f = f
	return nil
}

// Compile-time interface checks.
var (
	_ Sink      = (*Stdout)(nil)
	_ Sink      = (*File)(nil)
	_ io.Writer = (*File)(nil)
)
