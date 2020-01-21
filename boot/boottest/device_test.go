// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package boottest_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot/boottest"
)

func TestBoottest(t *testing.T) { TestingT(t) }

type boottestSuite struct{}

var _ = Suite(&boottestSuite{})

func (s *boottestSuite) TestMockDeviceClassic(c *C) {
	dev := boottest.MockDevice("")
	c.Check(dev.Classic(), Equals, true)
	c.Check(dev.Kernel(), Equals, "")
	c.Check(dev.Base(), Equals, "")
	c.Check(dev.RunMode(), Equals, true)

	dev = boottest.MockDevice("@run")
	c.Check(dev.Classic(), Equals, true)
	c.Check(dev.Kernel(), Equals, "")
	c.Check(dev.Base(), Equals, "")
	c.Check(dev.RunMode(), Equals, true)

	dev = boottest.MockDevice("@recover")
	c.Check(dev.Classic(), Equals, true)
	c.Check(dev.Kernel(), Equals, "")
	c.Check(dev.Base(), Equals, "")
	c.Check(dev.RunMode(), Equals, false)
}

func (s *boottestSuite) TestMockDeviceBaseOrKernel(c *C) {
	dev := boottest.MockDevice("boot-snap")
	c.Check(dev.Classic(), Equals, false)
	c.Check(dev.Kernel(), Equals, "boot-snap")
	c.Check(dev.Base(), Equals, "boot-snap")
	c.Check(dev.RunMode(), Equals, true)

	dev = boottest.MockDevice("boot-snap@run")
	c.Check(dev.Classic(), Equals, false)
	c.Check(dev.Kernel(), Equals, "boot-snap")
	c.Check(dev.Base(), Equals, "boot-snap")
	c.Check(dev.RunMode(), Equals, true)

	dev = boottest.MockDevice("boot-snap@recover")
	c.Check(dev.Classic(), Equals, false)
	c.Check(dev.Kernel(), Equals, "boot-snap")
	c.Check(dev.Base(), Equals, "boot-snap")
	c.Check(dev.RunMode(), Equals, false)
}
