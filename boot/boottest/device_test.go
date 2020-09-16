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
	c.Check(dev.HasModeenv(), Equals, false)

	c.Check(func() { dev.Model() }, PanicMatches, "Device.Model called.*")

	c.Check(func() { boottest.MockDevice("@run") }, Panics, "MockDevice with no snap name and @mode is unsupported")
}

func (s *boottestSuite) TestMockDeviceBaseOrKernel(c *C) {
	dev := boottest.MockDevice("boot-snap")
	c.Check(dev.Classic(), Equals, false)
	c.Check(dev.Kernel(), Equals, "boot-snap")
	c.Check(dev.Base(), Equals, "boot-snap")
	c.Check(dev.RunMode(), Equals, true)
	c.Check(dev.HasModeenv(), Equals, false)
	c.Check(func() { dev.Model() }, Panics, "Device.Model called but MockUC20Device not used")

	dev = boottest.MockDevice("boot-snap@run")
	c.Check(dev.Classic(), Equals, false)
	c.Check(dev.Kernel(), Equals, "boot-snap")
	c.Check(dev.Base(), Equals, "boot-snap")
	c.Check(dev.RunMode(), Equals, true)
	c.Check(dev.HasModeenv(), Equals, true)
	c.Check(func() { dev.Model() }, PanicMatches, "Device.Model called.*")

	dev = boottest.MockDevice("boot-snap@recover")
	c.Check(dev.Classic(), Equals, false)
	c.Check(dev.Kernel(), Equals, "boot-snap")
	c.Check(dev.Base(), Equals, "boot-snap")
	c.Check(dev.RunMode(), Equals, false)
	c.Check(dev.HasModeenv(), Equals, true)
	c.Check(func() { dev.Model() }, PanicMatches, "Device.Model called.*")
}

func (s *boottestSuite) TestMockUC20Device(c *C) {
	dev := boottest.MockUC20Device("", nil)
	c.Check(dev.HasModeenv(), Equals, true)
	c.Check(dev.Classic(), Equals, false)
	c.Check(dev.RunMode(), Equals, true)
	c.Check(dev.Kernel(), Equals, "pc-kernel")
	c.Check(dev.Base(), Equals, "core20")

	c.Check(dev.Model().Model(), Equals, "my-model-uc20")

	dev = boottest.MockUC20Device("run", nil)
	c.Check(dev.RunMode(), Equals, true)

	dev = boottest.MockUC20Device("recover", nil)
	c.Check(dev.RunMode(), Equals, false)

	model := boottest.MakeMockUC20Model(map[string]interface{}{
		"model": "other-model-uc20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name": "pc-linux",
				"id":   "pclinuxdidididididididididididid",
				"type": "kernel",
			},
			map[string]interface{}{
				"name": "pc",
				"id":   "pcididididididididididididididid",
				"type": "gadget",
			},
		},
	})
	dev = boottest.MockUC20Device("recover", model)
	c.Check(dev.RunMode(), Equals, false)
	c.Check(dev.Kernel(), Equals, "pc-linux")
	c.Check(dev.Model().Model(), Equals, "other-model-uc20")
	c.Check(dev.Model(), Equals, model)
}
