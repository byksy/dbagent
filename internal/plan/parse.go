package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Sentinel errors returned by Parse / ParseBytes.
var (
	ErrEmptyPlan         = errors.New("empty plan input")
	ErrInvalidJSON       = errors.New("invalid plan JSON")
	ErrUnsupportedFormat = errors.New("unsupported EXPLAIN format (use FORMAT JSON)")
)

// utf8BOM is the byte sequence PostgreSQL clients occasionally prepend
// when redirecting psql output through a UTF-8 file.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Parse reads an EXPLAIN (FORMAT JSON) document from r and returns a
// fully populated Plan. See package doc for the accepted input shapes.
func Parse(r io.Reader) (*Plan, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("plan: read input: %w", err)
	}
	return ParseBytes(raw)
}

// ParseBytes is the byte-slice counterpart to Parse.
func ParseBytes(b []byte) (*Plan, error) {
	normalized, err := normalizeInput(b)
	if err != nil {
		return nil, err
	}

	// The canonical shape is an array of length ≥ 1. Decoding into
	// []rawDoc also tolerates an empty array (we reject that below).
	var docs []rawDoc
	if err := json.Unmarshal(normalized, &docs); err != nil {
		// Accept a bare object as well — some psql pipelines lose the
		// surrounding array. Try again; if still malformed, bubble up
		// the original decoder error as ErrInvalidJSON.
		var one rawDoc
		if err2 := json.Unmarshal(normalized, &one); err2 != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidJSON, err)
		}
		docs = []rawDoc{one}
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("%w: no plan entries", ErrInvalidJSON)
	}

	doc := docs[0]
	if doc.Plan == nil {
		return nil, fmt.Errorf("%w: missing \"Plan\" key", ErrInvalidJSON)
	}

	p := &Plan{
		PlanningTimeMs:  doc.PlanningTime,
		ExecutionTimeMs: doc.ExecutionTime,
		Settings:        stringMap(doc.Settings),
	}
	p.TotalTimeMs = p.PlanningTimeMs + p.ExecutionTimeMs

	for _, t := range doc.Triggers {
		p.Triggers = append(p.Triggers, Trigger{
			Name:     t.TriggerName,
			Relation: t.Relation,
			TimeMs:   t.Time,
			Calls:    t.Calls,
		})
	}
	if doc.JIT != nil {
		p.JIT = &JITInfo{
			Functions: doc.JIT.Functions,
			Options:   doc.JIT.Options,
			TimingMs:  doc.JIT.Timing,
		}
	}

	rawPlan, err := decodeRawNode(doc.Plan)
	if err != nil {
		return nil, err
	}
	root := buildNode(rawPlan, nil, 0)

	nextID := 1
	assignIDs(root, &nextID)

	p.Root = root
	return p, nil
}

// normalizeInput strips leading whitespace and BOM, unwraps a single
// surrounding pair of double quotes (pgAdmin paste habit), and rejects
// obviously-TEXT-format inputs with ErrUnsupportedFormat. Returns the
// normalized byte slice or a typed sentinel error.
func normalizeInput(b []byte) ([]byte, error) {
	b = bytes.TrimSpace(b)
	b = bytes.TrimPrefix(b, utf8BOM)
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil, ErrEmptyPlan
	}
	// Surrounding quotes (whole-file quoted paste): if we see that,
	// strip once and un-escape doubled inner quotes.
	if len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"' {
		inner := b[1 : len(b)-1]
		inner = bytes.ReplaceAll(inner, []byte{'"', '"'}, []byte{'"'})
		b = bytes.TrimSpace(inner)
		if len(b) == 0 {
			return nil, ErrEmptyPlan
		}
	}
	// Anything that doesn't start with '[' or '{' after normalization is
	// not JSON — most commonly psql TEXT output starting with "QUERY PLAN".
	if b[0] != '[' && b[0] != '{' {
		return nil, fmt.Errorf("%w: expected JSON output from EXPLAIN (FORMAT JSON); TEXT/YAML/XML are not supported yet", ErrUnsupportedFormat)
	}
	return b, nil
}

