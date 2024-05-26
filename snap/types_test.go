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

	"github.com/ddkwork/golibrary/mylog"
	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"
)

type typeSuite struct{}

var _ = Suite(&typeSuite{})

func (s *typeSuite) TestJSONerr(c *C) {
	var t Type
	mylog.Check(json.Unmarshal([]byte("false"), &t))
	c.Assert(err, NotNil)
}

func (s *typeSuite) TestJsonMarshalTypes(c *C) {
	out := mylog.Check2(json.Marshal(TypeApp))

	c.Check(string(out), Equals, "\"app\"")

	out = mylog.Check2(json.Marshal(TypeGadget))

	c.Check(string(out), Equals, "\"gadget\"")

	out = mylog.Check2(json.Marshal(TypeOS))

	c.Check(string(out), Equals, "\"os\"")

	out = mylog.Check2(json.Marshal(TypeKernel))

	c.Check(string(out), Equals, "\"kernel\"")

	out = mylog.Check2(json.Marshal(TypeBase))

	c.Check(string(out), Equals, "\"base\"")

	out = mylog.Check2(json.Marshal(TypeSnapd))

	c.Check(string(out), Equals, "\"snapd\"")
}

func (s *typeSuite) TestJsonUnmarshalTypes(c *C) {
	var st Type
	mylog.Check(json.Unmarshal([]byte("\"application\""), &st))

	c.Check(st, Equals, TypeApp)
	mylog.Check(json.Unmarshal([]byte("\"app\""), &st))

	c.Check(st, Equals, TypeApp)
	mylog.Check(json.Unmarshal([]byte("\"gadget\""), &st))

	c.Check(st, Equals, TypeGadget)
	mylog.Check(json.Unmarshal([]byte("\"os\""), &st))

	c.Check(st, Equals, TypeOS)
	mylog.Check(json.Unmarshal([]byte("\"kernel\""), &st))

	c.Check(st, Equals, TypeKernel)
	mylog.Check(json.Unmarshal([]byte("\"base\""), &st))

	c.Check(st, Equals, TypeBase)
	mylog.Check(json.Unmarshal([]byte("\"snapd\""), &st))

	c.Check(st, Equals, TypeSnapd)
}

func (s *typeSuite) TestJsonUnmarshalInvalidTypes(c *C) {
	invalidTypes := []string{"foo", "-app", "gadget_"}
	var st Type
	for _, invalidType := range invalidTypes {
		mylog.Check(json.Unmarshal([]byte(fmt.Sprintf("%q", invalidType)), &st))
		c.Assert(err, NotNil, Commentf("Expected '%s' to be an invalid type", invalidType))
	}
}

func (s *typeSuite) TestYamlMarshalTypes(c *C) {
	out := mylog.Check2(yaml.Marshal(TypeApp))

	c.Check(string(out), Equals, "app\n")

	out = mylog.Check2(yaml.Marshal(TypeGadget))

	c.Check(string(out), Equals, "gadget\n")

	out = mylog.Check2(yaml.Marshal(TypeOS))

	c.Check(string(out), Equals, "os\n")

	out = mylog.Check2(yaml.Marshal(TypeKernel))

	c.Check(string(out), Equals, "kernel\n")

	out = mylog.Check2(yaml.Marshal(TypeBase))

	c.Check(string(out), Equals, "base\n")
}

func (s *typeSuite) TestYamlUnmarshalTypes(c *C) {
	var st Type
	mylog.Check(yaml.Unmarshal([]byte("application"), &st))

	c.Check(st, Equals, TypeApp)
	mylog.Check(yaml.Unmarshal([]byte("app"), &st))

	c.Check(st, Equals, TypeApp)
	mylog.Check(yaml.Unmarshal([]byte("gadget"), &st))

	c.Check(st, Equals, TypeGadget)
	mylog.Check(yaml.Unmarshal([]byte("os"), &st))

	c.Check(st, Equals, TypeOS)
	mylog.Check(yaml.Unmarshal([]byte("kernel"), &st))

	c.Check(st, Equals, TypeKernel)
	mylog.Check(yaml.Unmarshal([]byte("base"), &st))

	c.Check(st, Equals, TypeBase)
}

func (s *typeSuite) TestYamlUnmarshalInvalidTypes(c *C) {
	invalidTypes := []string{"foo", "-app", "gadget_"}
	var st Type
	for _, invalidType := range invalidTypes {
		mylog.Check(yaml.Unmarshal([]byte(invalidType), &st))
		c.Assert(err, NotNil, Commentf("Expected '%s' to be an invalid type", invalidType))
	}
}

func (s *typeSuite) TestYamlMarshalConfinementTypes(c *C) {
	out := mylog.Check2(yaml.Marshal(DevModeConfinement))

	c.Check(string(out), Equals, "devmode\n")

	out = mylog.Check2(yaml.Marshal(StrictConfinement))

	c.Check(string(out), Equals, "strict\n")
}

