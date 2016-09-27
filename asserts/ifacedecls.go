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
	"fmt"
	"regexp"
	"strconv"
)

type attrMatcher interface {
	match(interface{}) error
}

// compileAttrMatcher compiles an attrMatcher derived from constraints,
// context can be "top" or "map", alternatives have no influence on it
func compileAttrMatcher(context string, constraints interface{}) (attrMatcher, error) {
	switch x := constraints.(type) {
	case map[string]interface{}:
		return compileMapAttrMatcher(x)
	case []interface{}:
		// TODO: disallow nesting alternatives directly inside alernatives?
		return compileAltAttrMatcher(context, x)
	case string:
		if context == "top" {
			return nil, fmt.Errorf("first level of non alternative constraints must be a set of key-value contraints")
		}
		return compileRegexpAttrMatcher(x)
	default:
		return nil, fmt.Errorf("constraint must be a key-value map, regexp or a list of alternative constraints: %v", x)
	}
}

type mapAttrMatcher map[string]attrMatcher

func compileMapAttrMatcher(m map[string]interface{}) (attrMatcher, error) {
	matcher := make(mapAttrMatcher)
	for k, constraint := range m {
		matcher1, err := compileAttrMatcher("map", constraint)
		if err != nil {
			return nil, fmt.Errorf("constraint for %q: %v", k, err)
		}
		matcher[k] = matcher1
	}
	return matcher, nil
}

func matchEntry(k string, matcher1 attrMatcher, v interface{}) error {
	if v == nil {
		return fmt.Errorf("%q has constraints but is unset", k)
	}
	if err := matcher1.match(v); err != nil {
		return fmt.Errorf("%q mismatch: %v", k, err)
	}
	return nil
}

func matchList(matcher attrMatcher, l []interface{}) error {
	for i, elem := range l {
		if err := matcher.match(elem); err != nil {
			return fmt.Errorf("element %d: %v", i, err)
		}
	}
	return nil
}

func (matcher mapAttrMatcher) match(v interface{}) error {
	switch x := v.(type) {
	case map[string]interface{}: // top level looks like this
		for k, matcher1 := range matcher {
			if err := matchEntry(k, matcher1, x[k]); err != nil {
				return err
			}
		}
	case map[interface{}]interface{}: // nested maps look like this
		for k, matcher1 := range matcher {
			if err := matchEntry(k, matcher1, x[k]); err != nil {
				return err
			}
		}
	case []interface{}:
		return matchList(matcher, x)
	default:
		return fmt.Errorf("cannot match key-value constraints against: %v", v)
	}
	return nil
}

type regexpAttrMatcher struct {
	*regexp.Regexp
}

func compileRegexpAttrMatcher(s string) (attrMatcher, error) {
	rx, err := regexp.Compile("^" + s + "$")
	if err != nil {
		return nil, fmt.Errorf("cannot compile %q: %v", s, err)
	}
	return regexpAttrMatcher{rx}, nil
}

func (matcher regexpAttrMatcher) match(v interface{}) error {
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case bool:
		s = strconv.FormatBool(x)
	case int:
		s = strconv.Itoa(x)
	case []interface{}:
		return matchList(matcher, x)
	default:
		return fmt.Errorf("cannot match regexp constraint against: %v", v)
	}
	if !matcher.Regexp.MatchString(s) {
		return fmt.Errorf("%q does not match %v", s, matcher.Regexp)
	}
	return nil

}

type altAttrMatcher struct {
	alts []attrMatcher
}

func compileAltAttrMatcher(context string, l []interface{}) (attrMatcher, error) {
	alts := make([]attrMatcher, len(l))
	for i, constraint := range l {
		matcher1, err := compileAttrMatcher(context, constraint)
		if err != nil {
			return nil, fmt.Errorf("alternative %d: %v", i+1, err)
		}
		alts[i] = matcher1
	}
	return altAttrMatcher{alts}, nil

}

func (matcher altAttrMatcher) match(v interface{}) error {
	var firstErr error
	for _, alt := range matcher.alts {
		err := alt.match(v)
		if err == nil {
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return fmt.Errorf("no alternative matches: %v", firstErr)
}

// AttributeConstraints implements a set of constraints on the attributes of a slot or plug.
type AttributeConstraints struct {
	matcher attrMatcher
}

// compileAttributeContraints checks and compiles a mapping or list from the assertion format into AttributeConstraints.
func compileAttributeContraints(constraints interface{}) (*AttributeConstraints, error) {
	matcher, err := compileAttrMatcher("top", constraints)
	if err != nil {
		return nil, err
	}
	return &AttributeConstraints{matcher: matcher}, nil
}

// Check checks whether attrs don't match the constraints.
func (c *AttributeConstraints) Check(attrs map[string]interface{}) error {
	return c.matcher.match(attrs)
}
