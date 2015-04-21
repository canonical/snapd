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

package snappy

import (
	. "launchpad.net/gocheck"
)

func (s *SnapTestSuite) TestGetUdevPartName(c *C) {
	packageYaml, err := parsePackageYamlData([]byte(`name: foo
version: 1.0
icon: foo.svg
vendor: Foo Bar <foo@example.com>
`))
	c.Assert(err, IsNil)

	udevName, err := getUdevPartName(packageYaml, "/apps/foo.mvo/1.0/")
	c.Assert(err, IsNil)
	c.Assert(udevName, Equals, "foo.mvo")
}

func (s *SnapTestSuite) TestGetUdevPartNameFramework(c *C) {
	packageYaml, err := parsePackageYamlData([]byte(`name: foo
version: 1.0
icon: foo.svg
type: framework
vendor: Foo Bar <foo@example.com>
`))
	c.Assert(err, IsNil)

	udevName, err := getUdevPartName(packageYaml, "/apps/foo/1.0/")
	c.Assert(err, IsNil)
	c.Assert(udevName, Equals, "foo")
}
