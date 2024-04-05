// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package naming_test

import (
	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/snap/naming"
)

type componentRefSuite struct{}

var _ = Suite(&componentRefSuite{})

func (s *componentRefSuite) TestNewComponentRefAndString(c *C) {
	fooRef := naming.NewComponentRef("foo", "foo-comp")
	c.Check(fooRef.SnapName, Equals, "foo")
	c.Check(fooRef.ComponentName, Equals, "foo-comp")
	c.Check(fooRef.String(), Equals, "foo+foo-comp")
}

func (s *componentRefSuite) TestValidate(c *C) {
	fooRef := naming.NewComponentRef("foo", "foo-comp")
	c.Check(fooRef.Validate(), IsNil)

	fooRef = naming.NewComponentRef("foo_", "foo-comp")
	c.Check(fooRef.Validate().Error(), Equals, `invalid snap name: "foo_"`)
}

func (s *componentRefSuite) TestUnmarshal(c *C) {
	var cr naming.ComponentRef

	yamlData := []byte(`mysnap+test-info`)
	c.Check(yaml.UnmarshalStrict(yamlData, &cr), IsNil)

	yamlData = []byte(`mysnap`)
	c.Check(yaml.UnmarshalStrict(yamlData, &cr).Error(), Equals, `incorrect component name "mysnap"`)
}
