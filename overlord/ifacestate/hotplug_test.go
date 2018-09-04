// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package ifacestate_test

import (
	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/overlord/ifacestate"

	. "gopkg.in/check.v1"
)

type hotplugSuite struct{}

var _ = Suite(&hotplugSuite{})

func (s *hotplugSuite) TestDefaultDeviceKey(c *C) {
	di, err := hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":        "a/path",
		"ACTION":         "add",
		"SUBSYSTEM":      "foo",
		"ID_V4L_PRODUCT": "v4lproduct",
		"NAME":           "name",
		"ID_VENDOR_ID":   "vendor",
		"ID_MODEL_ID":    "model",
		"ID_SERIAL":      "serial",
		"ID_REVISION":    "revision",
	})
	c.Assert(err, IsNil)
	key := ifacestate.DefaultDeviceKey(di)
	c.Assert(key, Equals, "v4lproduct/vendor/model/serial")

	di, err = hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":      "a/path",
		"ACTION":       "add",
		"SUBSYSTEM":    "foo",
		"NAME":         "name",
		"ID_WWN":       "wnn",
		"ID_MODEL_ENC": "modelenc",
		"ID_REVISION":  "revision",
	})
	c.Assert(err, IsNil)
	key = ifacestate.DefaultDeviceKey(di)
	c.Assert(key, Equals, "name/wnn/modelenc/revision")

	di, err = hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":       "a/path",
		"ACTION":        "add",
		"SUBSYSTEM":     "foo",
		"PCI_SLOT_NAME": "pcislot",
		"ID_MODEL_ENC":  "modelenc",
	})
	c.Assert(err, IsNil)
	key = ifacestate.DefaultDeviceKey(di)
	c.Assert(key, Equals, "pcislot//modelenc/")

	di, err = hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":   "a/path",
		"ACTION":    "add",
		"SUBSYSTEM": "foo",
	})
	c.Assert(err, IsNil)
	key = ifacestate.DefaultDeviceKey(di)
	c.Assert(key, Equals, "///")
}