// rawDoc matches the top-level JSON shape.
type rawDoc struct {
	Plan          json.RawMessage    `json:"Plan"`
	PlanningTime  float64            `json:"Planning Time"`
	ExecutionTime float64            `json:"Execution Time"`
	Triggers      []rawTrigger       `json:"Triggers"`
	JIT           *rawJIT            `json:"JIT"`
	Settings      map[string]any     `json:"Settings"`
}

type rawTrigger struct {
	TriggerName string  `json:"Trigger Name"`
	Relation    string  `json:"Relation"`
	Time        float64 `json:"Time"`
	Calls       int64   `json:"Calls"`
}

type rawJIT struct {
	Functions int                `json:"Functions"`
	Options   map[string]bool    `json:"Options"`
	Timing    map[string]float64 `json:"Timing"`
}

// rawNode holds the typed subset of fields we care about. Any key not
// listed here is preserved on Node.Extra via decodeRawNode's second
// pass.
type rawNode struct {
	NodeType     string   `json:"Node Type"`
	ParentRel    string   `json:"Parent Relationship"`
	RelationName string   `json:"Relation Name"`
	Alias        string   `json:"Alias"`
	IndexName    string   `json:"Index Name"`
	Schema       string   `json:"Schema"`
	SubplanName  string   `json:"Subplan Name"`

	StartupCost float64 `json:"Startup Cost"`
	TotalCost   float64 `json:"Total Cost"`
	PlanRows    int64   `json:"Plan Rows"`
	PlanWidth   int64   `json:"Plan Width"`

	ActualStartupTime *float64 `json:"Actual Startup Time"`
	ActualTotalTime   *float64 `json:"Actual Total Time"`
	ActualRows        *int64   `json:"Actual Rows"`
	ActualLoops       *int64   `json:"Actual Loops"`

	Filter      string `json:"Filter"`
	IndexCond   string `json:"Index Cond"`
	HashCond    string `json:"Hash Cond"`
	MergeCond   string `json:"Merge Cond"`
	JoinFilter  string `json:"Join Filter"`
	RecheckCond string `json:"Recheck Cond"`

	RowsRemovedByFilter       int64 `json:"Rows Removed by Filter"`
	RowsRemovedByIndexRecheck int64 `json:"Rows Removed by Index Recheck"`
	RowsRemovedByJoinFilter   int64 `json:"Rows Removed by Join Filter"`

	SortKey       []string `json:"Sort Key"`
	SortMethod    string   `json:"Sort Method"`
	SortSpaceUsed int64    `json:"Sort Space Used"`
	SortSpaceType string   `json:"Sort Space Type"`

	GroupKey []string `json:"Group Key"`
	Strategy string   `json:"Strategy"`

	SharedHitBlocks     int64 `json:"Shared Hit Blocks"`
	SharedReadBlocks    int64 `json:"Shared Read Blocks"`
	SharedDirtiedBlocks int64 `json:"Shared Dirtied Blocks"`
	SharedWrittenBlocks int64 `json:"Shared Written Blocks"`
	LocalHitBlocks      int64 `json:"Local Hit Blocks"`
	LocalReadBlocks     int64 `json:"Local Read Blocks"`
	TempReadBlocks      int64 `json:"Temp Read Blocks"`
	TempWrittenBlocks   int64 `json:"Temp Written Blocks"`

	WorkersPlanned  int             `json:"Workers Planned"`
	WorkersLaunched int             `json:"Workers Launched"`
	Workers         []rawWorker     `json:"Workers"`

	HeapFetches int64 `json:"Heap Fetches"`

	Plans []json.RawMessage `json:"Plans"`

	CTEName string `json:"CTE Name"`
}

type rawWorker struct {
	Number              int      `json:"Worker Number"`
	ActualStartupTime   *float64 `json:"Actual Startup Time"`
	ActualTotalTime     *float64 `json:"Actual Total Time"`
	ActualRows          *int64   `json:"Actual Rows"`
	ActualLoops         *int64   `json:"Actual Loops"`
	SharedHitBlocks     int64    `json:"Shared Hit Blocks"`
	SharedReadBlocks    int64    `json:"Shared Read Blocks"`
}

