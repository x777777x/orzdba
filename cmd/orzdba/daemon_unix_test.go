//go:build unix

package main

import (
	"reflect"
	"testing"
)

func TestDaemonChildArgsStripsDaemon(t *testing.T) {
	got := daemonChildArgs([]string{"--daemon", "-t", "-l"}, "/tmp/orzdba.log")
	want := []string{"-t", "-l", "-L", "/tmp/orzdba.log", "-logfile_by_day"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("daemonChildArgs = %v, want %v", got, want)
	}
}

func TestDaemonChildArgsKeepsLogfile(t *testing.T) {
	// A -L is present → no default log injected.
	got := daemonChildArgs([]string{"--daemon", "-t", "-L", "/tmp/x.log"}, "/tmp/orzdba.log")
	want := []string{"-t", "-L", "/tmp/x.log"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("daemonChildArgs = %v, want %v", got, want)
	}
}

func TestDaemonChildArgsKeepsLogfileEquals(t *testing.T) {
	// --logfile=path (equals form) must also suppress the default.
	got := daemonChildArgs([]string{"--daemon", "--logfile=/tmp/y.log"}, "/tmp/orzdba.log")
	want := []string{"--logfile=/tmp/y.log"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("daemonChildArgs = %v, want %v", got, want)
	}
}

func TestDaemonChildArgsPreservesOtherFlags(t *testing.T) {
	got := daemonChildArgs([]string{"--daemon", "-H", "1.2.3.4", "-P", "3307", "-mysql"}, "/tmp/orzdba.log")
	// Expect the mysql/host/port args kept, plus the injected default log.
	want := []string{"-H", "1.2.3.4", "-P", "3307", "-mysql", "-L", "/tmp/orzdba.log", "-logfile_by_day"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("daemonChildArgs = %v, want %v", got, want)
	}
}
