// Package plan parses PostgreSQL's EXPLAIN (ANALYZE, BUFFERS, FORMAT
// JSON) output into a typed, renderable plan tree, and computes the
// derived statistics (exclusive time, misestimate factor, cache hit
// ratio, etc.) that downstream renderers and rule engines consume.
package plan

// Plan is a single parsed EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
// document. PostgreSQL emits this as a JSON array; Plan represents the
// first element plus its top-level metadata.
type Plan struct {
	Root              *Node             `json:"root"`
	PlanningTimeMs    float64           `json:"planning_time_ms"`
	ExecutionTimeMs   float64           `json:"execution_time_ms"`
	TotalTimeMs       float64           `json:"total_time_ms"`
	Triggers          []Trigger         `json:"triggers,omitempty"`
	JIT               *JITInfo          `json:"jit,omitempty"`
	Settings          map[string]string `json:"settings,omitempty"`
	QueryText         string            `json:"query_text,omitempty"`
	SourceDescription string            `json:"source_description,omitempty"`
}

// Node is one entry in the plan tree. Times and rows are reported as
// PostgreSQL emits them (per-loop for non-parallel; per-worker average
// for parallel leaves); callers that want totals should use the helpers
// in stats.go.
type Node struct {
	ID       int     `json:"id"`
	Parent   *Node   `json:"-"`
	Children []*Node `json:"children,omitempty"`
	Depth    int     `json:"depth"`

	NodeType     NodeType `json:"node_type"`
	RawNodeType  string   `json:"raw_node_type"`
	ParentRel    string   `json:"parent_rel,omitempty"`
	RelationName string   `json:"relation_name,omitempty"`
	Alias        string   `json:"alias,omitempty"`
	IndexName    string   `json:"index_name,omitempty"`
	Schema       string   `json:"schema,omitempty"`

	StartupCost float64 `json:"startup_cost"`
	TotalCost   float64 `json:"total_cost"`
	PlanRows    int64   `json:"plan_rows"`
	PlanWidth   int64   `json:"plan_width"`

	ActualStartupTimeMs float64 `json:"actual_startup_time_ms"`
	ActualTotalTimeMs   float64 `json:"actual_total_time_ms"`
	ActualRows          int64   `json:"actual_rows"`
	Loops               int64   `json:"loops"`
	NeverExecuted       bool    `json:"never_executed,omitempty"`

	Filter      string `json:"filter,omitempty"`
	IndexCond   string `json:"index_cond,omitempty"`
	HashCond    string `json:"hash_cond,omitempty"`
	MergeCond   string `json:"merge_cond,omitempty"`
	JoinFilter  string `json:"join_filter,omitempty"`
	RecheckCond string `json:"recheck_cond,omitempty"`

	RowsRemovedByFilter       int64 `json:"rows_removed_by_filter,omitempty"`
	RowsRemovedByIndexRecheck int64 `json:"rows_removed_by_index_recheck,omitempty"`
	RowsRemovedByJoinFilter   int64 `json:"rows_removed_by_join_filter,omitempty"`

	SortKey       []string `json:"sort_key,omitempty"`
	SortMethod    string   `json:"sort_method,omitempty"`
	SortSpaceKB   int64    `json:"sort_space_kb,omitempty"`
	SortSpaceType string   `json:"sort_space_type,omitempty"`

	GroupKey []string `json:"group_key,omitempty"`
	Strategy string   `json:"strategy,omitempty"`

	SharedHitBlocks     int64 `json:"shared_hit_blocks,omitempty"`
	SharedReadBlocks    int64 `json:"shared_read_blocks,omitempty"`
	SharedDirtiedBlocks int64 `json:"shared_dirtied_blocks,omitempty"`
	SharedWrittenBlocks int64 `json:"shared_written_blocks,omitempty"`
	LocalHitBlocks      int64 `json:"local_hit_blocks,omitempty"`
	LocalReadBlocks     int64 `json:"local_read_blocks,omitempty"`
	TempReadBlocks      int64 `json:"temp_read_blocks,omitempty"`
	TempWrittenBlocks   int64 `json:"temp_written_blocks,omitempty"`

	WorkersPlanned  int          `json:"workers_planned,omitempty"`
	WorkersLaunched int          `json:"workers_launched,omitempty"`
	Workers         []WorkerInfo `json:"workers,omitempty"`

	HeapFetches int64 `json:"heap_fetches,omitempty"`

	SubplanName string `json:"subplan_name,omitempty"`

	Extra map[string]any `json:"extra,omitempty"`
}

// WorkerInfo describes a single parallel worker's contribution to its
// parent scan node.
type WorkerInfo struct {
	Number              int     `json:"number"`
	ActualStartupTimeMs float64 `json:"actual_startup_time_ms"`
	ActualTotalTimeMs   float64 `json:"actual_total_time_ms"`
	ActualRows          int64   `json:"actual_rows"`
	ActualLoops         int64   `json:"actual_loops"`
	SharedHitBlocks     int64   `json:"shared_hit_blocks,omitempty"`
	SharedReadBlocks    int64   `json:"shared_read_blocks,omitempty"`
}

// Trigger records a firing from the "Triggers" block.
type Trigger struct {
	Name     string  `json:"name"`
	Relation string  `json:"relation,omitempty"`
	TimeMs   float64 `json:"time_ms"`
	Calls    int64   `json:"calls"`
}