// parsedNode is the intermediate form: typed rawNode plus the raw JSON
// bytes so we can walk extras and recurse into children.
type parsedNode struct {
	raw   rawNode
	bytes json.RawMessage
}

// decodeRawNode decodes one node's JSON into rawNode while keeping the
// original bytes around (we need them for children + extras).
func decodeRawNode(b json.RawMessage) (*parsedNode, error) {
	var rn rawNode
	if err := json.Unmarshal(b, &rn); err != nil {
		return nil, fmt.Errorf("%w: decode node: %v", ErrInvalidJSON, err)
	}
	return &parsedNode{raw: rn, bytes: b}, nil
}

// knownKeys lists every JSON key we map into typed fields; anything
// outside this set gets stashed on Node.Extra.
var knownKeys = stringSet([]string{
	"Node Type", "Parent Relationship", "Relation Name", "Alias",
	"Index Name", "Schema", "Subplan Name",
	"Startup Cost", "Total Cost", "Plan Rows", "Plan Width",
	"Actual Startup Time", "Actual Total Time", "Actual Rows", "Actual Loops",
	"Filter", "Index Cond", "Hash Cond", "Merge Cond", "Join Filter", "Recheck Cond",
	"Rows Removed by Filter", "Rows Removed by Index Recheck", "Rows Removed by Join Filter",
	"Sort Key", "Sort Method", "Sort Space Used", "Sort Space Type",
	"Group Key", "Strategy",
	"Shared Hit Blocks", "Shared Read Blocks", "Shared Dirtied Blocks", "Shared Written Blocks",
	"Local Hit Blocks", "Local Read Blocks", "Temp Read Blocks", "Temp Written Blocks",
	"Workers Planned", "Workers Launched", "Workers",
	"Heap Fetches", "Plans", "CTE Name",
})

