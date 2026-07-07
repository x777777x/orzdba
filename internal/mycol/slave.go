package mycol

import (
	"fmt"

	"orzdba/internal/metric"
)

// Slave reports replication status from SHOW SLAVE STATUS: Read/Exec Master
// Log Pos, their difference (replication lag in bytes), and Seconds_Behind_Master
// (the orzdba-go -slave extension, plan §2.2/§6). When the server is not a
// replica, all columns are 0.
type Slave struct{ src *StatusSource }

func NewSlave(s *StatusSource) *Slave { return &Slave{src: s} }
func (*Slave) Name() string           { return "slave" }
func (*Slave) Headline() (string, string) {
	return "---------------SlaveStatus------------- ",
		"    ReadMLP     ExecMLP   chkRE   SecBM|"
}

func (c *Slave) Collect() []metric.Cell {
	m, ok := c.src.SlaveStatus()
	if !ok {
		return zeroSlave()
	}
	return formatSlave(m)
}

// formatSlave renders the slave columns from a SHOW SLAVE STATUS row map.
// Pure (testable): readMLP/execMLP/chk WHITE, SecBM green (>300 red), NULL
// Seconds_Behind_Master → 0 (treated as caught up).
func formatSlave(m map[string]string) []metric.Cell {
	readMLP := parseI64(m["Read_Master_Log_Pos"])
	execMLP := parseI64(m["Exec_Master_Log_Pos"])
	chk := readMLP - execMLP
	secBM := parseI64(m["Seconds_Behind_Master"])
	col := metric.Green
	if secBM > 300 {
		col = metric.Red
	}
	return []metric.Cell{
		{Text: fmt.Sprintf("%11d%12d%8d", readMLP, execMLP, chk), Color: metric.White},
		{Text: fmt.Sprintf("%8d", secBM), Color: col},
	}
}

func zeroSlave() []metric.Cell {
	return []metric.Cell{{Text: fmt.Sprintf("%11d%12d%8d%8d", 0, 0, 0, 0), Color: metric.White}}
}

// parseI64 parses a string int, 0 on failure (NULL/empty/"NULL" → 0).
func parseI64(s string) int64 {
	var n int64
	fmt.Sscanf(s, "%d", &n)
	return n
}
