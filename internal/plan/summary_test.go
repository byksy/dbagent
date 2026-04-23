package plan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummarize_BelowThresholds(t *testing.T) {
	// Leaf with tiny misestimate (2×) and no filter: nothing should fire.
	leaf := &Node{
		ID: 2, PlanRows: 100, ActualRows: 200, Loops: 1,
		ActualTotalTimeMs: 1,
	}
	root := &Node{
		ID: 1, PlanRows: 1, ActualRows: 1, Loops: 1,
		ActualTotalTimeMs: 2, Children: []*Node{leaf},
	}
	leaf.Parent = root
	p := &Plan{Root: root, TotalTimeMs: 2, PlanningTimeMs: 0, ExecutionTimeMs: 2}
	s := Summarize(p)
	if s.SlowestNode == nil {
		t.Errorf("SlowestNode should always be set when any node executed")
	}
	if s.BiggestMisestimate != nil {
		t.Errorf("BiggestMisestimate should be nil below 10× factor")
	}
	if s.WorstFilterRatio != nil {
		t.Errorf("WorstFilterRatio should be nil when no filter removed rows")
	}
}

func TestSummarize_AboveThresholds(t *testing.T) {
	// 50× misestimate node, and a separate node with 90% filter removal.
	misestimated := &Node{
		ID: 2, PlanRows: 10, ActualRows: 500, Loops: 1,
		ActualTotalTimeMs: 5,
	}
	filtered := &Node{
		ID: 3, PlanRows: 100, ActualRows: 100, Loops: 1,
		ActualTotalTimeMs: 3,
		RowsRemovedByFilter: 900,
	}
	root := &Node{
		ID: 1, PlanRows: 600, ActualRows: 600, Loops: 1,
		ActualTotalTimeMs: 20, Children: []*Node{misestimated, filtered},
	}
	misestimated.Parent = root
	filtered.Parent = root
	p := &Plan{Root: root, TotalTimeMs: 20, ExecutionTimeMs: 20}
	s := Summarize(p)
	if s.BiggestMisestimate == nil || s.BiggestMisestimate.ID != 2 {
		t.Errorf("BiggestMisestimate wrong: %+v", s.BiggestMisestimate)
	}
	if s.WorstFilterRatio == nil || s.WorstFilterRatio.ID != 3 {
		t.Errorf("WorstFilterRatio wrong: %+v", s.WorstFilterRatio)
	}
	if s.SlowestNode == nil {
		t.Errorf("SlowestNode should be set")
	}
	if s.BiggestMisestimateID != 2 {
		t.Errorf("BiggestMisestimateID = %d, want 2", s.BiggestMisestimateID)
	}
}

func TestSummarize_SkipsNeverExecuted(t *testing.T) {
	dead := &Node{
		ID: 2, PlanRows: 10, ActualRows: 1000, Loops: 0,
		NeverExecuted: true,
	}
	root := &Node{
		ID: 1, PlanRows: 1, ActualRows: 1, Loops: 1,
		ActualTotalTimeMs: 1, Children: []*Node{dead},
	}
	dead.Parent = root
	p := &Plan{Root: root, TotalTimeMs: 1, ExecutionTimeMs: 1}
	s := Summarize(p)
	if s.BiggestMisestimate != nil {
		t.Errorf("NeverExecuted nodes must be skipped in summary")
	}
}

func TestSummarize_RealFixtures(t *testing.T) {
	entries, err := os.ReadDir(realFixtureDir)
	if err != nil {
		t.Fatalf("read fixture dir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(realFixtureDir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			p, err := ParseBytes(b)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			s := Summarize(p)
			if s == nil {
				t.Fatalf("nil summary")
			}
			if s.TotalTimeMs <= 0 {
				t.Errorf("TotalTimeMs <= 0")
			}
			if s.NodeCount == 0 {
				t.Errorf("NodeCount == 0")
			}
		})
	}
}
