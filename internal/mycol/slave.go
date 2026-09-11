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

// slaveCol reads a column from a SHOW SLAVE/REPLICA STATUS row map, falling
// back to the MySQL 8.0.22+ renamed spelling (master→source) when the old
// name is absent. Works against 5.7, 8.0 (either result set) and 8.4+.
func slaveCol(m map[string]string, old, renamed string) string {
	if v, ok := m[old]; ok {
		return v
	}
	return m[renamed]
}

// formatSlave renders the slave columns from a SHOW SLAVE STATUS row map.
// Pure (testable): readMLP/execMLP/chk WHITE, SecBM green (>300 red), NULL
// Seconds_Behind_Master → 0 (treated as caught up).
func formatSlave(m map[string]string) []metric.Cell {
	readMLP := parseInt64(slaveCol(m, "Read_Master_Log_Pos", "Read_Source_Log_Pos"))
	execMLP := parseInt64(slaveCol(m, "Exec_Master_Log_Pos", "Exec_Source_Log_Pos"))
	chk := readMLP - execMLP
	secBM := parseInt64(slaveCol(m, "Seconds_Behind_Master", "Seconds_Behind_Source"))
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
