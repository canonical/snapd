// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package builtin

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// CompatRange is a range of unsigned integers, with Min <= Max.
type CompatRange struct {
	Min uint
	Max uint
}

// CompatDimension represents a <tag>[-<integer|integer range>] (dimension) in a compatibility field.
type CompatDimension struct {
	// Tag is the dimension identifier
	Tag string
	// Values is the list of integer/integer ranges. Only the last range
	// can have Min!=Max.
	Values []CompatRange
}

type CompatField struct {
	Dimensions []CompatDimension
}

const (
	maxDimensions = 3
	maxIntegers   = 3
)

var (
	tagRegexp = regexp.MustCompile(`^[a-z][a-z0-9]{0,31}$`)
	// 0 or a number that starts with 1-9 with a maximum of 8 digits
	intRegexp      = regexp.MustCompile(`^(0|[1-9][0-9]{0,7})$`)
	intRangeRegexp = regexp.MustCompile(`^\((0|[1-9][0-9]{0,7})\.\.(0|[1-9][0-9]{0,7})\)$`)
)

func isRealCompatRange(cr CompatRange) bool {
	return cr.Min < cr.Max
}

func appendDimension(dimensions []CompatDimension, dimension *CompatDimension) ([]CompatDimension, error) {
	if dimension != nil {
		for _, d := range dimensions {
			if d.Tag == dimension.Tag {
				return nil, fmt.Errorf("dimension %q appears more than once",
					dimension.Tag)
			}
		}
		if len(dimension.Values) == 0 {
			// If we have just a tag, that is equivalent to <tag>-0
			dimension.Values = []CompatRange{{0, 0}}
		}
		dimensions = append(dimensions, *dimension)
	}
	return dimensions, nil
}

func appendRange(ranges []CompatRange, rg CompatRange) ([]CompatRange, error) {
	numVal := len(ranges)
	if numVal > 0 && isRealCompatRange(ranges[numVal-1]) {
		return nil, fmt.Errorf("ranges only allowed at the end of compatibility field")
	}
	if numVal == maxIntegers {
		return nil, fmt.Errorf("only %d integer/integer ranges allowed per dimension in compatibility field", maxIntegers)
	}
	return append(ranges, rg), nil
}

func parseIntegerRange(token, compat string) (rg *CompatRange, err error) {
	if intRegexp.MatchString(token) {
		// 8 decimals fit 32 bits
		min, err := strconv.ParseUint(token, 10, 32)
		if err != nil {
			return nil, err
		}
		// Integers are stored as ranges where min==max
		rg = &CompatRange{uint(min), uint(min)}
	} else {
		matches := intRangeRegexp.FindStringSubmatch(token)
		// Not a tag, integer or integer range
		if len(matches) == 0 {
			return nil, fmt.Errorf("invalid tag %q in compatibility field %q",
				token, compat)
		}
		// 8 decimals fit 32 bits
		min, err := strconv.ParseUint(matches[1], 10, 32)
		if err != nil {
			return nil, err
		}
		max, err := strconv.ParseUint(matches[2], 10, 32)
		if err != nil {
			return nil, err
		}
		if min > max {
			return nil, fmt.Errorf("invalid range %q in compatibility field %q",
				token, compat)
		}
		rg = &CompatRange{uint(min), uint(max)}
	}
	return rg, nil
}

// decodeCompatField decodes a compatibility string, which is a sequence of
// dimensions separated by dashes. Each dimension consist of an alphanumerical
// descriptor followed by a dash-separated series of integer or integer ranges.
func decodeCompatField(compat string) (*CompatField, error) {
	tokens := strings.Split(compat, "-")
	dimensions := []CompatDimension{}
	var currentDimension *CompatDimension
	var err error
	for _, t := range tokens {
		if tagRegexp.MatchString(t) {
			if len(dimensions)+1 >= maxDimensions {
				return nil, fmt.Errorf("only %d dimensions allowed in compatibility field: %q",
					maxDimensions, compat)
			}
			dimensions, err = appendDimension(dimensions, currentDimension)
			if err != nil {
				return nil, err
			}
			currentDimension = &CompatDimension{Tag: t}
			continue
		}
		if currentDimension == nil {
			return nil, fmt.Errorf("bad dimension descriptor %q in compatibility field %q",
				t, compat)
		}

		rg, err := parseIntegerRange(t, compat)
		if err != nil {
			return nil, err
		}
		currentDimension.Values, err = appendRange(currentDimension.Values, *rg)
		if err != nil {
			return nil, err
		}
	}

	// Add last dimension found
	dimensions, err = appendDimension(dimensions, currentDimension)
	if err != nil {
		return nil, err
	}

	return &CompatField{Dimensions: dimensions}, nil
}

// checkCompatibility checks if two compatibility fields are compatible by
// looking at the tags and at the intersection of the defined integer ranges.
func checkCompatibility(compat1, compat2 CompatField) bool {
	if len(compat1.Dimensions) != len(compat2.Dimensions) {
		return false
	}
	for i, t1 := range compat1.Dimensions {
		t2 := compat2.Dimensions[i]
		if t1.Tag != t2.Tag {
			return false
		}
		if len(t1.Values) != len(t2.Values) {
			return false
		}
		for j, v1 := range t1.Values {
			v2 := t2.Values[j]
			if v1.Max < v2.Min || v2.Max < v1.Min {
				return false
			}
		}
	}
	return true
}
