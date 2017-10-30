package netlink

import (
	"regexp"
)

type Matcher interface {
	Evaluate(e UEvent) bool
	Compile() error
}

type RuleDefinition struct {
	Action *string           `json:"action,omitempty"`
	Env    map[string]string `json:"env,omitempty"`
	rule   *rule
}

// Evaluate return true if all condition match uevent and envs in rule exists in uevent
func (r RuleDefinition) Evaluate(e UEvent) bool {

	// Compile if needed
	if r.rule == nil {
		if err := r.Compile(); err != nil {
			return false
		}
	}

	// Evaluate uevent with rule
	matchAction := (r.rule.Action == nil)
	if !matchAction {
		matchAction = r.rule.Action.MatchString(e.Action.String())
	}

	foundEnv := (len(r.rule.Env) == 0)
	for envName, reg := range r.rule.Env {
		foundEnv = false
		for k, v := range e.Env {
			if k == envName {
				foundEnv = true
				if !reg.MatchString(v) {
					return false
				}
			}
		}
		if !foundEnv {
			return false
		}
	}

	return matchAction && foundEnv
}

// Compile prepare rule definition to be able to Evaluate() an UEvent
func (r *RuleDefinition) Compile() error {
	r.rule = &rule{
		Env: make(map[string]*regexp.Regexp, 0),
	}

	if r.Action != nil {
		action, err := regexp.Compile(*(r.Action))
		if err != nil {
			return err
		}
		r.rule.Action = action
	}

	for k, v := range r.Env {
		reg, err := regexp.Compile(v)
		if err != nil {
			return err
		}
		r.rule.Env[k] = reg
	}
	return nil
}

// rule is the compiled version of the RuleDefinition
type rule struct {
	Action *regexp.Regexp
	Env    map[string]*regexp.Regexp
}

// RuleDefinitions is like chained rule with OR operator
type RuleDefinitions struct {
	Rules []RuleDefinition
}

func (rs *RuleDefinitions) AddRule(r RuleDefinition) {
	rs.Rules = append(rs.Rules, r)
}

func (rs *RuleDefinitions) Compile() error {
	for _, r := range rs.Rules {
		if err := r.Compile(); err != nil {
			return err
		}
	}
	return nil
}

func (rs RuleDefinitions) Evaluate(e UEvent) bool {
	for _, r := range rs.Rules {
		if r.Evaluate(e) {
			return true
		}
	}
	return false
}
