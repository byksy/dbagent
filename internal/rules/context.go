package rules

import (
	"github.com/byksy/dbagent/internal/plan"
	"github.com/byksy/dbagent/internal/schema"
)

// RuleContext bundles every input a rule might need. Schema is
// optional — it's nil when analysis runs offline without a loaded
// schema file and without a reachable database. Rules that don't
// need schema simply ignore the field; rules that rely on it must
// guard with a nil check (see missing_index_on_filter for the
// pattern).
type RuleContext struct {
	Plan   *plan.Plan
	Schema *schema.Schema
}

// newContext is a convenience for tests that want to construct a
// RuleContext from a plan without schema.
func newContext(p *plan.Plan) *RuleContext {
	return &RuleContext{Plan: p}
}
