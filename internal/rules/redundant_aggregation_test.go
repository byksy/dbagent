package rules

import "testing"

func TestRedundantAggregation(t *testing.T) {
	rule := &RedundantAggregation{}

	t.Run("fires on HashAggregate over matching Sort", func(t *testing.T) {
		p := loadRuleFixture(t, "redundant_aggregation", "positive.json")
		f := rule.Check(newContext(p))
		if len(f) != 1 {
			t.Fatalf("got %d findings, want 1: %+v", len(f), f)
		}
		if f[0].Severity != SeverityInfo {
			t.Errorf("severity = %v, want Info", f[0].Severity)
		}
		if f[0].Evidence["sort_feeds_group_key"] != true {
			t.Errorf("evidence missing sort_feeds_group_key flag")
		}
	})

	t.Run("silent on plain aggregate", func(t *testing.T) {
		p := loadRuleFixture(t, "redundant_aggregation", "negative.json")
		if f := rule.Check(newContext(p)); len(f) != 0 {
			t.Errorf("expected 0 findings, got %d: %+v", len(f), f)
		}
	})
}

func TestSortKeyColumns(t *testing.T) {
	tests := []struct {
		in   []string
		want []string
	}{
		{[]string{"customer_id"}, []string{"customer_id"}},
		{[]string{"customer_id DESC"}, []string{"customer_id"}},
		{[]string{"customer_id ASC"}, []string{"customer_id"}},
		{[]string{"customer_id DESC NULLS LAST"}, []string{"customer_id"}},
		{[]string{"customer_id NULLS FIRST"}, []string{"customer_id"}},
		{[]string{"customer_id DESC", "created_at"}, []string{"customer_id", "created_at"}},
	}
	for _, tt := range tests {
		got := sortKeyColumns(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("sortKeyColumns(%v) = %v, want %v", tt.in, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("sortKeyColumns(%v)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
			}
		}
	}
}

func TestPrefixCovers(t *testing.T) {
	if !prefixCovers([]string{"a", "b", "c"}, []string{"a", "b"}) {
		t.Errorf("(a,b,c) should cover (a,b)")
	}
	if prefixCovers([]string{"a", "b"}, []string{"a", "b", "c"}) {
		t.Errorf("(a,b) should not cover (a,b,c)")
	}
	if prefixCovers([]string{"b", "a"}, []string{"a"}) {
		t.Errorf("(b,a) should not cover (a)")
	}
}
