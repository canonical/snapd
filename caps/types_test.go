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
	c.Assert(FileType.String(), Equals, "file")
	c.Assert(Type("device").String(), Equals, "device")
}

func (s *TypeSuite) TestValidateMismatchedType(c *C) {
	cap := &Capability{Name: "name", Label: "label", Type: Type("device")}
	err := FileType.Validate(cap)
	c.Assert(err, ErrorMatches, `capability is not of type "file"`)
}

func (s *TypeSuite) TestValidateOK(c *C) {
	cap := &Capability{Name: "name", Label: "label", Type: FileType}
	err := FileType.Validate(cap)
	c.Assert(err, IsNil)
}

func (s *TypeSuite) TestValidateAttributes(c *C) {
	cap := &Capability{
		Name:  "name",
		Label: "label",
		Type:  FileType,
		Attrs: map[string]string{
			"Key": "Value",
		},
	}
	err := FileType.Validate(cap)
	c.Assert(err, ErrorMatches, "attributes must be empty for now")
}
