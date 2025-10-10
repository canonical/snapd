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

package compatibility

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/logger"
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

func (cf *CompatField) String() string {
	var sb strings.Builder
	for _, d := range cf.Dimensions {
		if sb.Len() > 0 {
			sb.WriteRune('-')
		}
		sb.WriteString(d.Tag)
		for _, vals := range d.Values {
			if vals.Min == vals.Max {
				sb.WriteString(fmt.Sprintf("-%d", vals.Min))
			} else {
				sb.WriteString(fmt.Sprintf("-(%d..%d)", vals.Min, vals.Max))
			}
		}
	}
	return sb.String()
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

// IsValidExpression checks whether a compatibility expression compat is valid
// or not. The expressions are composed of compatibility labels joined by OR
// and AND operators, with parenthesis where necessary. Each compatibility
// label is a sequence of dimensions separated by dashes. Each dimension
// consist of an alphanumerical descriptor followed by a dash-separated series
// of integer or integer ranges. An optional spec can be provided to restrict
// the valid compatibility fields. This spec must strictly follow the
// convention that there must be at least an integer field per string (that is,
// "foo" must be specified as "foo-0").
func IsValidExpression(compat string, spec *CompatSpec) error {
	compatError := func(msg string) error { return fmt.Errorf("compatibility label %q: %s", compat, msg) }
	_, labels, err := parse(compat)
	if err != nil {
		return compatError(err.Error())
	}

	// Additional high level check on number of strings/integers
	// TODO we want to consider these limits when doing snap pack but relax
	// in run time.
	maxNumDim := absoluteMaxDimensions
	maxNumInt := absoluteMaxIntegers
	for _, lab := range labels {
		if len(lab.Dimensions) > maxNumDim {
			return compatError(fmt.Sprintf("only %d strings allowed", maxNumDim))
		}
		for _, dim := range lab.Dimensions {
			if len(dim.Values) > maxNumInt {
				return compatError(fmt.Sprintf(
					"only %d integer/integer ranges allowed per string", maxNumInt))
			}
		}
	}

	// Are we compliant with the interface restrictions?
	for _, l := range labels {
		if err := checkCompatAgainstSpec(&l, spec); err != nil {
			return compatError(err.Error())
		}
	}

	return nil
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

// CheckCompatibility checks the compatibility of two compatibility expressions.
func CheckCompatibility(compat1, compat2 string) bool {
	expr1, labels1, err := parse(compat1)
	if err != nil {
		return false
	}
	expr2, labels2, err := parse(compat2)
	if err != nil {
		return false
	}

	// Check cross-compatibility. This ensures that what is
	// provided/expected on both of the sides fulfills the requirements
	// expressed with ORs/ANDs of the other side. A way to visualize this
	// is to view the expressions as defining sets containing labels (ORs
	// create one set with the operands, ANDs define separate sets for each
	// operand - and expressions can always be formatted into OR
	// expressions joined by ANDs). Exp. 1 is compatible with 2 if all sets
	// defined by 1 have an intersection with one of the labels defined in
	// 2, and the other way around. If compatibility happens on both
	// directions, we consider the expressions compatible.
	return checkExpressionCompatibility(expr1, labels2) &&
		checkExpressionCompatibility(expr2, labels1)
}

// areLabelsCompatible checks if two compatibility labels are compatible by
// looking at the strings and at the intersection of the defined integer ranges.
func areLabelsCompatible(compat1, compat2 CompatField) bool {
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

// checkExpressionCompatibility check if the provided labels fulfill the
// conditions expressed in the abstract syntax tree root.
func checkExpressionCompatibility(node *Node, labels []CompatField) bool {
	if !node.isLeaf() && !node.hasBothChildren() {
		logger.Noticef("internal error: node must have both children or none")
		return false
	}
	if node.isLeaf() {
		// Must be a label
		nodeLabel, ok := node.Exp.(*CompatField)
		if !ok {
			logger.Noticef("internal error: leaf node is not a label: %s", node.Exp)
			return false
		}
		for _, l := range labels {
			if areLabelsCompatible(l, *nodeLabel) {
				return true
			}
		}
		return false
	}

	oper, ok := node.Exp.(*Operator)
	if !ok {
		logger.Noticef("internal error: internal node is not an operation: %s", node.Exp)
		return false
	}

	if oper.Oper.Typ == ItemOR {
		return checkExpressionCompatibility(node.Left, labels) ||
			checkExpressionCompatibility(node.Right, labels)
	}
	// Must be the AND operator
	return checkExpressionCompatibility(node.Left, labels) &&
		checkExpressionCompatibility(node.Right, labels)
}
