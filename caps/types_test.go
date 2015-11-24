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
	"encoding/json"

	. "gopkg.in/check.v1"
)

type TypeSuite struct{}

var _ = Suite(&TypeSuite{})

// testType is only meant for testing. It is not useful in any way except
// that it offers an simple capability type that will happily validate.
var testType = &Type{"test"}

func (s *TypeSuite) TestTypeString(c *C) {
	c.Assert(testType.String(), Equals, "test")
}

func (s *TypeSuite) TestValidateMismatchedType(c *C) {
	testType2 := &Type{"test-two"} // Another test-like type that's not test itself
	cap := &Capability{Name: "name", Label: "label", Type: testType2}
	err := testType.Validate(cap)
	c.Assert(err, ErrorMatches, `capability is not of type "test"`)
}

func (s *TypeSuite) TestValidateOK(c *C) {
	cap := &Capability{Name: "name", Label: "label", Type: testType}
	err := testType.Validate(cap)
	c.Assert(err, IsNil)
}

func (s *TypeSuite) TestValidateAttributes(c *C) {
	cap := &Capability{
		Name:  "name",
		Label: "label",
		Type:  testType,
		Attrs: map[string]string{
			"Key": "Value",
		},
	}
	err := testType.Validate(cap)
	c.Assert(err, ErrorMatches, "attributes must be empty for now")
}

func (s *TypeSuite) TestMarhshalJSON(c *C) {
	b, err := json.Marshal(testType)
	c.Assert(err, IsNil)
	c.Assert(b, DeepEquals, []byte(`"test"`))
}

func (s *TypeSuite) TestUnmarhshalJSON(c *C) {
	var t Type
	err := json.Unmarshal([]byte(`"test"`), &t)
	c.Assert(err, IsNil)
	c.Assert(t.Name, Equals, "test")
}
