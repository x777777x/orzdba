package logsink

import (
	"io"
	"time"
)

// Tee writes every row to both stdout and a file sink (the --also-stdout
// behavior for -L). The file side reuses the existing File/DailyFile rotation
// logic, so -logfile_by_day still works unchanged. Close closes only the file
// side (stdout needs no close).
type Tee struct {
	out  io.Writer // stdout
	file Sink      // File or DailyFile
}

// NewTee returns a sink that writes to out (stdout) and to the file sink.
func NewTee(out io.Writer, file Sink) *Tee {
	return &Tee{out: out, file: file}
}

// Fresh reports whether the underlying file was empty at open time (title
// needed). Mirrors the file sink's Fresh so the caller prints the title once.
func (t *Tee) Fresh() bool {
	if f, ok := t.file.(interface{ Fresh() bool }); ok {
		return f.Fresh()
	}
	return true
}

// Write writes to both stdout and the file. If the file write fails, the
// error is returned (the row may still have reached stdout — a monitoring
// tool should prefer losing the file copy over losing the data entirely).
func (t *Tee) Write(p []byte) (int, error) {
	_, _ = t.out.Write(p)
	return t.file.Write(p)
}

// Close closes the file sink (stdout is not closed).
func (t *Tee) Close() error { return t.file.Close() }

// MaybeRotate delegates to the underlying RotateSink (DailyFile); returns
// false when the file side does not rotate.
func (t *Tee) MaybeRotate(now time.Time) bool {
	if r, ok := t.file.(RotateSink); ok {
		return r.MaybeRotate(now)
	}
	return false
}
