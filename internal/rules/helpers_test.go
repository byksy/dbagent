package rules

import (
	"reflect"
	"testing"

	"github.com/byksy/dbagent/internal/plan"
)

func TestExtractFilterColumns(t *testing.T) {
	tests := []struct {
		filter string
		want   []string
	}{
		{"(status = 'shipped')", []string{"status"}},
		{"((status = 'shipped') AND (created_at > '2024-01-01'))", []string{"status", "created_at"}},
		{"(amount > 100)", []string{"amount"}},
		{"(id IS NOT NULL)", []string{"id"}},
		{"(lower((name)::text) = 'foo'::text)", []string{"name"}},
		{"(id = ANY ('{1,2,3}'::integer[]))", []string{"id"}},
		{"((tags @> '{urgent}'::text[]) OR (priority > 5))", []string{"tags", "priority"}},
		{"(complex_function(a, b) = 0)", []string{}},
		{"", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.filter, func(t *testing.T) {
			got := ExtractFilterColumns(tt.filter)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractFilterColumns(%q) = %v, want %v", tt.filter, got, tt.want)
			}
		})
	}
}

func TestFirstRelationName(t *testing.T) {
	// grandchild (no relation) under BitmapHeapScan on "orders" under root
	grandchild := &plan.Node{NodeType: plan.NodeTypeBitmapIndexScan}
	heap := &plan.Node{NodeType: plan.NodeTypeBitmapHeapScan, RelationName: "orders", Children: []*plan.Node{grandchild}}
	grandchild.Parent = heap
	root := &plan.Node{NodeType: plan.NodeTypeLimit, Children: []*plan.Node{heap}}
	heap.Parent = root

	if got := FirstRelationName(grandchild); got != "orders" {
		t.Errorf("FirstRelationName(bitmap index scan) = %q, want orders", got)
	}
	if got := FirstRelationName(heap); got != "orders" {
		t.Errorf("FirstRelationName(heap scan) = %q, want orders", got)
	}
	if got := FirstRelationName(root); got != "" {
		t.Errorf("FirstRelationName(root with no relation) = %q, want empty", got)
	}
	if got := FirstRelationName(nil); got != "" {
		t.Errorf("FirstRelationName(nil) = %q, want empty", got)
	}
}

func TestFormatWorkMem(t *testing.T) {
	tests := []struct {
		kb   int64
		want string
	}{
		{0, "4MB"},
		{-5, "4MB"},
		{512, "1MB"},          // 0.5MB → next pow2 MB = 1
		{1024, "1MB"},         // 1 MB exactly
		{1500, "2MB"},         // ~1.5 MB → 2
		{16 * 1024, "16MB"},   // already pow2
		{17 * 1024, "32MB"},   // not pow2 → next
		{524288, "512MB"},     // 512 MB
		{900 * 1024, "1GB"},   // rounds up to 1024MB → 1GB
		{3 * 1024 * 1024, "2GB"}, // 3GB clamp to 2GB
	}
	for _, tt := range tests {
		if got := FormatWorkMem(tt.kb); got != tt.want {
			t.Errorf("FormatWorkMem(%d) = %q, want %q", tt.kb, got, tt.want)
		}
	}
}
