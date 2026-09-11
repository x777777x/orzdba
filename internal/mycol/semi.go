package mycol

import (
	"fmt"
	"strings"

	"orzdba/internal/metric"
)

// Semi reports semi-sync replication status: master ON/OFF, yes_tx, no_tx,
// no_timeouts (the orzdba-go -semi extension, plan §2.2/§6). When the plugin
// isn't loaded, all columns are 0/blank. Variable names follow the MySQL
// semi-sync plugin docs (orzdba-go's "no_times" is not a real status var).
type Semi struct{ src *StatusSource }

func NewSemi(s *StatusSource) *Semi { return &Semi{src: s} }
func (*Semi) Name() string          { return "semi" }
func (*Semi) Headline() (string, string) {
	return "---semi-sync--- ", "status  yesTx   noTx  noTimes|"
}

func (c *Semi) Collect() []metric.Cell {
	// MySQL 8.0.26 renamed the master-side semisync vars to source; read
	// whichever spelling the server exposes (both on 8.0.26-8.0.x).
	status := c.src.CurRawFallback("Rpl_semi_sync_master_status", "Rpl_semi_sync_source_status")
	on := 1
	if !strings.EqualFold(status, "ON") {
		on = 0
	}
	yesTx := c.src.CurFallback("Rpl_semi_sync_master_yes_tx", "Rpl_semi_sync_source_yes_tx")
	noTx := c.src.CurFallback("Rpl_semi_sync_master_no_tx", "Rpl_semi_sync_source_no_tx")
	noTimes := c.src.CurFallback("Rpl_semi_sync_master_no_timeouts", "Rpl_semi_sync_source_no_timeouts")
	col := metric.Green
	if noTx > 0 || noTimes > 0 {
		col = metric.Red
	}
	return []metric.Cell{
		{Text: fmt.Sprintf("%6d%8d%7d%9d", on, yesTx, noTx, noTimes), Color: col},
	}
}
