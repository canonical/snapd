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

package arch

import (
	"testing"

	. "gopkg.in/check.v1"
)
// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&ArchTestSuite{})

type ArchTestSuite struct {
}

func (ts *ArchTestSuite) TestUbuntuArchitecture(c *C) {
	goarch = "arm"
	c.Check(goToUbuntuArchitecture(), Equals, "armhf")

	goarch = "amd64"
	c.Check(goToUbuntuArchitecture(), Equals, "amd64")

	goarch = "386"
	c.Check(goToUbuntuArchitecture(), Equals, "i386")
}

func (ts *ArchTestSuite) TestSetArchitecture(c *C) {
	SetArchitecture("armhf")
	c.Assert(UbuntuArchitecture(), Equals, "armhf")
}

func (ts *ArchTestSuite) TestSupportedArchitectures(c *C) {
	arch = "armhf"
	c.Check(IsSupportedArchitecture([]string{"all"}), Equals, true)
	c.Check(IsSupportedArchitecture([]string{"amd64", "armhf", "powerpc"}), Equals, true)
	c.Check(IsSupportedArchitecture([]string{"armhf"}), Equals, true)
	c.Check(IsSupportedArchitecture([]string{"amd64", "powerpc"}), Equals, false)

	arch = "amd64"
	c.Check(IsSupportedArchitecture([]string{"amd64", "armhf", "powerpc"}), Equals, true)
	c.Check(IsSupportedArchitecture([]string{"powerpc"}), Equals, false)
}
