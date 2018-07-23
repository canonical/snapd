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
	c.Check(ubuntuArchFromGoArch("386"), Equals, "i386")
	c.Check(ubuntuArchFromGoArch("amd64"), Equals, "amd64")
	c.Check(ubuntuArchFromGoArch("arm"), Equals, "armhf")
	c.Check(ubuntuArchFromGoArch("arm64"), Equals, "arm64")
	c.Check(ubuntuArchFromGoArch("ppc64le"), Equals, "ppc64el")
	c.Check(ubuntuArchFromGoArch("ppc64"), Equals, "ppc64")
	c.Check(ubuntuArchFromGoArch("s390x"), Equals, "s390x")
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
