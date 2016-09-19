// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package builtin_test

import (
	"github.com/snapcore/snapd/interfaces/builtin"
	. "github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

type AllSuite struct{}

var _ = Suite(&AllSuite{})

func (s *AllSuite) TestInterfaces(c *C) {
	all := builtin.Interfaces()
	c.Check(all, Contains, &builtin.BoolFileInterface{})
	c.Check(all, Contains, &builtin.BluezInterface{})
	c.Check(all, Contains, &builtin.BrowserSupportInterface{})
	c.Check(all, Contains, &builtin.DockerInterface{})
	c.Check(all, Contains, &builtin.GpioInterface{})
	c.Check(all, Contains, &builtin.HidrawInterface{})
	c.Check(all, Contains, &builtin.LocationControlInterface{})
	c.Check(all, Contains, &builtin.LocationObserveInterface{})
	c.Check(all, Contains, &builtin.LxdSupportInterface{})
	c.Check(all, Contains, &builtin.MirInterface{})
	c.Check(all, Contains, &builtin.MprisInterface{})
	c.Check(all, Contains, &builtin.SerialPortInterface{})
	c.Check(all, Contains, &builtin.PulseAudioInterface{})
	c.Check(all, Contains, &builtin.UDisks2Interface{})
	c.Check(all, Contains, &builtin.FwupdInterface{})
	c.Check(all, DeepContains, builtin.NewFirewallControlInterface())
	c.Check(all, DeepContains, builtin.NewGsettingsInterface())
	c.Check(all, DeepContains, builtin.NewHomeInterface())
	c.Check(all, DeepContains, builtin.NewLocaleControlInterface())
	c.Check(all, DeepContains, builtin.NewLogObserveInterface())
	c.Check(all, DeepContains, builtin.NewMountObserveInterface())
	c.Check(all, DeepContains, builtin.NewNetworkInterface())
	c.Check(all, DeepContains, builtin.NewNetworkBindInterface())
	c.Check(all, DeepContains, builtin.NewNetworkControlInterface())
	c.Check(all, DeepContains, builtin.NewNetworkObserveInterface())
	c.Check(all, DeepContains, builtin.NewProcessControlInterface())
	c.Check(all, DeepContains, builtin.NewRemovableMediaInterface())
	c.Check(all, DeepContains, builtin.NewScreenInhibitControlInterface())
	c.Check(all, DeepContains, builtin.NewSnapdControlInterface())
	c.Check(all, DeepContains, builtin.NewSystemObserveInterface())
	c.Check(all, DeepContains, builtin.NewSystemTraceInterface())
	c.Check(all, DeepContains, builtin.NewTimeserverControlInterface())
	c.Check(all, DeepContains, builtin.NewTimezoneControlInterface())
	c.Check(all, DeepContains, builtin.NewTpmInterface())
	c.Check(all, DeepContains, builtin.NewUnity7Interface())
	c.Check(all, DeepContains, builtin.NewUPowerObserveInterface())
	c.Check(all, DeepContains, builtin.NewX11Interface())
	c.Check(all, DeepContains, builtin.NewOpenglInterface())
	c.Check(all, DeepContains, builtin.NewCupsControlInterface())
	c.Check(all, DeepContains, builtin.NewOpticalDriveInterface())
	c.Check(all, DeepContains, builtin.NewCameraInterface())
	c.Check(all, DeepContains, builtin.NewKernelModuleControlInterface())
	c.Check(all, DeepContains, builtin.NewFuseSupportInterface())
}
