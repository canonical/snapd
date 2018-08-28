// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v2"

	. "gopkg.in/check.v1"
)

type typeSuite struct{}

var _ = Suite(&typeSuite{})

func (s *typeSuite) TestJSONerr(c *C) {
	var t Type
	err := json.Unmarshal([]byte("false"), &t)
	c.Assert(err, NotNil)
}

func (s *typeSuite) TestJsonMarshalTypes(c *C) {
	out, err := json.Marshal(TypeApp)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "\"app\"")

	out, err = json.Marshal(TypeGadget)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "\"gadget\"")

	out, err = json.Marshal(TypeOS)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "\"os\"")

	out, err = json.Marshal(TypeKernel)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "\"kernel\"")

	out, err = json.Marshal(TypeBase)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "\"base\"")

	out, err = json.Marshal(TypeSnapd)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "\"snapd\"")
}

func (s *typeSuite) TestJsonUnmarshalTypes(c *C) {
	var st Type

	err := json.Unmarshal([]byte("\"application\""), &st)
	c.Assert(err, IsNil)
	c.Check(st, Equals, TypeApp)

	err = json.Unmarshal([]byte("\"app\""), &st)
	c.Assert(err, IsNil)
	c.Check(st, Equals, TypeApp)

	err = json.Unmarshal([]byte("\"gadget\""), &st)
	c.Assert(err, IsNil)
	c.Check(st, Equals, TypeGadget)

	err = json.Unmarshal([]byte("\"os\""), &st)
	c.Assert(err, IsNil)
	c.Check(st, Equals, TypeOS)

	err = json.Unmarshal([]byte("\"kernel\""), &st)
	c.Assert(err, IsNil)
	c.Check(st, Equals, TypeKernel)

	err = json.Unmarshal([]byte("\"base\""), &st)
	c.Assert(err, IsNil)
	c.Check(st, Equals, TypeBase)

	err = json.Unmarshal([]byte("\"snapd\""), &st)
	c.Assert(err, IsNil)
	c.Check(st, Equals, TypeSnapd)
}

func (s *typeSuite) TestJsonUnmarshalInvalidTypes(c *C) {
	invalidTypes := []string{"foo", "-app", "gadget_"}
	var st Type
	for _, invalidType := range invalidTypes {
		err := json.Unmarshal([]byte(fmt.Sprintf("%q", invalidType)), &st)
		c.Assert(err, NotNil, Commentf("Expected '%s' to be an invalid type", invalidType))
	}
}

func (s *typeSuite) TestYamlMarshalTypes(c *C) {
	out, err := yaml.Marshal(TypeApp)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "app\n")

	out, err = yaml.Marshal(TypeGadget)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "gadget\n")

	out, err = yaml.Marshal(TypeOS)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "os\n")

	out, err = yaml.Marshal(TypeKernel)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "kernel\n")

	out, err = yaml.Marshal(TypeBase)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "base\n")
}

func (s *typeSuite) TestYamlUnmarshalTypes(c *C) {
	var st Type

	err := yaml.Unmarshal([]byte("application"), &st)
	c.Assert(err, IsNil)
	c.Check(st, Equals, TypeApp)

	err = yaml.Unmarshal([]byte("app"), &st)
	c.Assert(err, IsNil)
	c.Check(st, Equals, TypeApp)

	err = yaml.Unmarshal([]byte("gadget"), &st)
	c.Assert(err, IsNil)
	c.Check(st, Equals, TypeGadget)

	err = yaml.Unmarshal([]byte("os"), &st)
	c.Assert(err, IsNil)
	c.Check(st, Equals, TypeOS)

	err = yaml.Unmarshal([]byte("kernel"), &st)
	c.Assert(err, IsNil)
	c.Check(st, Equals, TypeKernel)

	err = yaml.Unmarshal([]byte("base"), &st)
	c.Assert(err, IsNil)
	c.Check(st, Equals, TypeBase)
}

func (s *typeSuite) TestYamlUnmarshalInvalidTypes(c *C) {
	invalidTypes := []string{"foo", "-app", "gadget_"}
	var st Type
	for _, invalidType := range invalidTypes {
		err := yaml.Unmarshal([]byte(invalidType), &st)
		c.Assert(err, NotNil, Commentf("Expected '%s' to be an invalid type", invalidType))
	}
}

func (s *typeSuite) TestYamlMarshalConfinementTypes(c *C) {
	out, err := yaml.Marshal(DevModeConfinement)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "devmode\n")

	out, err = yaml.Marshal(StrictConfinement)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "strict\n")
}

func (s *typeSuite) TestYamlUnmarshalConfinementTypes(c *C) {
	var confinementType ConfinementType
	err := yaml.Unmarshal([]byte("devmode"), &confinementType)
	c.Assert(err, IsNil)
	c.Check(confinementType, Equals, DevModeConfinement)

	err = yaml.Unmarshal([]byte("strict"), &confinementType)
	c.Assert(err, IsNil)
	c.Check(confinementType, Equals, StrictConfinement)
}

func (s *typeSuite) TestYamlUnmarshalInvalidConfinementTypes(c *C) {
	var invalidConfinementTypes = []string{
		"foo", "strict-", "_devmode",
	}
	var confinementType ConfinementType
	for _, thisConfinementType := range invalidConfinementTypes {
		err := yaml.Unmarshal([]byte(thisConfinementType), &confinementType)
		c.Assert(err, NotNil, Commentf("Expected '%s' to be an invalid confinement type", thisConfinementType))
	}
}

func (s *typeSuite) TestJsonMarshalConfinementTypes(c *C) {
	out, err := json.Marshal(DevModeConfinement)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "\"devmode\"")

	out, err = json.Marshal(StrictConfinement)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "\"strict\"")
}

func (s *typeSuite) TestJsonUnmarshalConfinementTypes(c *C) {
	var confinementType ConfinementType
	err := json.Unmarshal([]byte("\"devmode\""), &confinementType)
	c.Assert(err, IsNil)
	c.Check(confinementType, Equals, DevModeConfinement)

	err = json.Unmarshal([]byte("\"strict\""), &confinementType)
	c.Assert(err, IsNil)
	c.Check(confinementType, Equals, StrictConfinement)
}

func (s *typeSuite) TestJsonUnmarshalInvalidConfinementTypes(c *C) {
	var invalidConfinementTypes = []string{
		"foo", "strict-", "_devmode",
	}
	var confinementType ConfinementType
	for _, thisConfinementType := range invalidConfinementTypes {
		err := json.Unmarshal([]byte(fmt.Sprintf("%q", thisConfinementType)), &confinementType)
		c.Assert(err, NotNil, Commentf("Expected '%s' to be an invalid confinement type", thisConfinementType))
	}
}
