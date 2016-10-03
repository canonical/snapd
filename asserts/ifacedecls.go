// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package asserts

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type attrMatcher interface {
	match(context string, v interface{}) error
}

func chain(context, k string) string {
	if context == "" {
		return k
	}
	return fmt.Sprintf("%s.%s", context, k)
}

type compileContext struct {
	dotted string
	hadMap bool
	wasAlt bool
}

func (cc compileContext) String() string {
	return cc.dotted
}

func (cc compileContext) keyEntry(k string) compileContext {
	return compileContext{
		dotted: chain(cc.dotted, k),
		hadMap: true,
		wasAlt: false,
	}
}

func (cc compileContext) alt(alt int) compileContext {
	return compileContext{
		dotted: fmt.Sprintf("%s/alt#%d/", cc.dotted, alt+1),
		hadMap: cc.hadMap,
		wasAlt: true,
	}
}

// compileAttrMatcher compiles an attrMatcher derived from constraints,
func compileAttrMatcher(cc compileContext, constraints interface{}) (attrMatcher, error) {
	switch x := constraints.(type) {
	case map[string]interface{}:
		return compileMapAttrMatcher(cc, x)
	case []interface{}:
		if cc.wasAlt {
			return nil, fmt.Errorf("cannot nest alternative constraints directly at %q", cc)
		}
		return compileAltAttrMatcher(cc, x)
	case string:
		if !cc.hadMap {
			return nil, fmt.Errorf("first level of non alternative constraints must be a set of key-value contraints")
		}
		return compileRegexpAttrMatcher(cc, x)
	default:
		return nil, fmt.Errorf("constraint %q must be a key-value map, regexp or a list of alternative constraints: %v", cc, x)
	}
}

type mapAttrMatcher map[string]attrMatcher

func compileMapAttrMatcher(cc compileContext, m map[string]interface{}) (attrMatcher, error) {
	matcher := make(mapAttrMatcher)
	for k, constraint := range m {
		matcher1, err := compileAttrMatcher(cc.keyEntry(k), constraint)
		if err != nil {
			return nil, err
		}
		matcher[k] = matcher1
	}
	return matcher, nil
}

func matchEntry(context, k string, matcher1 attrMatcher, v interface{}) error {
	context = chain(context, k)
	if v == nil {
		return fmt.Errorf("attribute %q has constraints but is unset", context)
	}
	if err := matcher1.match(context, v); err != nil {
		return err
	}
	return nil
}

func matchList(context string, matcher attrMatcher, l []interface{}) error {
	for i, elem := range l {
		if err := matcher.match(chain(context, strconv.Itoa(i)), elem); err != nil {
			return err
		}
	}
	return nil
}

func (matcher mapAttrMatcher) match(context string, v interface{}) error {
	switch x := v.(type) {
	case map[string]interface{}: // top level looks like this
		for k, matcher1 := range matcher {
			if err := matchEntry(context, k, matcher1, x[k]); err != nil {
				return err
			}
		}
	case map[interface{}]interface{}: // nested maps look like this
		for k, matcher1 := range matcher {
			if err := matchEntry(context, k, matcher1, x[k]); err != nil {
				return err
			}
		}
	case []interface{}:
		return matchList(context, matcher, x)
	default:
		return fmt.Errorf("attribute %q must be a map", context)
	}
	return nil
}

type regexpAttrMatcher struct {
	*regexp.Regexp
}

func compileRegexpAttrMatcher(cc compileContext, s string) (attrMatcher, error) {
	rx, err := regexp.Compile("^(" + s + ")$")
	if err != nil {
		return nil, fmt.Errorf("cannot compile %q constraint %q: %v", cc, s, err)
	}
	return regexpAttrMatcher{rx}, nil
}

func (matcher regexpAttrMatcher) match(context string, v interface{}) error {
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case bool:
		s = strconv.FormatBool(x)
	case int:
		s = strconv.Itoa(x)
	case []interface{}:
		return matchList(context, matcher, x)
	default:
		return fmt.Errorf("attribute %q must be a scalar or list", context)
	}
	if !matcher.Regexp.MatchString(s) {
		return fmt.Errorf("attribute %q value %q does not match %v", context, s, matcher.Regexp)
	}
	return nil

}

