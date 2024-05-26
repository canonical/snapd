package netlink

import (
	"fmt"
	"regexp"

	"github.com/ddkwork/golibrary/mylog"
)

type Matcher interface {
	Evaluate(e UEvent) bool
	EvaluateAction(a KObjAction) bool
	EvaluateEnv(e map[string]string) bool
	Compile() error
	String() string
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
		mylog.Check(r.Compile())
	}

	return r.EvaluateAction(e.Action) && r.EvaluateEnv(e.Env)
}

// EvaluateAction return true if the action match
func (r RuleDefinition) EvaluateAction(a KObjAction) (match bool) {
	// Compile if needed
	if r.rule == nil {
		mylog.Check(r.Compile())
	}
	if match = (r.rule.Action == nil); !match {
		match = r.rule.Action.MatchString(a.String())
	}
	return
}

// EvaluateEnv return true if all env match and exists
func (r RuleDefinition) EvaluateEnv(e map[string]string) bool {
	// Compile if needed
	if r.rule == nil {
		mylog.Check(r.Compile())
	}
	return r.rule.Env.Evaluate(e)
}

// Compile prepare rule definition to be able to Evaluate() an UEvent
func (r *RuleDefinition) Compile() error {
	r.rule = &rule{
		Env: make(map[string]*regexp.Regexp, 0),
	}

	if r.Action != nil {
		action := mylog.Check2(regexp.Compile(*(r.Action)))

		r.rule.Action = action
	}

	for k, v := range r.Env {
		reg := mylog.Check2(regexp.Compile(v))

		r.rule.Env[k] = reg
	}
	return nil
}

func (r RuleDefinition) String() string {
	return fmt.Sprintf("Action: %v / Env: %+v", r.Action, r.Env)
}

// rule is the compiled version of the RuleDefinition
type rule struct {
	Action *regexp.Regexp
	Env    Env
}

type Env map[string]*regexp.Regexp

func (e Env) Evaluate(env map[string]string) bool {
	foundEnv := (len(e) == 0)
	for envName, reg := range e {
		foundEnv = false
		for k, v := range env {
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

	return foundEnv
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
		mylog.Check(r.Compile())
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

// EvaluateAction return true if the action match
func (rs RuleDefinitions) EvaluateAction(a KObjAction) (match bool) {
	for _, r := range rs.Rules {
		if r.EvaluateAction(a) {
			return true
		}
	}
	return false
}

// EvaluateEnv return true if almost one env match all regexp
func (rs RuleDefinitions) EvaluateEnv(e map[string]string) bool {
	for _, r := range rs.Rules {
		if r.EvaluateEnv(e) {
			return true
		}
	}
	return false
}

func (rs RuleDefinitions) String() string {
	output := ""
	for _, v := range rs.Rules {
		output += "- " + v.String() + "\n"
	}
	return output
}
