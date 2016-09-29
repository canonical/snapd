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
	rx, err := regexp.Compile("^" + s + "$")
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

// compileAttributeConstraints checks and compiles a mapping or list from the assertion format into AttributeConstraints.
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

func checkMapOrShortcut(context string, v interface{}) (m map[string]interface{}, outcomeShortcut string, err error) {
	switch x := v.(type) {
	case map[string]interface{}:
		return x, "", nil
	case string:
		switch x {
		case "true":
			return nil, "true", nil
		case "false":
			return nil, "false", nil
		}
	}
	return nil, "", fmt.Errorf("%s must be a map or one of the shortcuts 'true' or 'false'", context)

}

// PlugRule holds the rule of what is allowed, wrt installation and connection, for a plug of a specific interface for a snap.
type PlugRule struct {
	Interface string

	AllowInstallation *PlugInstallationConstraints
	DenyInstallation  *PlugInstallationConstraints

	AllowConnection *PlugConnectionConstraints
	DenyConnection  *PlugConnectionConstraints

	AllowAutoConnection *PlugConnectionConstraints
	DenyAutoConnection  *PlugConnectionConstraints
}

func (r *PlugRule) setPlugInstallationConstraints(field string, cstrs *PlugInstallationConstraints) {
	switch field {
	case "allow-installation":
		r.AllowInstallation = cstrs
	case "deny-installation":
		r.DenyInstallation = cstrs
	default:
		panic("unknown PlugRule field " + field)
	}
}

func (r *PlugRule) setPlugConnectionConstraints(field string, cstrs *PlugConnectionConstraints) {
	switch field {
	case "allow-connection":
		r.AllowConnection = cstrs
	case "deny-connection":
		r.DenyConnection = cstrs
	case "allow-auto-connection":
		r.AllowAutoConnection = cstrs
	case "deny-auto-connection":
		r.DenyAutoConnection = cstrs
	default:
		panic("unknown PlugRule field " + field)
	}
}

// PlugInstallationConstraints specifies a set of constraints on an interface plug relevant to the installation of snap.
type PlugInstallationConstraints struct {
	PlugAttributes *AttributeConstraints
}

func compilePlugInstallationConstraints(interfaceName string, entry string, constraints interface{}) (*PlugInstallationConstraints, error) {
	context := fmt.Sprintf("%s in plug rule for interface %q", entry, interfaceName)
	cMap, shortcut, err := checkMapOrShortcut(context, constraints)
	if err != nil {
		return nil, err
	}
	if cMap == nil {
		if shortcut == "true" {
			return &PlugInstallationConstraints{PlugAttributes: AlwaysMatchAttributes}, nil
		}
		// "false"
		return &PlugInstallationConstraints{PlugAttributes: NeverMatchAttributes}, nil
	}
	attrs, err := compileAttributeConstraints(cMap["plug-attributes"])
	if err != nil {
		return nil, fmt.Errorf("cannot compile plug-attributes in %s: %v", context, err)
	}
	return &PlugInstallationConstraints{PlugAttributes: attrs}, nil
}

// PlugConnectionConstraints specfies a set of constraints on an interface plug for a snap relevant to its connection or auto-connection.
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

	validPlugIDConstraints = map[string]*regexp.Regexp{
		"slot-snap-type":    regexp.MustCompile("^os|kernel|gadget|app$"),
		"slot-snap-id":      regexp.MustCompile("^[a-z0-9A-Z]{32}$"),                                             // snap-ids look like this
		"slot-publisher-id": regexp.MustCompile("^(?:[a-z0-9A-Z]{32}|[-a-z0-9]{2,28}|\\$[a-z](?:-?[a-z0-9])*)$"), // account ids look like snap-ids or are nice identifiers, support our own special markers $MARKER
	}
)

func compilePlugConnectionConstraints(interfaceName string, entry string, constraints interface{}) (*PlugConnectionConstraints, error) {
	context := fmt.Sprintf("%s in plug rule for interface %q", entry, interfaceName)
	cMap, shortcut, err := checkMapOrShortcut(context, constraints)
	if err != nil {
		return nil, err
	}
	if cMap == nil {
		if shortcut == "true" {
			return &PlugConnectionConstraints{PlugAttributes: AlwaysMatchAttributes, SlotAttributes: AlwaysMatchAttributes}, nil
		}
		// "false"
		return &PlugConnectionConstraints{PlugAttributes: NeverMatchAttributes, SlotAttributes: NeverMatchAttributes}, nil
	}
	plugConnCstrs := &PlugConnectionConstraints{}
	defaultUsed := 0
	for _, field := range plugIDConstraints {
		l, err := checkStringListInMap(cMap, field, fmt.Sprintf("%s in %s", field, context), validPlugIDConstraints[field])
		if err != nil {
			return nil, err
		}
		if l == nil {
			defaultUsed++
		}
		plugConnCstrs.setIDConstraints(field, l)
	}
	for _, field := range attributeConstraints {
		cstrs := AlwaysMatchAttributes
		v := cMap[field]
		if v != nil {
			var err error
			cstrs, err = compileAttributeConstraints(cMap[field])
			if err != nil {
				return nil, fmt.Errorf("cannot compile %s in %s: %v", field, context, err)
			}
		} else {
			defaultUsed++
		}
		plugConnCstrs.setAttributeConstraints(field, cstrs)
	}
	if defaultUsed == len(attributeConstraints)+len(plugIDConstraints) {
		return nil, fmt.Errorf("%s must specify at least one of %s, %s", context, strings.Join(attributeConstraints, ", "), strings.Join(plugIDConstraints, ", "))
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

	installationSubrules = []string{"allow-installation", "deny-installation"}
	connectionsSubrules  = []string{"allow-connection", "deny-connection", "allow-auto-connection", "deny-auto-connection"}
)

func compilePlugRule(interfaceName string, rule interface{}) (*PlugRule, error) {
	context := fmt.Sprintf("plug rule for interface %q", interfaceName)
	rMap, shortcut, err := checkMapOrShortcut(context, rule)
	if err != nil {
		return nil, err
	}
	if rMap == nil {
		if shortcut == "true" {
			rMap = defaultOutcome
		} else { // "false"
			rMap = invertedOutcome
		}
	}
	plugRule := &PlugRule{
		Interface: interfaceName,
	}
	defaultUsed := 0
	// installation subrules
	for _, subrule := range installationSubrules {
		v := rMap[subrule]
		if v == nil {
			v = defaultOutcome[subrule]
			defaultUsed++
		}
		cstrs, err := compilePlugInstallationConstraints(interfaceName, subrule, v)
		if err != nil {
			return nil, err
		}
		plugRule.setPlugInstallationConstraints(subrule, cstrs)
	}
	// connection subrules
	for _, subrule := range connectionsSubrules {
		v := rMap[subrule]
		if v == nil {
			v = defaultOutcome[subrule]
			defaultUsed++
		}
		cstrs, err := compilePlugConnectionConstraints(interfaceName, subrule, v)
		if err != nil {
			return nil, err
		}
		plugRule.setPlugConnectionConstraints(subrule, cstrs)
	}
	if defaultUsed == len(installationSubrules)+len(connectionsSubrules) {
		return nil, fmt.Errorf("%s must specify at least one of %s, %s", context, strings.Join(installationSubrules, ", "), strings.Join(connectionsSubrules, ", "))
	}
	return plugRule, nil
}
