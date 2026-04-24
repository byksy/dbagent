// Package rules runs diagnostic and prescriptive checks against a
// parsed plan. Each rule is a pure function of the plan: given a
// *plan.Plan it returns zero or more Findings. Rules never perform
// I/O and hold no state — this matters for testability and for
// future parallel execution.
package rules

import (
	"fmt"
	"sort"
	"strings"
)

// Severity expresses how urgent a finding is. Ordering is ascending
// (Info < Warning < Critical) so numeric comparisons on the enum
// work for "at or above" threshold checks.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarning
	SeverityCritical
)

// String returns the canonical lower-case name.
func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityCritical:
		return "critical"
	}
	return "unknown"
}

// ParseSeverity parses a case-insensitive severity string.
func ParseSeverity(s string) (Severity, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "info":
		return SeverityInfo, nil
	case "warning", "warn":
		return SeverityWarning, nil
	case "critical", "crit":
		return SeverityCritical, nil
	}
	return 0, fmt.Errorf("invalid severity %q, expected one of info|warning|critical", s)
}

// Category groups rules by intent. Diagnostic rules describe what's
// wrong without prescribing a fix; Prescriptive rules propose a
// specific SQL or configuration change.
type Category int

const (
	CategoryDiagnostic Category = iota
	CategoryPrescriptive
)

// String returns the canonical lower-case name.
func (c Category) String() string {
	switch c {
	case CategoryDiagnostic:
		return "diagnostic"
	case CategoryPrescriptive:
		return "prescriptive"
	}
	return "unknown"
}

// Rule is implemented by every check in this package.
type Rule interface {
	// ID is a stable, machine-readable identifier (lower_snake_case).
	ID() string
	// Name is a short human-readable label.
	Name() string
	// Category returns diagnostic or prescriptive.
	Category() Category
	// Check inspects the plan (and, when available, schema) via the
	// RuleContext and returns zero or more findings. Findings for
	// NeverExecuted nodes must not be emitted.
	Check(ctx *RuleContext) []Finding
}

// Finding is a single rule's output for one node (or the plan as a
// whole, when NodeID = 0).
type Finding struct {
	RuleID        string         `json:"rule_id"`
	RuleName      string         `json:"rule_name"`
	Severity      Severity       `json:"-"`
	SeverityName  string         `json:"severity"`
	Category      Category       `json:"-"`
	CategoryName  string         `json:"category"`
	NodeID        int            `json:"node_id"`
	Message       string         `json:"message"`
	Evidence      map[string]any `json:"evidence,omitempty"`
	Suggested     string         `json:"suggested,omitempty"`
	SuggestedMeta map[string]any `json:"suggested_meta,omitempty"`
}

// newFinding centralises Finding construction so all rules emit the
// same shape, including the string projections used by the JSON
// renderer.
func newFinding(r Rule, nodeID int, sev Severity, msg string, evidence map[string]any) Finding {
	return Finding{
		RuleID:       r.ID(),
		RuleName:     r.Name(),
		Severity:     sev,
		SeverityName: sev.String(),
		Category:     r.Category(),
		CategoryName: r.Category().String(),
		NodeID:       nodeID,
		Message:      msg,
		Evidence:     evidence,
	}
}

// Default returns the standard rule set. Registration is explicit
// (not init()-time) so tests can pass a subset to Run.
func Default() []Rule {
	return []Rule{
		// Stage 3 — plan-only diagnostic
		&HotNode{},
		&RowMisestimate{},
		&FilterRemovalRatio{},
		// Stage 3 — plan-only prescriptive
		&MissingIndexOnFilter{},
		&BitmapAndComposite{},
		&SortSpilled{},
		&PlanningVsExecution{},
		&WorkerShortage{},
		// Stage 4 — schema-aware
		&FKMissingIndex{},
		// Stage 5 — plan-only
		&CTECartesianProduct{},
		&NetworkOverhead{},
		&RedundantAggregation{},
		&MemoizeOpportunity{},
		// Stage 5 — schema-aware
		&UnusedIndexHint{},
		&DuplicateIndex{},
		&CompositeIndexExtension{},
		&TableBloat{},
	}
}

// Run executes every rule against ctx and returns their findings
// concatenated, sorted deterministically by (severity desc, NodeID
// asc, RuleID asc). Stable ordering matters for golden tests and for
// users diffing JSON output. A nil ctx is treated as "no plan, no
// schema" — no rule can fire and the result is nil.
func Run(ctx *RuleContext, rs []Rule) []Finding {
	if ctx == nil {
		return nil
	}
	var out []Finding
	for _, r := range rs {
		out = append(out, r.Check(ctx)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity > out[j].Severity
		}
		if out[i].NodeID != out[j].NodeID {
			return out[i].NodeID < out[j].NodeID
		}
		return out[i].RuleID < out[j].RuleID
	})
	return out
}
