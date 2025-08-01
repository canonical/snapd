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

// CompatDimension represents a <string>[-<integer|integer range>...]
// (dimension) in a compatibility label.
type CompatDimension struct {
	// Tag is the string identifier
	Tag string
	// Values is the list of integer/integer ranges
	Values []CompatRange
}

type CompatField struct {
	Dimensions []CompatDimension
}

// CompatSpec specifies valid values for a compatibility field, this can be
// used to further restrict these fields for a given interface. The number of
// dimensions, tags, and number of values per dimension must match the ones in
// this struct. The value ranges specified must be contained in the ranges
// specified here.
type CompatSpec struct {
	Dimensions []CompatDimension
}

const (
	absoluteMaxDimensions = 3
	absoluteMaxIntegers   = 3
)

var (
	tagRegexp = regexp.MustCompile(`^[a-z][a-z0-9]{0,31}$`)
	// 0 or a number that starts with 1-9 with a maximum of 8 digits
	intRegexp      = regexp.MustCompile(`^(0|[1-9][0-9]{0,7})$`)
	intRangeRegexp = regexp.MustCompile(`^\((0|[1-9][0-9]{0,7})\.\.(0|[1-9][0-9]{0,7})\)$`)
)

func appendDimension(dimensions []CompatDimension, dimension *CompatDimension) ([]CompatDimension, error) {
	if dimension != nil {
		for _, d := range dimensions {
			if d.Tag == dimension.Tag {
				return nil, fmt.Errorf("string %q appears more than once",
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

func parseIntegerRange(token string) (rg *CompatRange, err error) {
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
			return nil, fmt.Errorf("%q is not a valid string", token)
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
			return nil, fmt.Errorf("invalid range %q", token)
		}
		rg = &CompatRange{uint(min), uint(max)}
	}
	return rg, nil
}

// decodeCompatField decodes a compatibility string, which is a sequence of
// dimensions separated by dashes. Each dimension consist of an alphanumerical
// descriptor followed by a dash-separated series of integer or integer ranges.
// An optional spec can be provided to restrict the valid compatibility fields.
// This spec must strictly follow the convention that there must be at least an
// integer field per string (that is, "foo" must be specified as "foo-0").
func decodeCompatField(compat string, spec *CompatSpec) (*CompatField, error) {
	// TODO we want to consider these limits when doing snap pack but relax
	// in run time.
	maxNumDim := absoluteMaxDimensions
	maxNumInt := absoluteMaxIntegers
	tokens := strings.Split(compat, "-")
	dimensions := []CompatDimension{}
	compatError := func(msg string) error { return fmt.Errorf("compatibility label %q: %s", compat, msg) }
	var currentDimension *CompatDimension
	var err error
	for _, t := range tokens {
		if tagRegexp.MatchString(t) {
			dimIdx := len(dimensions) + 1
			if currentDimension == nil {
				dimIdx = 0
			}
			if dimIdx == maxNumDim {
				return nil, compatError(fmt.Sprintf("only %d strings allowed",
					maxNumDim))
			}
			dimensions, err = appendDimension(dimensions, currentDimension)
			if err != nil {
				return nil, compatError(err.Error())
			}
			currentDimension = &CompatDimension{Tag: t}
			continue
		}
		if currentDimension == nil {
			return nil, compatError(fmt.Sprintf("bad string %q", t))
		}

		rg, err := parseIntegerRange(t)
		if err != nil {
			return nil, compatError(err.Error())
		}
		currNumVal := len(currentDimension.Values)
		if currNumVal == maxNumInt {
			return nil, compatError(fmt.Sprintf(
				"only %d integer/integer ranges allowed per string", maxNumInt))
		}
		currentDimension.Values = append(currentDimension.Values, *rg)
	}

	// Add last dimension found
	dimensions, err = appendDimension(dimensions, currentDimension)
	if err != nil {
		return nil, compatError(err.Error())
	}

	// Are we compliant with the interface restrictions?
	compatField := &CompatField{Dimensions: dimensions}
	if err := checkCompatAgainstSpec(compatField, spec); err != nil {
		return nil, compatError(err.Error())
	}

	return compatField, nil
}

func checkCompatAgainstSpec(compatField *CompatField, spec *CompatSpec) error {
	if spec == nil {
		return nil
	}
	if len(compatField.Dimensions) != len(spec.Dimensions) {
		return fmt.Errorf("unexpected number of strings (should be %d)",
			len(spec.Dimensions))
	}
	for i, d := range compatField.Dimensions {
		specDim := spec.Dimensions[i]
		if d.Tag != specDim.Tag {
			return fmt.Errorf("string does not match interface spec (%s != %s)",
				d.Tag, specDim.Tag)
		}
		specNumVals := len(specDim.Values)
		if len(d.Values) != len(specDim.Values) {
			return fmt.Errorf("unexpected number of integers (should be %d for %q)",
				specNumVals, specDim.Tag)
		}
		for j, v := range d.Values {
			rgSpec := specDim.Values[j]
			if v.Min < rgSpec.Min || v.Max > rgSpec.Max {
				return fmt.Errorf("range (%d..%d) is not included in valid range (%d..%d)",
					v.Min, v.Max, rgSpec.Min, rgSpec.Max)
			}
		}
	}
	return nil
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
