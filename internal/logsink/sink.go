// Package logsink implements output sinks: stdout, single file, and
// daily-rotated file.
//
// File mode is forced to 0600 (with os.Chmod fallback) — no world-writable
// logs (plan §9.6, fixing orzdba-go P0-4). Data log writes go directly to
// file.Write, not through the log package (plan §9.6, avoiding orzdba-go
// P0-6's `log.New` flag misuse). Handles are held for the process lifetime,
// not reopened per tick (plan §5.6).
package logsink

import (
	"io"
	"os"
	"time"
)

// Sink is the output target. Write is the data path (no `log` package).
type Sink interface {
	io.Writer
	Close() error
}

// RotateSink is implemented by sinks that rotate on a day boundary. On
// rotation the caller reprints the title block and resets its row counter
// (plan §7.13: `count -= mycount; mycount = 0`).
type RotateSink interface {
	Sink
	// MaybeRotate rotates to a new day if `now` crossed midnight. Returns true
	// when a rotation happened.
	MaybeRotate(now time.Time) bool
}

// New returns the appropriate sink:
//   - no logfile            → Stdout (stdout only)
//   - -L path (--also-stdout off) → File/DailyFile (file only; historical behavior)
//   - -L path --also-stdout → Tee (stdout + file)
func New(logfile string, byDay, alsoStdout bool) (Sink, error) {
	if logfile == "" {
		return &Stdout{}, nil
	}
	var file Sink
	var err error
	if byDay {
		file, err = newDailyFile(logfile)
	} else {
		file, err = newFile(logfile)
	}
	if err != nil {
		return nil, err
	}
	if alsoStdout {
		return NewTee(os.Stdout, file), nil
	}
	return file, nil
}

// openFile opens (append) a 0600 log file + Chmod fallback. P1-3: O_APPEND
// (not O_TRUNC) so restarting orzdba never destroys previously collected data
// — a monitoring tool must survive a crash/restart without losing its log.
// newFile reports whether the file was freshly created (empty) so the caller
// can decide whether to reprint the title block.
func openFile(path string) (f *os.File, fresh bool, err error) {
	f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, false, err
	}
	_ = os.Chmod(path, 0o600)
	// A file is "fresh" (needs a title) if it was empty before appending.
	if fi, serr := f.Stat(); serr == nil {
		fresh = fi.Size() == 0
	}
	return f, fresh, nil
}
