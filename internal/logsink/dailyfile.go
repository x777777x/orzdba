package logsink

import (
	"fmt"
	"time"
)

// DailyFile rotates the log file at midnight, naming files path.<YYYY-MM-DD>
// (plan §6: -logfile_by_day suffix 'yyyy-mm-dd'). On rotation it closes the
// current file and opens the new day's file (0600), then returns true so the
// caller reprints the title and resets its row counter (plan §7.13).
type DailyFile struct {
	path string
	f    *File
	day  string // current "YYYY-MM-DD"
}

func newDailyFile(path string) (*DailyFile, error) {
	now := time.Now()
	day := now.Format("2006-01-02")
	f, err := newFile(path + "." + day)
	if err != nil {
		return nil, err
	}
	return &DailyFile{path: path, f: f, day: day}, nil
}

func (s *DailyFile) Write(p []byte) (int, error) { return s.f.Write(p) }
func (s *DailyFile) Close() error                { return s.f.Close() }

// MaybeRotate opens a new dated file when the day changes. The detection
// mirrors the Perl original's `-e $logfile_day` check (lines 178-190) but uses
// a day-string comparison, which is race-free and equivalent.
func (s *DailyFile) MaybeRotate(now time.Time) bool {
	day := now.Format("2006-01-02")
	if day == s.day {
		return false
	}
	// Open the new day's file (newFile truncates a 0600 file).
	nf, err := newFile(fmt.Sprintf("%s.%s", s.path, day))
	if err != nil {
		// Keep writing to the old file rather than dropping the tick.
		s.day = day
		return false
	}
	_ = s.f.Close()
	s.f = nf
	s.day = day
	return true
}
