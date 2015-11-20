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

package caps

import (
	"testing"

	. "gopkg.in/check.v1"
)

func TestType(t *testing.T) {
	TestingT(t)
}

type TypeSuite struct{}

var _ = Suite(&TypeSuite{})

func (s *TypeSuite) TestTypeString(c *C) {
	c.Assert(iotaType.String(), Equals, "iota")
}

func (s *TypeSuite) TestValidateMismatchedType(c *C) {
	iotaType2 := Type("iota-two") // Another iota-like type that's not iota itself
	cap := &Capability{Name: "name", Label: "label", Type: iotaType2}
	err := iotaType.Validate(cap)
	c.Assert(err, ErrorMatches, `capability is not of type "iota"`)
}

func (s *TypeSuite) TestValidateOK(c *C) {
	cap := &Capability{Name: "name", Label: "label", Type: iotaType}
	err := iotaType.Validate(cap)
	c.Assert(err, IsNil)
}

func (s *TypeSuite) TestValidateAttributes(c *C) {
	cap := &Capability{
		Name:  "name",
		Label: "label",
		Type:  iotaType,
		Attrs: map[string]string{
			"Key": "Value",
		},
	}
	err := iotaType.Validate(cap)
	c.Assert(err, ErrorMatches, "attributes must be empty for now")
}