func (s *typeSuite) TestYamlUnmarshalConfinementTypes(c *C) {
	var confinementType ConfinementType
	mylog.Check(yaml.Unmarshal([]byte("devmode"), &confinementType))

	c.Check(confinementType, Equals, DevModeConfinement)
	mylog.Check(yaml.Unmarshal([]byte("strict"), &confinementType))

	c.Check(confinementType, Equals, StrictConfinement)
}

func (s *typeSuite) TestYamlUnmarshalInvalidConfinementTypes(c *C) {
	invalidConfinementTypes := []string{
		"foo", "strict-", "_devmode",
	}
	var confinementType ConfinementType
	for _, thisConfinementType := range invalidConfinementTypes {
		mylog.Check(yaml.Unmarshal([]byte(thisConfinementType), &confinementType))
		c.Assert(err, NotNil, Commentf("Expected '%s' to be an invalid confinement type", thisConfinementType))
	}
}

func (s *typeSuite) TestJsonMarshalConfinementTypes(c *C) {
	out := mylog.Check2(json.Marshal(DevModeConfinement))

	c.Check(string(out), Equals, "\"devmode\"")

	out = mylog.Check2(json.Marshal(StrictConfinement))

	c.Check(string(out), Equals, "\"strict\"")
}

func (s *typeSuite) TestJsonUnmarshalConfinementTypes(c *C) {
	var confinementType ConfinementType
	mylog.Check(json.Unmarshal([]byte("\"devmode\""), &confinementType))

	c.Check(confinementType, Equals, DevModeConfinement)
	mylog.Check(json.Unmarshal([]byte("\"strict\""), &confinementType))

	c.Check(confinementType, Equals, StrictConfinement)
}

func (s *typeSuite) TestJsonUnmarshalInvalidConfinementTypes(c *C) {
	invalidConfinementTypes := []string{
		"foo", "strict-", "_devmode",
	}
	var confinementType ConfinementType
	for _, thisConfinementType := range invalidConfinementTypes {
		mylog.Check(json.Unmarshal([]byte(fmt.Sprintf("%q", thisConfinementType)), &confinementType))
		c.Assert(err, NotNil, Commentf("Expected '%s' to be an invalid confinement type", thisConfinementType))
	}
}

func (s *typeSuite) TestYamlMarshalDaemonScopes(c *C) {
	out := mylog.Check2(yaml.Marshal(SystemDaemon))

	c.Check(string(out), Equals, "system\n")

	out = mylog.Check2(yaml.Marshal(UserDaemon))

	c.Check(string(out), Equals, "user\n")
}

func (s *typeSuite) TestYamlUnmarshalDaemonScopes(c *C) {
	var daemonScope DaemonScope
	mylog.Check(yaml.Unmarshal([]byte("system"), &daemonScope))

	c.Check(daemonScope, Equals, SystemDaemon)
	mylog.Check(yaml.Unmarshal([]byte("user"), &daemonScope))

	c.Check(daemonScope, Equals, UserDaemon)
}

func (s *typeSuite) TestYamlUnmarshalInvalidDaemonScopes(c *C) {
	invalidDaemonScopes := []string{
		"foo", "system-", "_user",
	}
	var daemonScope DaemonScope
	for _, thisDaemonScope := range invalidDaemonScopes {
		mylog.Check(yaml.Unmarshal([]byte(thisDaemonScope), &daemonScope))
		c.Assert(err, NotNil, Commentf("Expected '%s' to be an invalid daemon scope", thisDaemonScope))
	}
}

func (s *typeSuite) TestJsonMarshalDaemonScopes(c *C) {
	out := mylog.Check2(json.Marshal(SystemDaemon))

	c.Check(string(out), Equals, "\"system\"")

	out = mylog.Check2(json.Marshal(UserDaemon))

	c.Check(string(out), Equals, "\"user\"")
}

func (s *typeSuite) TestJsonUnmarshalDaemonScopes(c *C) {
	var daemonScope DaemonScope
	mylog.Check(json.Unmarshal([]byte("\"system\""), &daemonScope))

	c.Check(daemonScope, Equals, SystemDaemon)
	mylog.Check(json.Unmarshal([]byte("\"user\""), &daemonScope))

	c.Check(daemonScope, Equals, UserDaemon)
}

func (s *typeSuite) TestJsonUnmarshalInvalidDaemonScopes(c *C) {
	invalidDaemonScopes := []string{
		"foo", "system-", "_user",
	}
	var daemonScope DaemonScope
	for _, thisDaemonScope := range invalidDaemonScopes {
		mylog.Check(json.Unmarshal([]byte(fmt.Sprintf("%q", thisDaemonScope)), &daemonScope))
		c.Assert(err, NotNil, Commentf("Expected '%s' to be an invalid daemon scope", thisDaemonScope))
	}
}
