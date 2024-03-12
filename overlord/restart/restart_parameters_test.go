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

package restart_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/overlord/restart"
)

type restartParametersSuite struct{}

var _ = Suite(&restartParametersSuite{})

func (s *restartParametersSuite) TestSetParameters(c *C) {
	rt := &restart.RestartParameters{}

	restart.RestartParametersInit(rt, "some-snap", restart.RestartSystem, &boot.RebootInfo{
		BootloaderOptions: &bootloader.Options{
			Role:        bootloader.RoleRunMode,
			NoSlashBoot: true,
		},
	})
	c.Check(rt.SnapName, Equals, "some-snap")
	c.Check(rt.RestartType, Equals, restart.RestartSystem)
	c.Check(rt.BootloaderOptions, DeepEquals, &bootloader.Options{
		Role:        bootloader.RoleRunMode,
		NoSlashBoot: true,
	})
}

func (s *restartParametersSuite) TestSetParametersPrecedences(c *C) {
	rt := &restart.RestartParameters{}

	restart.RestartParametersInit(rt, "restart", restart.RestartSystem, nil)
	c.Check(rt.SnapName, Equals, "restart")
	c.Check(rt.RestartType, Equals, restart.RestartSystem)
	c.Check(rt.BootloaderOptions, IsNil)

	restart.RestartParametersInit(rt, "restart-now", restart.RestartSystemNow, nil)
	c.Check(rt.SnapName, Equals, "restart-now")
	c.Check(rt.RestartType, Equals, restart.RestartSystemNow)
	c.Check(rt.BootloaderOptions, IsNil)

	restart.RestartParametersInit(rt, "halt-now", restart.RestartSystemHaltNow, nil)
	c.Check(rt.SnapName, Equals, "halt-now")
	c.Check(rt.RestartType, Equals, restart.RestartSystemHaltNow)
	c.Check(rt.BootloaderOptions, IsNil)

	restart.RestartParametersInit(rt, "poweroff-now", restart.RestartSystemPoweroffNow, nil)
	c.Check(rt.SnapName, Equals, "poweroff-now")
	c.Check(rt.RestartType, Equals, restart.RestartSystemPoweroffNow)
	c.Check(rt.BootloaderOptions, IsNil)

	// verify it's not changed after setting with *Now
	restart.RestartParametersInit(rt, "restart", restart.RestartSystem, nil)
	c.Check(rt.SnapName, Equals, "poweroff-now")
	c.Check(rt.RestartType, Equals, restart.RestartSystemPoweroffNow)
	c.Check(rt.BootloaderOptions, IsNil)
}

func (s *restartParametersSuite) TestSetParametersBootloaderOptionsSetOnce(c *C) {
	rt := &restart.RestartParameters{}

	restart.RestartParametersInit(rt, "some-snap", restart.RestartSystem, &boot.RebootInfo{
		BootloaderOptions: &bootloader.Options{
			PrepareImageTime: true,
			Role:             bootloader.RoleRunMode,
		},
	})
	c.Check(rt.SnapName, Equals, "some-snap")
	c.Check(rt.RestartType, Equals, restart.RestartSystem)
	c.Check(rt.BootloaderOptions, DeepEquals, &bootloader.Options{
		PrepareImageTime: true,
		Role:             bootloader.RoleRunMode,
	})

	// should essentially be a no-op right now
	restart.RestartParametersInit(rt, "other-snap", restart.RestartSystem, &boot.RebootInfo{
		BootloaderOptions: &bootloader.Options{
			Role: bootloader.RoleRunMode,
		},
	})
	c.Check(rt.SnapName, Equals, "some-snap")
	c.Check(rt.RestartType, Equals, restart.RestartSystem)
	c.Check(rt.BootloaderOptions, DeepEquals, &bootloader.Options{
		PrepareImageTime: true,
		Role:             bootloader.RoleRunMode,
	})
}
