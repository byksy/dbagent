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
var explanations map[string]Explanation

func init() {
	if err := yaml.Unmarshal(explanationsRaw, &explanations); err != nil {
		panic(fmt.Sprintf("failed to parse embedded explanations.yaml: %v", err))
	}
}

// LookupExplanation returns the explanation for a rule ID, or nil if
// no entry exists. Callers should treat nil as a programming error:
// the TestExplanations_AllRulesCovered test in this package fails the
// build if a rule ships without an explanation.
func LookupExplanation(ruleID string) *Explanation {
	if e, ok := explanations[ruleID]; ok {
		return &e
	}
	return nil
}

// AllExplanations returns a copy of the full map. Used by tests and
// reserved for the Stage 9 LLM layer so callers can seed prompts
// without reaching into package-private state.
func AllExplanations() map[string]Explanation {
	out := make(map[string]Explanation, len(explanations))
	for k, v := range explanations {
		out[k] = v
	}
	return out
}
