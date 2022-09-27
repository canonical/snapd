// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package devicestate_test

import (
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

type deviceCtxSuite struct {
	testutil.BaseTest
}

var _ = Suite(&deviceCtxSuite{})

func (s *deviceCtxSuite) SetUpTest(c *C) {
}

func testGroundDeviceContextCommonChecks(c *C, devCtx snapstate.DeviceContext, mode string) {
	c.Check(devCtx.RunMode(), Equals, mode == "run", Commentf("mode is %q", mode))
	c.Check(devCtx.GroundContext(), Equals, devCtx)
	c.Check(func() { devCtx.Store() }, PanicMatches,
		"retrieved ground context is not intended to drive store operations")
	c.Check(devCtx.ForRemodeling(), Equals, false)
	c.Check(devCtx.SystemMode(), Equals, mode)
}

func (s *deviceCtxSuite) testGroundDeviceContext(c *C, mode string) {
	var devCtx snapstate.DeviceContext

	// Classic with classic initramfs
	classicModel := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "model",
		"classic":      "true",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"display-name": "My Model",
		"architecture": "amd64",
		"timestamp":    "2018-01-01T08:00:00+00:00",
	}).(*asserts.Model)
	devCtx = devicestate.BuildGroundDeviceContext(classicModel, mode)
	c.Check(devCtx.Classic(), Equals, true)
	c.Check(devCtx.Kernel(), Equals, "")
	c.Check(devCtx.Base(), Equals, "")
	c.Check(devCtx.Gadget(), Equals, "")
	c.Check(devCtx.HasModeenv(), Equals, false)
	c.Check(devCtx.IsCoreBoot(), Equals, false)
	c.Check(devCtx.IsClassicBoot(), Equals, true)
	c.Check(devCtx.Model(), DeepEquals, classicModel)
	testGroundDeviceContextCommonChecks(c, devCtx, mode)

	// UC16/18
	legacyUCModel := boottest.MakeMockModel()
	devCtx = devicestate.BuildGroundDeviceContext(legacyUCModel, mode)
	c.Check(devCtx.Classic(), Equals, false)
	c.Check(devCtx.Kernel(), Equals, "pc-kernel")
	c.Check(devCtx.Base(), Equals, "core18")
	c.Check(devCtx.Gadget(), Equals, "pc")
	c.Check(devCtx.HasModeenv(), Equals, false)
	c.Check(devCtx.IsCoreBoot(), Equals, true)
	c.Check(devCtx.IsClassicBoot(), Equals, false)
	c.Check(devCtx.Model(), DeepEquals, legacyUCModel)
	testGroundDeviceContextCommonChecks(c, devCtx, mode)

	// UC20+
	ucModel := boottest.MakeMockUC20Model()
	devCtx = devicestate.BuildGroundDeviceContext(ucModel, mode)
	c.Check(devCtx.Classic(), Equals, false)
	c.Check(devCtx.Kernel(), Equals, "pc-kernel")
	c.Check(devCtx.Base(), Equals, "core20")
	c.Check(devCtx.Gadget(), Equals, "pc")
	c.Check(devCtx.HasModeenv(), Equals, true)
	c.Check(devCtx.IsCoreBoot(), Equals, true)
	c.Check(devCtx.IsClassicBoot(), Equals, false)
	c.Check(devCtx.Model(), DeepEquals, ucModel)
	testGroundDeviceContextCommonChecks(c, devCtx, mode)

	// Classic with modes
	classicWithModes := boottest.MakeMockClassicWithModesModel()
	devCtx = devicestate.BuildGroundDeviceContext(classicWithModes, mode)
	c.Check(devCtx.Classic(), Equals, true)
	c.Check(devCtx.Kernel(), Equals, "pc-kernel")
	c.Check(devCtx.Base(), Equals, "core20")
	c.Check(devCtx.Gadget(), Equals, "pc")
	c.Check(devCtx.HasModeenv(), Equals, true)
	c.Check(devCtx.IsCoreBoot(), Equals, true)
	c.Check(devCtx.IsClassicBoot(), Equals, false)
	c.Check(devCtx.Model(), DeepEquals, classicWithModes)
	testGroundDeviceContextCommonChecks(c, devCtx, mode)
}

func (s *deviceCtxSuite) TestGroundDeviceContext(c *C) {
	for _, mode := range []string{"run", "install", "recover", "factory-reset"} {
		s.testGroundDeviceContext(c, mode)
	}
}
