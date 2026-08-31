package logsink

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileWriteAndMode0600(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "o.log")
	s, err := newFile(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %o, want 0600 (plan §9.6, fixing P0-4)", fi.Mode().Perm())
	}
	b, _ := os.ReadFile(p)
	if string(b) != "hello\n" {
		t.Errorf("file content = %q, want \"hello\\n\"", string(b))
	}
}

func TestFileNotWritableErrors(t *testing.T) {
	// A path under a non-existent directory should error (plan §11.1).
	if _, err := newFile("/nonexistent-dir/o.log"); err == nil {
		t.Error("expected error opening file in a missing dir")
	}
}

func TestNewFactory(t *testing.T) {
	s, err := New("", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(*Stdout); !ok {
		t.Error("New('',false,false) should return *Stdout")
	}
	dir := t.TempDir()
	f, err := New(filepath.Join(dir, "a.log"), false, false)
	if err != nil || f == nil {
		t.Errorf("New(path,false,false) failed: %v", err)
	}
	if _, ok := f.(*File); !ok {
		t.Error("New(path,false,false) should return *File")
	}
	d, err := New(filepath.Join(dir, "b.log"), true, false)
	if err != nil || d == nil {
		t.Errorf("New(path,true,false) failed: %v", err)
	}
	if _, ok := d.(*DailyFile); !ok {
		t.Error("New(path,true,false) should return *DailyFile")
	}
	// --also-stdout with a file → Tee (stdout + file double-write).
	te, err := New(filepath.Join(dir, "c.log"), false, true)
	if err != nil || te == nil {
		t.Errorf("New(path,false,true) failed: %v", err)
	}
	if _, ok := te.(*Tee); !ok {
		t.Error("New(path,false,true) should return *Tee")
	}
}

func TestDailyFileNoRotateSameDay(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "o.log")
	s, err := newDailyFile(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	// Same day → no rotation.
	if s.MaybeRotate(now) {
		t.Error("MaybeRotate(same day) returned true, want false")
	}
}

func TestDailyFileRotatesNextDay(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "o.log")
	s, err := newDailyFile(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	today := time.Now()
	tomorrow := today.AddDate(0, 0, 1)
	todayPath := p + "." + today.Format("2006-01-02")
	tomorrowPath := p + "." + tomorrow.Format("2006-01-02")

	// today's file exists, tomorrow's doesn't yet.
	if _, err := os.Stat(todayPath); err != nil {
		t.Fatalf("today's file not created: %v", err)
	}
	if !s.MaybeRotate(tomorrow) {
		t.Fatal("MaybeRotate(next day) returned false, want true")
	}
	// Tomorrow's file should now exist at 0600.
	fi, err := os.Stat(tomorrowPath)
	if err != nil {
		t.Fatalf("tomorrow's file not created: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("rotated file mode = %o, want 0600", fi.Mode().Perm())
	}
	// Writes now go to the new file.
	if _, err := s.Write([]byte("day2\n")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(tomorrowPath)
	if string(b) != "day2\n" {
		t.Errorf("new-day write went to %q, want day2 in %s", string(b), tomorrowPath)
	}
}

// TestTeeDoubleWrite verifies --also-stdout behavior: one Write reaches both
// stdout and the file.
func TestTeeDoubleWrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "t.log")
	file, err := newFile(p)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var buf bytes.Buffer
	tee := NewTee(&buf, file)
	if _, err := tee.Write([]byte("row1\n")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "row1\n" {
		t.Errorf("stdout side = %q, want \"row1\\n\"", got)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "row1\n" {
		t.Errorf("file side = %q, want \"row1\\n\"", string(b))
	}
}

// TestTeeDailyFileRotate verifies Tee delegates rotation to an inner
// DailyFile, so --also-stdout with -logfile_by_day still rotates.
func TestTeeDailyFileRotate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "o.log")
	df, err := newDailyFile(p)
	if err != nil {
		t.Fatal(err)
	}
	defer df.Close()
	var buf bytes.Buffer
	tee := NewTee(&buf, df)
	if r := tee.MaybeRotate(time.Now()); r {
		t.Error("MaybeRotate(same day) via Tee returned true, want false")
	}
	tomorrow := time.Now().AddDate(0, 0, 1)
	if r := tee.MaybeRotate(tomorrow); !r {
		t.Error("MaybeRotate(next day) via Tee returned false, want true")
	}
}

// TestTeeFresh verifies Fresh is delegated to the inner file.
func TestTeeFresh(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "fresh.log")
	file, err := newFile(p)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	tee := NewTee(&bytes.Buffer{}, file)
	if !tee.Fresh() {
		t.Error("Tee.Fresh() on a brand-new file = false, want true")
	}
	if _, err := tee.Write([]byte("x\n")); err != nil {
		t.Fatal(err)
	}
	// Reopen a second file (already non-empty) → Fresh false.
	file2, err := newFile(p)
	if err != nil {
		t.Fatal(err)
	}
	defer file2.Close()
	tee2 := NewTee(&bytes.Buffer{}, file2)
	if tee2.Fresh() {
		t.Error("Tee.Fresh() on a non-empty file = true, want false")
	}
}
