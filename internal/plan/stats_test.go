package plan

import (
	"math"
	"testing"
)

func TestExclusiveTime_SimpleChain(t *testing.T) {
	leaf := &Node{ActualTotalTimeMs: 5, Loops: 1}
	mid := &Node{ActualTotalTimeMs: 10, Loops: 1, Children: []*Node{leaf}}
	leaf.Parent = mid
	root := &Node{ActualTotalTimeMs: 20, Loops: 1, Children: []*Node{mid}}
	mid.Parent = root

	if got := root.ExclusiveTimeMs(); !closeEnough(got, 10) {
		t.Errorf("root exclusive = %v, want 10", got)
	}
	if got := mid.ExclusiveTimeMs(); !closeEnough(got, 5) {
		t.Errorf("mid exclusive = %v, want 5", got)
	}
	if got := leaf.ExclusiveTimeMs(); !closeEnough(got, 5) {
		t.Errorf("leaf exclusive = %v, want 5", got)
	}
}

func TestExclusiveTime_WithLoops(t *testing.T) {
	leaf := &Node{ActualTotalTimeMs: 1, Loops: 50}
	root := &Node{ActualTotalTimeMs: 60, Loops: 1, Children: []*Node{leaf}}
	leaf.Parent = root

	// Leaf inclusive = 1 * 50 = 50. Root inclusive = 60. Root exclusive = 10.
	if got := root.ExclusiveTimeMs(); !closeEnough(got, 10) {
		t.Errorf("root exclusive = %v, want 10", got)
	}
}

func TestExclusiveTime_InitPlan_NotSubtracted(t *testing.T) {
	initChild := &Node{
		ActualTotalTimeMs: 5,
		Loops:             1,
		ParentRel:         "InitPlan",
		SubplanName:       "InitPlan 1 (returns $0)",
	}
	regular := &Node{ActualTotalTimeMs: 2, Loops: 1, ParentRel: "Outer"}
	root := &Node{
		ActualTotalTimeMs: 10,
		Loops:             1,
		Children:          []*Node{initChild, regular},
	}
	initChild.Parent = root
	regular.Parent = root

	// 10 - (regular only) = 8; initChild's 5ms must NOT be subtracted.
	if got := root.ExclusiveTimeMs(); !closeEnough(got, 8) {
		t.Errorf("root exclusive = %v, want 8 (InitPlan time must NOT be subtracted)", got)
	}
}

func TestExclusiveTime_NeverExecuted_ReturnsZero(t *testing.T) {
	n := &Node{ActualTotalTimeMs: 99, Loops: 0, NeverExecuted: true}
	if got := n.ExclusiveTimeMs(); got != 0 {
		t.Errorf("NeverExecuted exclusive = %v, want 0", got)
	}
}

func TestMisestimateFactor(t *testing.T) {
	tests := []struct {
		name   string
		planned, actual int64
		want   float64
	}{
		{"equal", 100, 100, 1},
		{"2x under", 100, 200, 2},
		{"100x over", 10000, 100, 100},
		{"planned zero", 0, 100, 0},
		{"actual zero", 100, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := &Node{PlanRows: tt.planned, ActualRows: tt.actual, Loops: 1}
			if got := n.MisestimateFactor(); !closeEnough(got, tt.want) {
				t.Errorf("factor = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMisestimateDirection(t *testing.T) {
	tests := []struct {
		name   string
		planned, actual int64
		want   int
	}{
		{"under", 100, 500, +1},
		{"over", 500, 100, -1},
		{"equal", 100, 100, 0},
		{"zero planned", 0, 100, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := &Node{PlanRows: tt.planned, ActualRows: tt.actual, Loops: 1}
			if got := n.MisestimateDirection(); got != tt.want {
				t.Errorf("direction = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCacheHitRatio(t *testing.T) {
	tests := []struct {
		name string
		hit, read int64
		want float64
	}{
		{"all hit", 100, 0, 1.0},
		{"all miss", 0, 100, 0.0},
		{"typical", 98, 2, 0.98},
		{"both zero returns -1", 0, 0, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := &Node{SharedHitBlocks: tt.hit, SharedReadBlocks: tt.read}
			if got := n.CacheHitRatio(); !closeEnough(got, tt.want) {
				t.Errorf("ratio = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilterRemovalRatio(t *testing.T) {
	tests := []struct {
		name string
		rows, removed int64
		want float64
	}{
		{"50 kept 50 removed", 50, 50, 0.5},
		{"all kept", 100, 0, 0},
		{"all removed", 0, 100, 1.0},
		{"no data", 0, 0, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := &Node{ActualRows: tt.rows, RowsRemovedByFilter: tt.removed, Loops: 1}
			if got := n.FilterRemovalRatio(); !closeEnough(got, tt.want) {
				t.Errorf("ratio = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestActualRowsTotal(t *testing.T) {
	tests := []struct {
		name   string
		rows   int64
		loops  int64
		want   int64
	}{
		{"single loop", 42, 1, 42},
		{"nested loop inner", 10, 1000, 10000},
		{"parallel scan loops encode workers", 10000, 3, 30000},
		{"gather already-aggregated output", 30000, 1, 30000},
		{"never-executed handled upstream returns 0 via zero loops", 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := &Node{ActualRows: tt.rows, Loops: tt.loops}
			if got := n.ActualRowsTotal(); got != tt.want {
				t.Errorf("ActualRowsTotal = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMisestimate_PerLoop_NotInflatedByLoops(t *testing.T) {
	// Inner side of a nested loop: planner correctly estimated 10
	// rows per invocation, and that's exactly what happened — 1000
	// times. Comparing per-loop figures should yield no misestimate.
	n := &Node{PlanRows: 10, ActualRows: 10, Loops: 1000}
	if got := n.MisestimateFactor(); got != 0 && got > 1.001 {
		t.Errorf("inner nested-loop leaf factor = %v, want ~1 (per-loop comparison)", got)
	}
	if got := n.MisestimateDirection(); got != 0 {
		t.Errorf("direction = %d, want 0 for equal planned/actual", got)
	}
}

func TestWalk_DepthFirstPreOrder(t *testing.T) {
	// Build: root → [a → [a1], b]
	a1 := &Node{ID: 3}
	a := &Node{ID: 2, Children: []*Node{a1}}
	a1.Parent = a
	b := &Node{ID: 4}
	root := &Node{ID: 1, Children: []*Node{a, b}}
	a.Parent = root
	b.Parent = root

	var got []int
	root.Walk(func(n *Node) { got = append(got, n.ID) })
	want := []int{1, 2, 3, 4}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func closeEnough(a, b float64) bool {
	return math.Abs(a-b) < 0.0001
}