// buildNode converts a parsedNode into a Node and recurses into its
// children. parent may be nil (root) and depth is 0 for the root.
func buildNode(p *parsedNode, parent *Node, depth int) *Node {
	n := &Node{
		Parent:       parent,
		Depth:        depth,
		RawNodeType:  p.raw.NodeType,
		NodeType:     ParseNodeType(p.raw.NodeType),
		ParentRel:    p.raw.ParentRel,
		RelationName: p.raw.RelationName,
		Alias:        p.raw.Alias,
		IndexName:    p.raw.IndexName,
		Schema:       p.raw.Schema,
		SubplanName:  p.raw.SubplanName,

		StartupCost: p.raw.StartupCost,
		TotalCost:   p.raw.TotalCost,
		PlanRows:    p.raw.PlanRows,
		PlanWidth:   p.raw.PlanWidth,

		Filter:      p.raw.Filter,
		IndexCond:   p.raw.IndexCond,
		HashCond:    p.raw.HashCond,
		MergeCond:   p.raw.MergeCond,
		JoinFilter:  p.raw.JoinFilter,
		RecheckCond: p.raw.RecheckCond,

		RowsRemovedByFilter:       p.raw.RowsRemovedByFilter,
		RowsRemovedByIndexRecheck: p.raw.RowsRemovedByIndexRecheck,
		RowsRemovedByJoinFilter:   p.raw.RowsRemovedByJoinFilter,

		SortKey:       p.raw.SortKey,
		SortMethod:    p.raw.SortMethod,
		SortSpaceKB:   p.raw.SortSpaceUsed,
		SortSpaceType: p.raw.SortSpaceType,

		GroupKey: p.raw.GroupKey,
		Strategy: p.raw.Strategy,

		SharedHitBlocks:     p.raw.SharedHitBlocks,
		SharedReadBlocks:    p.raw.SharedReadBlocks,
		SharedDirtiedBlocks: p.raw.SharedDirtiedBlocks,
		SharedWrittenBlocks: p.raw.SharedWrittenBlocks,
		LocalHitBlocks:      p.raw.LocalHitBlocks,
		LocalReadBlocks:     p.raw.LocalReadBlocks,
		TempReadBlocks:      p.raw.TempReadBlocks,
		TempWrittenBlocks:   p.raw.TempWrittenBlocks,

		WorkersPlanned:  p.raw.WorkersPlanned,
		WorkersLaunched: p.raw.WorkersLaunched,

		HeapFetches: p.raw.HeapFetches,
	}

	// "CTE Name" is a CTE-Scan-only field; keep it visible under Alias
	// when the node didn't get a more specific alias.
	if n.NodeType == NodeTypeCTEScan && n.Alias == "" && p.raw.CTEName != "" {
		n.Alias = p.raw.CTEName
	}

	// NeverExecuted detection per spec: Actual Loops == 0 OR any of the
	// Actual* numeric fields absent.
	missingAny := p.raw.ActualStartupTime == nil ||
		p.raw.ActualTotalTime == nil ||
		p.raw.ActualRows == nil ||
		p.raw.ActualLoops == nil
	if missingAny {
		n.NeverExecuted = true
	} else {
		n.ActualStartupTimeMs = *p.raw.ActualStartupTime
		n.ActualTotalTimeMs = *p.raw.ActualTotalTime
		n.ActualRows = *p.raw.ActualRows
		n.Loops = *p.raw.ActualLoops
		if n.Loops == 0 {
			n.NeverExecuted = true
			n.ActualStartupTimeMs = 0
			n.ActualTotalTimeMs = 0
			n.ActualRows = 0
			n.Loops = 0
		}
	}

	for _, rw := range p.raw.Workers {
		w := WorkerInfo{
			Number:          rw.Number,
			SharedHitBlocks: rw.SharedHitBlocks,
			SharedReadBlocks: rw.SharedReadBlocks,
		}
		if rw.ActualStartupTime != nil {
			w.ActualStartupTimeMs = *rw.ActualStartupTime
		}
		if rw.ActualTotalTime != nil {
			w.ActualTotalTimeMs = *rw.ActualTotalTime
		}
		if rw.ActualRows != nil {
			w.ActualRows = *rw.ActualRows
		}
		if rw.ActualLoops != nil {
			w.ActualLoops = *rw.ActualLoops
		}
		n.Workers = append(n.Workers, w)
	}

	// Second pass: anything on this node's JSON that we didn't map
	// above ends up on Extra. This keeps the data available for future
	// renderers without forcing us to exhaustively type every shape.
	n.Extra = collectExtras(p.bytes)

	// Recurse into children.
	for _, childBytes := range p.raw.Plans {
		childParsed, err := decodeRawNode(childBytes)
		if err != nil {
			// Malformed child — stash the raw JSON on Extra rather than
			// failing the entire parse, since partial trees are still
			// useful.
			n.Extra["Malformed Child"] = string(childBytes)
			continue
		}
		child := buildNode(childParsed, n, depth+1)
		n.Children = append(n.Children, child)
	}
	return n
}

// collectExtras returns a map of every JSON key not in knownKeys. "Plans"
// is always excluded since children are materialized separately.
func collectExtras(b json.RawMessage) map[string]any {
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(b, &generic); err != nil {
		return nil
	}
	var out map[string]any
	for k, v := range generic {
		if knownKeys[k] {
			continue
		}
		var val any
		if err := json.Unmarshal(v, &val); err != nil {
			// Keep the raw bytes as a fallback.
			val = string(v)
		}
		if out == nil {
			out = make(map[string]any, 4)
		}
		out[k] = val
	}
	return out
}

// assignIDs performs a depth-first pre-order walk assigning *next as
// each node's ID, starting at *next and incrementing after assignment.
func assignIDs(n *Node, next *int) {
	if n == nil {
		return
	}
	n.ID = *next
	*next++
	for _, c := range n.Children {
		assignIDs(c, next)
	}
}

// stringSet turns a slice into a set for O(1) membership tests.
func stringSet(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// stringMap coerces a Settings block (values may be strings, numbers,
// bools) into a map[string]string so downstream code can treat it
// uniformly.
func stringMap(m map[string]any) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		switch x := v.(type) {
		case string:
			out[k] = x
		case bool:
			if x {
				out[k] = "true"
			} else {
				out[k] = "false"
			}
		case float64:
			out[k] = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", x), "0"), ".")
		default:
			out[k] = fmt.Sprint(v)
		}
	}
	return out
}
