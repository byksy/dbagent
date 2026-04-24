package rules

import (
	"reflect"
	"strings"
	"testing"
)

func TestBitmapAndComposite(t *testing.T) {
	rule := &BitmapAndComposite{}

	t.Run("fires on 2-child BitmapAnd with composite CREATE INDEX", func(t *testing.T) {
		p := loadRuleFixture(t, "bitmap_and_composite", "positive.json")
		f := rule.Check(newContext(p))
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(f), f)
		}
		if f[0].Severity != SeverityWarning {
			t.Errorf("severity = %v, want Warning", f[0].Severity)
		}
		// Selectivity ordering: customer_id (10 rows) before status (500 rows).
		want := "CREATE INDEX ON orders (customer_id, status);"
		if f[0].Suggested != want {
			t.Errorf("suggested = %q, want %q", f[0].Suggested, want)
		}
		cols, _ := f[0].Evidence["proposed_columns"].([]string)
		if !reflect.DeepEqual(cols, []string{"customer_id", "status"}) {
			t.Errorf("proposed_columns = %v, want [customer_id status]", cols)
		}
	})

	t.Run("no finding for single-child bitmap path", func(t *testing.T) {
		p := loadRuleFixture(t, "bitmap_and_composite", "negative.json")
		if f := rule.Check(newContext(p)); len(f) != 0 {
			t.Errorf("got %d findings, want 0: %+v", len(f), f)
		}
	})

	t.Run("suggested mentions CREATE INDEX", func(t *testing.T) {
		p := loadRuleFixture(t, "bitmap_and_composite", "positive.json")
		f := rule.Check(newContext(p))
		if !strings.HasPrefix(f[0].Suggested, "CREATE INDEX") {
			t.Errorf("suggested prefix: %q", f[0].Suggested)
		}
	})
}
