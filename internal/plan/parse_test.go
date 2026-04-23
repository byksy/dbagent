package plan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realFixtureDir is relative to this test file's package directory.
const (
	realFixtureDir      = "../../testdata/plans/real"
	syntheticFixtureDir = "../../testdata/plans/synthetic"
)

func loadFile(t *testing.T, path string) *Plan {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	p, err := ParseBytes(b)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return p
}

func TestParse_Real(t *testing.T) {
	entries, err := os.ReadDir(realFixtureDir)
	if err != nil {
		t.Fatalf("read fixture dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("no real fixtures — run scripts/capture-fixtures.sh")
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			p := loadFile(t, filepath.Join(realFixtureDir, e.Name()))
			if p.Root == nil {
				t.Fatalf("root is nil")
			}
			if p.TotalTimeMs <= 0 {
				t.Errorf("expected TotalTimeMs > 0, got %v", p.TotalTimeMs)
			}
			all := p.AllNodes()
			if len(all) == 0 {
				t.Fatalf("no nodes in plan")
			}
			for _, n := range all {
				if n.ID == 0 {
					t.Errorf("node %q has zero ID", n.RawNodeType)
				}
			}
			if p.Root.ID != 1 {
				t.Errorf("root ID = %d, want 1", p.Root.ID)
			}
		})
	}
}

func TestParse_Synthetic_NeverExecuted(t *testing.T) {
	p := loadFile(t, filepath.Join(syntheticFixtureDir, "never_executed_branch.json"))
	// The second child (cold_partition) has "Actual Loops": 0.
	if len(p.Root.Children) < 2 {
		t.Fatalf("expected >=2 children, got %d", len(p.Root.Children))
	}
	cold := p.Root.Children[1]
	if !cold.NeverExecuted {
		t.Errorf("cold partition should be NeverExecuted")
	}
	if got := cold.ExclusiveTimeMs(); got != 0 {
		t.Errorf("NeverExecuted ExclusiveTimeMs = %v, want 0", got)
	}
	if got := cold.InclusiveTimeMs(); got != 0 {
		t.Errorf("NeverExecuted InclusiveTimeMs = %v, want 0", got)
	}
}

func TestParse_Synthetic_Parallel(t *testing.T) {
	p := loadFile(t, filepath.Join(syntheticFixtureDir, "parallel_seq_scan.json"))
	gather := p.Root
	if gather.WorkersPlanned != 2 {
		t.Errorf("Gather.WorkersPlanned = %d, want 2", gather.WorkersPlanned)
	}
	if gather.WorkersLaunched != 2 {
		t.Errorf("Gather.WorkersLaunched = %d, want 2", gather.WorkersLaunched)
	}
	leaf := p.Root.Children[0]
	if len(leaf.Workers) != 2 {
		t.Errorf("leaf.Workers = %d, want 2", len(leaf.Workers))
	}
}

func TestParse_Synthetic_InitPlan(t *testing.T) {
	p := loadFile(t, filepath.Join(syntheticFixtureDir, "init_plan.json"))
	if len(p.Root.Children) == 0 {
		t.Fatalf("expected InitPlan child")
	}
	init := p.Root.Children[0]
	if init.ParentRel != "InitPlan" {
		t.Errorf("ParentRel = %q, want InitPlan", init.ParentRel)
	}
	// Root's exclusive time should not have InitPlan's inclusive time
	// subtracted.
	rootIncl := p.Root.ActualTotalTimeMs * float64(p.Root.Loops)
	if p.Root.ExclusiveTimeMs() >= rootIncl-0.0001 {
		// When the only child is an InitPlan, exclusive time equals
		// inclusive time (no subtraction). Good.
	} else {
		t.Errorf("root exclusive time %.2f < inclusive %.2f — InitPlan time was wrongly subtracted",
			p.Root.ExclusiveTimeMs(), rootIncl)
	}
}

