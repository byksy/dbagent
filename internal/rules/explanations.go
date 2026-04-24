package rules

import (
	_ "embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed explanations.yaml
var explanationsRaw []byte

// Explanation holds the three text blocks for a rule. The field tags
// match explanations.yaml exactly; no other source feeds this type.
type Explanation struct {
	WhatHappened string `yaml:"what_happened" json:"what_happened"`
	WhyItMatters string `yaml:"why_it_matters" json:"why_it_matters"`
	WhatToDo     string `yaml:"what_to_do"     json:"what_to_do"`
}

// explanations is populated at init() from the embedded YAML. A
// parse failure here fails the whole binary — explanations are
// data the build depends on, so late detection isn't acceptable.
// Stored as *Explanation so LookupExplanation can return a stable
// pointer into package state instead of allocating a fresh copy on
// every call.
var explanations map[string]*Explanation

func init() {
	var raw map[string]Explanation
	if err := yaml.Unmarshal(explanationsRaw, &raw); err != nil {
		panic(fmt.Sprintf("failed to parse embedded explanations.yaml: %v", err))
	}
	explanations = make(map[string]*Explanation, len(raw))
	for k := range raw {
		e := raw[k] // avoid aliasing the loop variable's single address
		explanations[k] = &e
	}
}

// LookupExplanation returns the explanation for a rule ID, or nil if
// no entry exists. Callers should treat nil as a programming error:
// the TestExplanations_AllRulesCovered test in this package fails the
// build if a rule ships without an explanation.
func LookupExplanation(ruleID string) *Explanation {
	return explanations[ruleID]
}

// AllExplanations returns a copy of the full map (by value, so the
// Stage 9 LLM layer and tests can iterate without risking mutation
// of package state). Used sparingly — lookups should go through
// LookupExplanation.
func AllExplanations() map[string]Explanation {
	out := make(map[string]Explanation, len(explanations))
	for k, v := range explanations {
		out[k] = *v
	}
	return out
}
