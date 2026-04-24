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
		// Regressions for the function-prefix false positive: columns
		// whose names start with recognised function words must still
		// be captured whole, not split into "prefix" + "_rest".
		{"(lower_col = 1)", []string{"lower_col"}},
		{"(upper_case_flag = true)", []string{"upper_case_flag"}},
		{"(trim_ws_enabled = false)", []string{"trim_ws_enabled"}},
		// Compound-filter coverage: AND/OR chains, dedupe on repeated
		// references, and nested conjunctions from Stage 4.5 polish.
		{"((a = 1) AND (b = 2))", []string{"a", "b"}},
		{"((a = 1) AND (b = 2) AND (c = 3))", []string{"a", "b", "c"}},
		{"((a >= 1) AND (a <= 10))", []string{"a"}},
		{"((a = 1) OR (b = 2))", []string{"a", "b"}},
		{"(((a = 1) AND (b = 2)) OR (c = 3))", []string{"a", "b", "c"}},
		{"((customer_id >= 1) AND (customer_id <= 200) AND (status = 'shipped'::text))", []string{"customer_id", "status"}},
		// LIKE / ILIKE operators (~~, ~~*, !~~, !~~*).
		{"(name ~~ 'A%'::text)", []string{"name"}},
		{"(name !~~* 'foo%'::text)", []string{"name"}},
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

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 kB"},
		{1_500, "1.5 kB"},
		{1_048_576, "1.0 MB"},
		{64_000_000, "61.0 MB"},
		{1_073_741_824, "1.0 GB"},
		{2 * 1_073_741_824, "2.0 GB"},
		{-2048, "-2.0 kB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.in); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBloatFactor(t *testing.T) {
	tests := []struct {
		name                   string
		blocks, rows, width    int64
		wantMin, wantMax       float64
	}{
		{"zero blocks", 0, 100, 100, 0, 0},
		{"zero rows", 1000, 0, 100, 0, 0},
		{"healthy scan", 10, 1000, 100, 0.5, 1.5},          // 1000 rows × 100B fits ~10 pages
		{"bloated scan", 4412, 100, 100, 100, 10_000},      // massive over-read
		{"wide rows keep factor sane", 100, 100, 8_192, 0.9, 1.1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bloatFactor(tt.blocks, tt.rows, tt.width)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("bloatFactor(%d,%d,%d) = %v, want in [%v, %v]",
					tt.blocks, tt.rows, tt.width, got, tt.wantMin, tt.wantMax)
			}
		})
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
