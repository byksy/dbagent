package rules

import (
	"strings"
	"testing"
)

// TestExplanations_AllRulesCovered is the build-time contract: every
// rule returned by Default() must have a matching entry in
// explanations.yaml. A rule ships without an explanation only if this
// test is deleted, which is the whole point.
func TestExplanations_AllRulesCovered(t *testing.T) {
	for _, r := range Default() {
		if got := LookupExplanation(r.ID()); got == nil {
			t.Errorf("rule %q has no entry in explanations.yaml", r.ID())
		}
	}
}

// TestExplanation_NonEmpty asserts all three blocks carry substantive
// text. The 10-word floor is a tripwire for accidentally-empty
// entries; real explanations run 30+ words per block.
func TestExplanation_NonEmpty(t *testing.T) {
	const minWords = 10
	for id, e := range AllExplanations() {
		check := func(label, body string) {
			if strings.TrimSpace(body) == "" {
				t.Errorf("rule %q: %s is empty", id, label)
				return
			}
			if words := len(strings.Fields(body)); words < minWords {
				t.Errorf("rule %q: %s has %d words (< %d), looks like a stub",
					id, label, words, minWords)
			}
		}
		check("what_happened", e.WhatHappened)
		check("why_it_matters", e.WhyItMatters)
		check("what_to_do", e.WhatToDo)
	}
}

// TestExplanation_NoMarkdown guards against markdown seeping in.
// YAML renders plain text; if an author starts adding **bold** or #
// headings the CLI layer will print the raw characters. SQL snippets
// using "*" are fine — we only reject line-leading markup.
func TestExplanation_NoMarkdown(t *testing.T) {
	bad := []string{"# ", "## ", "> ", "**"}
	for id, e := range AllExplanations() {
		for _, label := range []string{"what_happened", "why_it_matters", "what_to_do"} {
			var body string
			switch label {
			case "what_happened":
				body = e.WhatHappened
			case "why_it_matters":
				body = e.WhyItMatters
			case "what_to_do":
				body = e.WhatToDo
			}
			for _, line := range strings.Split(body, "\n") {
				trimmed := strings.TrimLeft(line, " \t")
				for _, marker := range bad {
					if marker == "**" {
						// allow runs of "*" that aren't emphasis — SQL /
						// inline emphasis pairs catch both asterisks on
						// the same line.
						if strings.Count(line, "**") >= 2 {
							t.Errorf("rule %q / %s: markdown emphasis in line: %q",
								id, label, line)
							break
						}
						continue
					}
					if strings.HasPrefix(trimmed, marker) {
						t.Errorf("rule %q / %s: markdown marker %q at start of line: %q",
							id, label, marker, line)
						break
					}
				}
			}
		}
	}
}

// TestLookupExplanation_MissingReturnsNil documents the contract used
// by renderers: unknown IDs yield nil rather than an empty struct, so
// a missing explanation is easy to detect without false positives
// from zero-value strings.
func TestLookupExplanation_MissingReturnsNil(t *testing.T) {
	if got := LookupExplanation("__not_a_real_rule__"); got != nil {
		t.Errorf("expected nil for unknown rule, got %+v", got)
	}
}

// TestAllExplanations_IsCopy ensures callers can't mutate package
// state through the accessor.
func TestAllExplanations_IsCopy(t *testing.T) {
	copy1 := AllExplanations()
	const probe = "__injected__"
	copy1[probe] = Explanation{WhatHappened: "x"}
	copy2 := AllExplanations()
	if _, ok := copy2[probe]; ok {
		t.Errorf("AllExplanations must return a defensive copy")
	}
}
