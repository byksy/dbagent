package rules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/byksy/dbagent/internal/plan"
)

// ruleFixtureDir is the root for rule fixtures, relative to this
// package's directory.
const ruleFixtureDir = "../../testdata/plans/rules"

// loadRuleFixture reads and parses a plan fixture under
// testdata/plans/rules/<rule>/<file>.
func loadRuleFixture(t *testing.T, rule, file string) *plan.Plan {
	t.Helper()
	path := filepath.Join(ruleFixtureDir, rule, file)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	p, err := plan.ParseBytes(b)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return p
}
