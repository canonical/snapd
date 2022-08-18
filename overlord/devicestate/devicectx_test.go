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
	"testing"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

func TestBoot(t *testing.T) { TestingT(t) }

type deviceCtxSuite struct {
	testutil.BaseTest
}

var _ = Suite(&deviceCtxSuite{})

func (s *deviceCtxSuite) SetUpTest(c *C) {
}

func (s *deviceCtxSuite) TestInUseClassic(c *C) {
	var devCtx snapstate.DeviceContext

	classModel := assertstest.FakeAssertion(map[string]interface{}{
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
	devCtx = devicestate.BuildGroundDeviceContext(classModel, "run")
	c.Check(devCtx.RunMode(), Equals, true)
	c.Check(devCtx.Classic(), Equals, true)
	c.Check(devCtx.Kernel(), Equals, "")
	c.Check(devCtx.Base(), Equals, "")
	c.Check(devCtx.Gadget(), Equals, "")
	c.Check(devCtx.HasModeenv(), Equals, false)
	c.Check(devCtx.IsCoreBoot(), Equals, false)
	c.Check(devCtx.IsClassicBoot(), Equals, true)
	c.Check(devCtx.Model(), DeepEquals, classModel)

	legacyUCModel := boottest.MakeMockModel()
	devCtx = devicestate.BuildGroundDeviceContext(legacyUCModel, "run")
	c.Check(devCtx.RunMode(), Equals, true)
	c.Check(devCtx.Classic(), Equals, false)
	c.Check(devCtx.Kernel(), Equals, "pc-kernel")
	c.Check(devCtx.Base(), Equals, "core18")
	c.Check(devCtx.Gadget(), Equals, "pc")
	c.Check(devCtx.HasModeenv(), Equals, false)
	c.Check(devCtx.IsCoreBoot(), Equals, true)
	c.Check(devCtx.IsClassicBoot(), Equals, false)
	c.Check(devCtx.Model(), DeepEquals, legacyUCModel)

	ucModel := boottest.MakeMockUC20Model()
	devCtx = devicestate.BuildGroundDeviceContext(ucModel, "run")
	c.Check(devCtx.RunMode(), Equals, true)
	c.Check(devCtx.Classic(), Equals, false)
	c.Check(devCtx.Kernel(), Equals, "pc-kernel")
	c.Check(devCtx.Base(), Equals, "core20")
	c.Check(devCtx.Gadget(), Equals, "pc")
	c.Check(devCtx.HasModeenv(), Equals, true)
	c.Check(devCtx.IsCoreBoot(), Equals, true)
	c.Check(devCtx.IsClassicBoot(), Equals, false)
	c.Check(devCtx.Model(), DeepEquals, ucModel)

	classWithModes := boottest.MakeMockClassicWithModesModel()
	devCtx = devicestate.BuildGroundDeviceContext(classWithModes, "run")
	c.Check(devCtx.RunMode(), Equals, true)
	c.Check(devCtx.Classic(), Equals, true)
	c.Check(devCtx.Kernel(), Equals, "pc-kernel")
	c.Check(devCtx.Base(), Equals, "core20")
	c.Check(devCtx.Gadget(), Equals, "pc")
	c.Check(devCtx.HasModeenv(), Equals, true)
	c.Check(devCtx.IsCoreBoot(), Equals, true)
	c.Check(devCtx.IsClassicBoot(), Equals, false)
	c.Check(devCtx.Model(), DeepEquals, classWithModes)
}
