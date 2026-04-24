package rules

import (
	"fmt"
)

// Thresholds for the table-bloat heuristic. Conservative by design —
// the signal is cheap (blocks vs row work) and we'd rather miss
// subtle bloat than fire on wide-row tables that genuinely need
// many pages.
const (
	tableBloatMinBlocks     int64   = 64
	tableBloatInfoFactor    float64 = 2
	tableBloatWarningFactor float64 = 10
)

// TableBloat flags scans whose buffer reads are dramatically
// disproportionate to the row work they produced — a pattern
// consistent with dead-tuple accumulation or TOAST overhead. The
// suggested remediation is a plain VACUUM (ANALYZE), not VACUUM
// FULL, because FULL takes an exclusive lock.
type TableBloat struct{}

func (*TableBloat) ID() string         { return "table_bloat" }
func (*TableBloat) Name() string       { return "Possible table bloat" }
func (*TableBloat) Category() Category { return CategoryPrescriptive }

func (r *TableBloat) Check(ctx *RuleContext) []Finding {
	if ctx == nil || ctx.Plan == nil || ctx.Plan.Root == nil {
		return nil
	}
	var out []Finding
	for _, n := range ctx.Plan.AllNodes() {
		if n.NeverExecuted || !isScanNode(n.NodeType) {
			continue
		}
		if n.RelationName == "" {
			continue
		}
		blocks := n.SharedHitBlocks + n.SharedReadBlocks
		if blocks < tableBloatMinBlocks {
			continue
		}
		loops := n.Loops
		if loops < 1 {
			loops = 1
		}
		totalRows := loops * (n.ActualRows + n.RowsRemovedByFilter)
		factor := bloatFactor(blocks, totalRows, n.PlanWidth)
		if factor < tableBloatInfoFactor {
			continue
		}

		sev := SeverityInfo
		if factor >= tableBloatWarningFactor {
			sev = SeverityWarning
		}

		bytesRead := mulSaturating(blocks, 8192)
		var bytesPerRow int64
		if totalRows > 0 {
			bytesPerRow = bytesRead / totalRows
		}

		msg := fmt.Sprintf("Scan reads %d blocks (%s) for %d rows — approximately %d bytes per row. This may indicate table bloat or dead tuples.",
			blocks, humanBytes(bytesRead), totalRows, bytesPerRow)

		ev := map[string]any{
			"blocks_read":    blocks,
			"bytes_read":     bytesRead,
			"rows_returned":  totalRows,
			"bytes_per_row":  bytesPerRow,
			"bloat_factor":   factor,
		}
		f := newFinding(r, n.ID, sev, msg, ev)
		f.Suggested = fmt.Sprintf("VACUUM (ANALYZE) %s;", qualifiedRelation(n, n.RelationName))
		f.SuggestedMeta = map[string]any{
			"kind":     "vacuum",
			"relation": n.RelationName,
			"mode":     "standard",
		}
		out = append(out, f)
	}
	return out
}
