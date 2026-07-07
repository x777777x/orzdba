package logsink

import "os"

// Stdout writes to standard output (the default when -L is not given).
type Stdout struct{}

func (*Stdout) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (*Stdout) Close() error                { return nil }
