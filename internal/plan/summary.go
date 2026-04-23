package plan

// Summary is the short per-plan highlight block printed under each
// rendered plan. Each field is a pointer so that a nil value signals
// "no node qualified at the configured threshold".
type Summary struct {
	TotalTimeMs        float64 `json:"total_time_ms"`
	NodeCount          int     `json:"node_count"`
	SlowestNode        *Node   `json:"-"`
	BiggestMisestimate *Node   `json:"-"`
	WorstFilterRatio   *Node   `json:"-"`

	// JSON-friendly ID projections of the three pointer fields, set by
	// Summarize and used by the JSON renderer.
	SlowestNodeID        int `json:"slowest_node_id,omitempty"`
	BiggestMisestimateID int `json:"biggest_misestimate_id,omitempty"`
	WorstFilterRatioID   int `json:"worst_filter_ratio_id,omitempty"`
}

// Thresholds are the cut-offs at which a node qualifies for the
// Summary block. Below these, the finding is noise.
const (
	// misestimateMinFactor — ratios under this are typical planner
	// variance, not worth surfacing.
	misestimateMinFactor = 10.0

	// filterRemovalMinRatio — below 80% removed, the scan is doing
	// useful work even if imperfect.
	filterRemovalMinRatio = 0.8

	// filterRemovalMinRows — a 99% removal ratio on 5 rows doesn't
	// matter; require meaningful volume.
	filterRemovalMinRows = 100
)

// Summarize inspects the plan and picks the most notable node for
// each category, skipping never-executed nodes throughout.
func Summarize(p *Plan) *Summary {
	if p == nil {
		return nil
	}
	s := &Summary{TotalTimeMs: p.TotalTimeMs}
	if p.Root == nil {
		return s
	}

	nodes := p.AllNodes()
	s.NodeCount = len(nodes)

	for _, n := range nodes {
		if n.NeverExecuted {
			continue
		}
		if s.SlowestNode == nil || n.ExclusiveTimeMs() > s.SlowestNode.ExclusiveTimeMs() {
			s.SlowestNode = n
		}
		if n.MisestimateFactor() >= misestimateMinFactor {
			if s.BiggestMisestimate == nil || n.MisestimateFactor() > s.BiggestMisestimate.MisestimateFactor() {
				s.BiggestMisestimate = n
			}
		}
		if n.RowsRemovedByFilter >= filterRemovalMinRows && n.FilterRemovalRatio() >= filterRemovalMinRatio {
			if s.WorstFilterRatio == nil || n.FilterRemovalRatio() > s.WorstFilterRatio.FilterRemovalRatio() {
				s.WorstFilterRatio = n
			}
		}
	}

	if s.SlowestNode != nil {
		s.SlowestNodeID = s.SlowestNode.ID
	}
	if s.BiggestMisestimate != nil {
		s.BiggestMisestimateID = s.BiggestMisestimate.ID
	}
	if s.WorstFilterRatio != nil {
		s.WorstFilterRatioID = s.WorstFilterRatio.ID
	}
	return s
}