type altAttrMatcher struct {
	alts []attrMatcher
}

func compileAltAttrMatcher(cc compileContext, l []interface{}) (attrMatcher, error) {
	alts := make([]attrMatcher, len(l))
	for i, constraint := range l {
		matcher1, err := compileAttrMatcher(cc.alt(i), constraint)
		if err != nil {
			return nil, err
		}
		alts[i] = matcher1
	}
	return altAttrMatcher{alts}, nil

}

func (matcher altAttrMatcher) match(context string, v interface{}) error {
	var firstErr error
	for _, alt := range matcher.alts {
		err := alt.match(context, v)
		if err == nil {
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	ctxDescr := ""
	if context != "" {
		ctxDescr = fmt.Sprintf(" for attribute %q", context)
	}
	return fmt.Errorf("no alternative%s matches: %v", ctxDescr, firstErr)
}

// AttributeConstraints implements a set of constraints on the attributes of a slot or plug.
type AttributeConstraints struct {
	matcher attrMatcher
}

// compileAttributeConstraints checks and compiles a mapping or list
// from the assertion format into AttributeConstraints.
func compileAttributeConstraints(constraints interface{}) (*AttributeConstraints, error) {
	matcher, err := compileAttrMatcher(compileContext{}, constraints)
	if err != nil {
		return nil, err
	}
	return &AttributeConstraints{matcher: matcher}, nil
}

type fixedAttrMatcher struct {
	result error
}

func (matcher fixedAttrMatcher) match(context string, v interface{}) error {
	return matcher.result
}

var (
	AlwaysMatchAttributes = &AttributeConstraints{matcher: fixedAttrMatcher{nil}}
	NeverMatchAttributes  = &AttributeConstraints{matcher: fixedAttrMatcher{errors.New("not allowed")}}
)

// Check checks whether attrs don't match the constraints.
func (c *AttributeConstraints) Check(attrs map[string]interface{}) error {
	return c.matcher.match("", attrs)
}

// rules

var (
	validSnapType  = regexp.MustCompile("^(?:core|kernel|gadget|app)$")
	validSnapID    = regexp.MustCompile("^[a-z0-9A-Z]{32}$")                                        // snap-ids look like this
	validPublisher = regexp.MustCompile("^(?:[a-z0-9A-Z]{32}|[-a-z0-9]{2,28}|\\$[A-Z][A-Z0-9_]*)$") // account ids look like snap-ids or are nice identifiers, support our own special markers $MARKER

	validIDConstraints = map[string]*regexp.Regexp{
		"slot-snap-type":    validSnapType,
		"slot-snap-id":      validSnapID,
		"slot-publisher-id": validPublisher,
		"plug-snap-type":    validSnapType,
		"plug-snap-id":      validSnapID,
		"plug-publisher-id": validPublisher,
	}
)

func checkMapOrShortcut(context string, v interface{}) (m map[string]interface{}, invert bool, err error) {
	switch x := v.(type) {
	case map[string]interface{}:
		return x, false, nil
	case string:
		switch x {
		case "true":
			return nil, false, nil
		case "false":
			return nil, true, nil
		}
	}
	return nil, false, fmt.Errorf("%s must be a map or one of the shortcuts 'true' or 'false'", context)

}

type constraintsHolder interface {
	setAttributeConstraints(field string, cstrs *AttributeConstraints)
	setIDConstraints(field string, cstrs []string)
}

func baseCompileConstraints(context string, constraints interface{}, target constraintsHolder, attrConstraints, idConstraints []string) error {
	cMap, invert, err := checkMapOrShortcut(context, constraints)
	if err != nil {
		return err
	}
	if cMap == nil {
		fixed := AlwaysMatchAttributes // "true"
		if invert {                    // "false"
			fixed = NeverMatchAttributes
		}
		for _, field := range attrConstraints {
			target.setAttributeConstraints(field, fixed)
		}
		return nil
	}
	defaultUsed := 0
	for _, field := range idConstraints {
		l, err := checkStringListInMap(cMap, field, fmt.Sprintf("%s in %s", field, context), validIDConstraints[field])
		if err != nil {
			return err
		}
		if l == nil {
			defaultUsed++
		}
		target.setIDConstraints(field, l)
	}
	for _, field := range attrConstraints {
		cstrs := AlwaysMatchAttributes
		v := cMap[field]
		if v != nil {
			var err error
			cstrs, err = compileAttributeConstraints(cMap[field])
			if err != nil {
				return fmt.Errorf("cannot compile %s in %s: %v", field, context, err)
			}
		} else {
			defaultUsed++
		}
		target.setAttributeConstraints(field, cstrs)
	}
	if defaultUsed == len(attributeConstraints)+len(idConstraints) {
		return fmt.Errorf("%s must specify at least one of %s, %s", context, strings.Join(attrConstraints, ", "), strings.Join(idConstraints, ", "))
	}
	return nil
}

type rule interface {
	setConstraints(field string, cstrs constraintsHolder)
}

type subruleCompiler func(context string, subrule string, constraints interface{}) (constraintsHolder, error)

func baseCompileRule(context string, rule interface{}, target rule, subrules []string, compilers map[string]subruleCompiler, defaultOutcome, invertedOutcome map[string]interface{}) error {
	rMap, invert, err := checkMapOrShortcut(context, rule)
	if err != nil {
		return err
	}
	if rMap == nil {
		rMap = defaultOutcome // "true"
		if invert {
			rMap = invertedOutcome // "false"
		}
	}
	defaultUsed := 0
	// compile and set subrules
	for _, subrule := range subrules {
		v := rMap[subrule]
		if v == nil {
			v = defaultOutcome[subrule]
			defaultUsed++
		}
		compiler := compilers[subrule]
		if compiler == nil {
			panic(fmt.Sprintf("no compiler for %s in %s", subrule, context))
		}
		cstrs, err := compiler(context, subrule, v)
		if err != nil {
			return err
		}
		target.setConstraints(subrule, cstrs)
	}
	if defaultUsed == len(subrules) {
		return fmt.Errorf("%s must specify at least one of %s", context, strings.Join(subrules, ", "))
	}
	return nil
}

// PlugRule holds the rule of what is allowed, wrt installation and
// connection, for a plug of a specific interface for a snap.
type PlugRule struct {
	Interface string

	AllowInstallation *PlugInstallationConstraints
	DenyInstallation  *PlugInstallationConstraints

	AllowConnection *PlugConnectionConstraints
	DenyConnection  *PlugConnectionConstraints

	AllowAutoConnection *PlugConnectionConstraints
	DenyAutoConnection  *PlugConnectionConstraints
}

func (r *PlugRule) setConstraints(field string, cstrs constraintsHolder) {
	switch x := cstrs.(type) {
	case *PlugInstallationConstraints:
		switch field {
		case "allow-installation":
			r.AllowInstallation = x
			return
		case "deny-installation":
			r.DenyInstallation = x
			return
		}
	case *PlugConnectionConstraints:
		switch field {
		case "allow-connection":
			r.AllowConnection = x
			return
		case "deny-connection":
			r.DenyConnection = x
			return
		case "allow-auto-connection":
			r.AllowAutoConnection = x
			return
		case "deny-auto-connection":
			r.DenyAutoConnection = x
			return
		}
	}
	panic(fmt.Sprintf("cannot set PlugRule field %q with %T", field, cstrs))
}

// PlugInstallationConstraints specifies a set of constraints on an interface plug relevant to the installation of snap.
type PlugInstallationConstraints struct {
	PlugSnapTypes []string

	PlugAttributes *AttributeConstraints
}

func (c *PlugInstallationConstraints) setAttributeConstraints(field string, cstrs *AttributeConstraints) {
	switch field {
	case "plug-attributes":
		c.PlugAttributes = cstrs
	default:
		panic("unknown PlugInstallationConstraints field " + field)
	}
}

func (c *PlugInstallationConstraints) setIDConstraints(field string, cstrs []string) {
	switch field {
	case "plug-snap-type":
		c.PlugSnapTypes = cstrs
	default:
		panic("unknown PlugInstallationConstraints field " + field)
	}
}

func compilePlugInstallationConstraints(context, entry string, constraints interface{}) (constraintsHolder, error) {
	context = fmt.Sprintf("%s in %s", entry, context)
	plugInstCstrs := &PlugInstallationConstraints{}
	err := baseCompileConstraints(context, constraints, plugInstCstrs, []string{"plug-attributes"}, []string{"plug-snap-type"})
	if err != nil {
		return nil, err
	}
	return plugInstCstrs, nil
}

// PlugConnectionConstraints specfies a set of constraints on an
// interface plug for a snap relevant to its connection or
// auto-connection.
type PlugConnectionConstraints struct {
	SlotSnapTypes    []string
	SlotSnapIDs      []string
	SlotPublisherIDs []string

	PlugAttributes *AttributeConstraints
	SlotAttributes *AttributeConstraints
}

func (c *PlugConnectionConstraints) setAttributeConstraints(field string, cstrs *AttributeConstraints) {
	switch field {
	case "plug-attributes":
		c.PlugAttributes = cstrs
	case "slot-attributes":
		c.SlotAttributes = cstrs
	default:
		panic("unknown PlugConnectionConstraints field " + field)
	}
}

func (c *PlugConnectionConstraints) setIDConstraints(field string, cstrs []string) {
	switch field {
	case "slot-snap-type":
		c.SlotSnapTypes = cstrs
	case "slot-snap-id":
		c.SlotSnapIDs = cstrs
	case "slot-publisher-id":
		c.SlotPublisherIDs = cstrs
	default:
		panic("unknown PlugConnectionConstraints field " + field)
	}
}

var (
	attributeConstraints = []string{"plug-attributes", "slot-attributes"}
	plugIDConstraints    = []string{"slot-snap-type", "slot-publisher-id", "slot-snap-id"}
)

func compilePlugConnectionConstraints(context, entry string, constraints interface{}) (constraintsHolder, error) {
	context = fmt.Sprintf("%s in %s", entry, context)
	plugConnCstrs := &PlugConnectionConstraints{}
	err := baseCompileConstraints(context, constraints, plugConnCstrs, attributeConstraints, plugIDConstraints)
	if err != nil {
		return nil, err
	}
	return plugConnCstrs, nil
}

var (
	defaultOutcome = map[string]interface{}{
		"allow-installation":    "true",
		"allow-connection":      "true",
		"allow-auto-connection": "true",
		"deny-installation":     "false",
		"deny-connection":       "false",
		"deny-auto-connection":  "false",
	}

	invertedOutcome = map[string]interface{}{
		"allow-installation":    "false",
		"allow-connection":      "false",
		"allow-auto-connection": "false",
		"deny-installation":     "true",
		"deny-connection":       "true",
		"deny-auto-connection":  "true",
	}

	ruleSubrules = []string{"allow-installation", "deny-installation", "allow-connection", "deny-connection", "allow-auto-connection", "deny-auto-connection"}
)

var plugRuleCompilers = map[string]subruleCompiler{
	"allow-installation":    compilePlugInstallationConstraints,
	"deny-installation":     compilePlugInstallationConstraints,
	"allow-connection":      compilePlugConnectionConstraints,
	"deny-connection":       compilePlugConnectionConstraints,
	"allow-auto-connection": compilePlugConnectionConstraints,
	"deny-auto-connection":  compilePlugConnectionConstraints,
}

func compilePlugRule(interfaceName string, rule interface{}) (*PlugRule, error) {
	context := fmt.Sprintf("plug rule for interface %q", interfaceName)
	plugRule := &PlugRule{
		Interface: interfaceName,
	}
	err := baseCompileRule(context, rule, plugRule, ruleSubrules, plugRuleCompilers, defaultOutcome, invertedOutcome)
	if err != nil {
		return nil, err
	}
	return plugRule, nil
}

// SlotRule holds the rule of what is allowed, wrt installation and
// connection, for a slot of a specific interface for a snap.
type SlotRule struct {
	Interface string

	AllowInstallation *SlotInstallationConstraints
	DenyInstallation  *SlotInstallationConstraints

	AllowConnection *SlotConnectionConstraints
	DenyConnection  *SlotConnectionConstraints

	AllowAutoConnection *SlotConnectionConstraints
	DenyAutoConnection  *SlotConnectionConstraints
}

func (r *SlotRule) setConstraints(field string, cstrs constraintsHolder) {
	switch x := cstrs.(type) {
	case *SlotInstallationConstraints:
		switch field {
		case "allow-installation":
			r.AllowInstallation = x
			return
		case "deny-installation":
			r.DenyInstallation = x
			return
		}
	case *SlotConnectionConstraints:
		switch field {
		case "allow-connection":
			r.AllowConnection = x
			return
		case "deny-connection":
			r.DenyConnection = x
			return
		case "allow-auto-connection":
			r.AllowAutoConnection = x
			return
		case "deny-auto-connection":
			r.DenyAutoConnection = x
			return
		}
	}
	panic(fmt.Sprintf("cannot set SlotRule field %q with %T", field, cstrs))
}

// SlotInstallationConstraints specifies a set of constraints on an
// interface slot relevant to the installation of snap.
type SlotInstallationConstraints struct {
	SlotSnapTypes []string

	SlotAttributes *AttributeConstraints
}

func (c *SlotInstallationConstraints) setAttributeConstraints(field string, cstrs *AttributeConstraints) {
	switch field {
	case "slot-attributes":
		c.SlotAttributes = cstrs
	default:
		panic("unknown SlotInstallationConstraints field " + field)
	}
}

func (c *SlotInstallationConstraints) setIDConstraints(field string, cstrs []string) {
	switch field {
	case "slot-snap-type":
		c.SlotSnapTypes = cstrs
	default:
		panic("unknown SlotInstallationConstraints field " + field)
	}
}

func compileSlotInstallationConstraints(context, entry string, constraints interface{}) (constraintsHolder, error) {
	context = fmt.Sprintf("%s in %s", entry, context)
	slotInstCstrs := &SlotInstallationConstraints{}
	err := baseCompileConstraints(context, constraints, slotInstCstrs, []string{"slot-attributes"}, []string{"slot-snap-type"})
	if err != nil {
		return nil, err
	}
	return slotInstCstrs, nil
}

// SlotConnectionConstraints specfies a set of constraints on an
// interface slot for a snap relevant to its connection or
// auto-connection.
type SlotConnectionConstraints struct {
	PlugSnapTypes    []string
	PlugSnapIDs      []string
	PlugPublisherIDs []string

	SlotAttributes *AttributeConstraints
	PlugAttributes *AttributeConstraints
}

func (c *SlotConnectionConstraints) setAttributeConstraints(field string, cstrs *AttributeConstraints) {
	switch field {
	case "plug-attributes":
		c.PlugAttributes = cstrs
	case "slot-attributes":
		c.SlotAttributes = cstrs
	default:
		panic("unknown SlotConnectionConstraints field " + field)
	}
}

func (c *SlotConnectionConstraints) setIDConstraints(field string, cstrs []string) {
	switch field {
	case "plug-snap-type":
		c.PlugSnapTypes = cstrs
	case "plug-snap-id":
		c.PlugSnapIDs = cstrs
	case "plug-publisher-id":
		c.PlugPublisherIDs = cstrs
	default:
		panic("unknown SlotConnectionConstraints field " + field)
	}
}

var (
	slotIDConstraints = []string{"plug-snap-type", "plug-publisher-id", "plug-snap-id"}
)

func compileSlotConnectionConstraints(context, entry string, constraints interface{}) (constraintsHolder, error) {
	context = fmt.Sprintf("%s in %s", entry, context)
	slotConnCstrs := &SlotConnectionConstraints{}
	err := baseCompileConstraints(context, constraints, slotConnCstrs, attributeConstraints, slotIDConstraints)
	if err != nil {
		return nil, err
	}
	return slotConnCstrs, nil
}

var slotRuleCompilers = map[string]subruleCompiler{
	"allow-installation":    compileSlotInstallationConstraints,
	"deny-installation":     compileSlotInstallationConstraints,
	"allow-connection":      compileSlotConnectionConstraints,
	"deny-connection":       compileSlotConnectionConstraints,
	"allow-auto-connection": compileSlotConnectionConstraints,
	"deny-auto-connection":  compileSlotConnectionConstraints,
}

func compileSlotRule(interfaceName string, rule interface{}) (*SlotRule, error) {
	context := fmt.Sprintf("slot rule for interface %q", interfaceName)
	slotRule := &SlotRule{
		Interface: interfaceName,
	}
	err := baseCompileRule(context, rule, slotRule, ruleSubrules, slotRuleCompilers, defaultOutcome, invertedOutcome)
	if err != nil {
		return nil, err
	}
	return slotRule, nil
}