// JITInfo captures the "JIT" block when present.
type JITInfo struct {
	Functions int                `json:"functions,omitempty"`
	Options   map[string]bool    `json:"options,omitempty"`
	TimingMs  map[string]float64 `json:"timing_ms,omitempty"`
}

// NodeType is the canonical, typed form of "Node Type" strings that
// PostgreSQL emits. Strings like "HashAggregate" and "GroupAggregate"
// both collapse to NodeTypeAggregate; the variant is preserved in
// Node.Strategy.
type NodeType int

const (
	NodeTypeUnknown NodeType = iota
	NodeTypeSeqScan
	NodeTypeIndexScan
	NodeTypeIndexOnlyScan
	NodeTypeBitmapIndexScan
	NodeTypeBitmapHeapScan
	NodeTypeTidScan
	NodeTypeNestedLoop
	NodeTypeHashJoin
	NodeTypeMergeJoin
	NodeTypeHash
	NodeTypeSort
	NodeTypeIncrementalSort
	NodeTypeAggregate
	NodeTypeWindowAgg
	NodeTypeLimit
	NodeTypeMaterialize
	NodeTypeMemoize
	NodeTypeGather
	NodeTypeGatherMerge
	NodeTypeAppend
	NodeTypeMergeAppend
	NodeTypeSubqueryScan
	NodeTypeCTEScan
	NodeTypeWorkTableScan
	NodeTypeResult
	NodeTypeProjectSet
	NodeTypeUnique
	NodeTypeSetOp
	NodeTypeLockRows
	NodeTypeModifyTable
	NodeTypeFunctionScan
	NodeTypeValuesScan
	NodeTypeForeignScan
)

// nodeTypeNames maps typed NodeType back to its canonical string. The
// reverse map (string → NodeType) is derived in init() so that
// ParseNodeType and (NodeType).String() stay in lockstep.
var nodeTypeNames = map[NodeType]string{
	NodeTypeUnknown:         "Unknown",
	NodeTypeSeqScan:         "Seq Scan",
	NodeTypeIndexScan:       "Index Scan",
	NodeTypeIndexOnlyScan:   "Index Only Scan",
	NodeTypeBitmapIndexScan: "Bitmap Index Scan",
	NodeTypeBitmapHeapScan:  "Bitmap Heap Scan",
	NodeTypeTidScan:         "Tid Scan",
	NodeTypeNestedLoop:      "Nested Loop",
	NodeTypeHashJoin:        "Hash Join",
	NodeTypeMergeJoin:       "Merge Join",
	NodeTypeHash:            "Hash",
	NodeTypeSort:            "Sort",
	NodeTypeIncrementalSort: "Incremental Sort",
	NodeTypeAggregate:       "Aggregate",
	NodeTypeWindowAgg:       "WindowAgg",
	NodeTypeLimit:           "Limit",
	NodeTypeMaterialize:     "Materialize",
	NodeTypeMemoize:         "Memoize",
	NodeTypeGather:          "Gather",
	NodeTypeGatherMerge:     "Gather Merge",
	NodeTypeAppend:          "Append",
	NodeTypeMergeAppend:     "Merge Append",
	NodeTypeSubqueryScan:    "Subquery Scan",
	NodeTypeCTEScan:         "CTE Scan",
	NodeTypeWorkTableScan:   "WorkTable Scan",
	NodeTypeResult:          "Result",
	NodeTypeProjectSet:      "ProjectSet",
	NodeTypeUnique:          "Unique",
	NodeTypeSetOp:           "SetOp",
	NodeTypeLockRows:        "LockRows",
	NodeTypeModifyTable:     "ModifyTable",
	NodeTypeFunctionScan:    "Function Scan",
	NodeTypeValuesScan:      "Values Scan",
	NodeTypeForeignScan:     "Foreign Scan",
}

// aggregateAliases collapse PostgreSQL's three Aggregate strings into
// one NodeType. The specific flavor is preserved on Node.Strategy
// during parsing.
var aggregateAliases = map[string]NodeType{
	"Aggregate":      NodeTypeAggregate,
	"HashAggregate":  NodeTypeAggregate,
	"GroupAggregate": NodeTypeAggregate,
	"MixedAggregate": NodeTypeAggregate,
}

// nodeTypeByString is built from nodeTypeNames + aggregateAliases.
var nodeTypeByString map[string]NodeType

func init() {
	nodeTypeByString = make(map[string]NodeType, len(nodeTypeNames)+len(aggregateAliases))
	for t, s := range nodeTypeNames {
		if t == NodeTypeUnknown {
			continue
		}
		nodeTypeByString[s] = t
	}
	for s, t := range aggregateAliases {
		nodeTypeByString[s] = t
	}
}

// String returns the canonical PostgreSQL node-type string for t.
func (t NodeType) String() string {
	if s, ok := nodeTypeNames[t]; ok {
		return s
	}
	return "Unknown"
}

// ParseNodeType maps a raw "Node Type" string to a typed NodeType.
// Unknown strings return NodeTypeUnknown so the caller can still keep
// the raw string on Node.RawNodeType.
func ParseNodeType(s string) NodeType {
	if t, ok := nodeTypeByString[s]; ok {
		return t
	}
	return NodeTypeUnknown
}