func TestParse_Synthetic_CTE(t *testing.T) {
	p := loadFile(t, filepath.Join(syntheticFixtureDir, "cte_scan_multiple_refs.json"))
	var cteScans, cteSources int
	for _, n := range p.AllNodes() {
		if n.NodeType == NodeTypeCTEScan {
			cteScans++
		}
		if n.SubplanName != "" && strings.HasPrefix(n.SubplanName, "CTE ") {
			cteSources++
		}
	}
	if cteScans != 2 {
		t.Errorf("expected 2 CTE Scan nodes, got %d", cteScans)
	}
	if cteSources != 1 {
		t.Errorf("expected 1 CTE source definition, got %d", cteSources)
	}
}

func TestParse_Synthetic_UnknownNodeType(t *testing.T) {
	p := loadFile(t, filepath.Join(syntheticFixtureDir, "unknown_node_type.json"))
	if p.Root.NodeType != NodeTypeUnknown {
		t.Errorf("NodeType = %v, want NodeTypeUnknown", p.Root.NodeType)
	}
	if p.Root.RawNodeType != "Imaginary Scan" {
		t.Errorf("RawNodeType = %q, want Imaginary Scan", p.Root.RawNodeType)
	}
}

func TestParse_Errors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"empty", "", ErrEmptyPlan},
		{"whitespace only", "   \n\t  ", ErrEmptyPlan},
		{"text format", "QUERY PLAN\n-----------\nSeq Scan on t", ErrUnsupportedFormat},
		{"bare word", "hello", ErrUnsupportedFormat},
		{"malformed json array", "[{bad}]", ErrInvalidJSON},
		{"empty array", "[]", ErrInvalidJSON},
		{"missing Plan key", `[{"Planning Time": 1.0}]`, ErrInvalidJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseBytes([]byte(tt.input))
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("got %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestParse_BOMStripped(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(realFixtureDir, "simple_seq_scan.json"))
	if err != nil {
		t.Fatal(err)
	}
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, b...)
	if _, err := ParseBytes(withBOM); err != nil {
		t.Errorf("parse with BOM failed: %v", err)
	}
}

func TestParse_QuoteWrapped(t *testing.T) {
	p := loadFile(t, filepath.Join(syntheticFixtureDir, "missing_buffers.json"))
	_ = p
	// pgAdmin-style paste: double-quote wrapping with doubled inner quotes.
	b, _ := os.ReadFile(filepath.Join(syntheticFixtureDir, "missing_buffers.json"))
	wrapped := []byte(`"` + strings.ReplaceAll(string(b), `"`, `""`) + `"`)
	if _, err := ParseBytes(wrapped); err != nil {
		t.Errorf("quote-wrapped parse failed: %v", err)
	}
}

func TestParseNodeType(t *testing.T) {
	tests := []struct {
		in   string
		want NodeType
	}{
		{"Seq Scan", NodeTypeSeqScan},
		{"Index Scan", NodeTypeIndexScan},
		{"Hash Join", NodeTypeHashJoin},
		{"Aggregate", NodeTypeAggregate},
		{"HashAggregate", NodeTypeAggregate},
		{"GroupAggregate", NodeTypeAggregate},
		{"MixedAggregate", NodeTypeAggregate},
		{"Gather", NodeTypeGather},
		{"Gather Merge", NodeTypeGatherMerge},
		{"CTE Scan", NodeTypeCTEScan},
		{"Imaginary Scan", NodeTypeUnknown},
		{"", NodeTypeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := ParseNodeType(tt.in); got != tt.want {
				t.Errorf("ParseNodeType(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestExtra_PopulatedForUnknownKeys(t *testing.T) {
	p := loadFile(t, filepath.Join(realFixtureDir, "hash_join_with_filter.json"))
	// "Hash Buckets" is not in our typed model — should show up in Extra
	// on the Hash node somewhere.
	found := false
	p.Root.Walk(func(n *Node) {
		if _, ok := n.Extra["Hash Buckets"]; ok {
			found = true
		}
	})
	if !found {
		t.Errorf("expected some node to have Extra[\"Hash Buckets\"]")
	}
}
