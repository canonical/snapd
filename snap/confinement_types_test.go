// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package snap

import (
	"gopkg.in/yaml.v2"

	. "gopkg.in/check.v1"
)

type confinementTypeSuite struct{}

var _ = Suite(&confinementTypeSuite{})

func (s *confinementTypeSuite) TestMarshalConfinementTypes(c *C) {
	for key, value := range ConfinementTypeMap {
		out, err := yaml.Marshal(value)
		c.Assert(err, IsNil)
		c.Check(string(out), Equals, key+"\n")
	}
}

func (s *confinementTypeSuite) TestUnmarshalConfinementTypes(c *C) {
	var confinementType ConfinementType

	for key, value := range ConfinementTypeMap {
		err := yaml.Unmarshal([]byte(key), &confinementType)
		c.Assert(err, IsNil)
		c.Check(confinementType, Equals, value)
	}
}

func (s *confinementTypeSuite) TestUnmarshalInvalidConfinementTypes(c *C) {
	var invalidConfinementTypes = []string{
		"foo", "strict-", "_devmode",
	}
	var confinementType ConfinementType
	for _, thisConfinementType := range invalidConfinementTypes {
		err := yaml.Unmarshal([]byte(thisConfinementType), &confinementType)
		c.Assert(err, NotNil, Commentf("Expected '%s' to be an invalid confinement type", thisConfinementType))
	}
}
