package main

import (
	"errors"
	"testing"
)

// failSink fails the first N writes, then succeeds (simulating a transient
// hiccup); failAll fails every write (disk full / file removed).
type failSink struct {
	remainingFails int
	writes         int
}

func (s *failSink) Write(p []byte) (int, error) {
	s.writes++
	if s.writes <= s.remainingFails {
		return 0, errors.New("disk full")
	}
	return len(p), nil
}
func (s *failSink) Close() error { return nil }

func TestSinkWriterToleratesTransientFailures(t *testing.T) {
	// Two failures then recovery → never fatal; the counter resets on success.
	sink := &failSink{remainingFails: 2}
	w := &sinkWriter{sink: sink}
	for i := 0; i < 2; i++ {
		if w.write("x") {
			t.Fatalf("write %d reported dead, want tolerated", i+1)
		}
	}
	if w.write("x") {
		t.Fatal("recovered write reported dead")
	}
	// Counter must be reset: another 2 failures are still tolerated.
	for i := 0; i < 2; i++ {
		if w.write("x") {
			t.Fatalf("post-recovery failure %d reported dead, want tolerated", i+1)
		}
	}
}

func TestSinkWriterDiesAfterThreeConsecutiveFailures(t *testing.T) {
	sink := &failSink{remainingFails: 1 << 30} // always fails
	w := &sinkWriter{sink: sink}
	for i := 0; i < 2; i++ {
		if w.write("x") {
			t.Fatalf("failure %d reported dead, want tolerated", i+1)
		}
	}
	if !w.write("x") {
		t.Fatal("third consecutive failure must be fatal")
	}
}
