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
	c.Check(all, DeepContains, &builtin.BluezInterface{})
	c.Check(all, DeepContains, &builtin.BoolFileInterface{})
	c.Check(all, DeepContains, &builtin.BrowserSupportInterface{})
	c.Check(all, DeepContains, &builtin.DbusInterface{})
	c.Check(all, DeepContains, &builtin.DockerInterface{})
	c.Check(all, DeepContains, &builtin.DockerSupportInterface{})
	c.Check(all, DeepContains, &builtin.FramebufferInterface{})
	c.Check(all, DeepContains, &builtin.FwupdInterface{})
	c.Check(all, DeepContains, &builtin.GpioInterface{})
	c.Check(all, DeepContains, &builtin.HidrawInterface{})
	c.Check(all, DeepContains, &builtin.I2cInterface{})
	c.Check(all, DeepContains, &builtin.IioInterface{})
	c.Check(all, DeepContains, &builtin.IioPortsControlInterface{})
	c.Check(all, DeepContains, &builtin.JoystickInterface{})
	c.Check(all, DeepContains, &builtin.LocationControlInterface{})
	c.Check(all, DeepContains, &builtin.LocationObserveInterface{})
	c.Check(all, DeepContains, &builtin.LxdSupportInterface{})
	c.Check(all, DeepContains, &builtin.MediaHubInterface{})
	c.Check(all, DeepContains, &builtin.MaliitInterface{})
	c.Check(all, DeepContains, &builtin.MirInterface{})
	c.Check(all, DeepContains, &builtin.MprisInterface{})
	c.Check(all, DeepContains, &builtin.PhysicalMemoryControlInterface{})
	c.Check(all, DeepContains, &builtin.PhysicalMemoryObserveInterface{})
	c.Check(all, DeepContains, &builtin.PulseAudioInterface{})
	c.Check(all, DeepContains, &builtin.SerialPortInterface{})
	c.Check(all, DeepContains, &builtin.ThumbnailerServiceInterface{})
	c.Check(all, DeepContains, &builtin.TimeControlInterface{})
	c.Check(all, DeepContains, &builtin.UDisks2Interface{})
	c.Check(all, DeepContains, &builtin.UbuntuDownloadManagerInterface{})
	c.Check(all, DeepContains, &builtin.Unity8Interface{})
	c.Check(all, DeepContains, &builtin.UpowerObserveInterface{})
	c.Check(all, DeepContains, &builtin.UhidInterface{})
	c.Check(all, DeepContains, &builtin.Unity7Interface{})
	c.Check(all, DeepContains, builtin.NewAccountControlInterface())
	c.Check(all, DeepContains, builtin.NewAlsaInterface())
	c.Check(all, DeepContains, builtin.NewAvahiObserveInterface())
	c.Check(all, DeepContains, builtin.NewAutopilotIntrospectionInterface())
	c.Check(all, DeepContains, builtin.NewBluetoothControlInterface())
	c.Check(all, DeepContains, builtin.NewCameraInterface())
	c.Check(all, DeepContains, builtin.NewCupsControlInterface())
	c.Check(all, DeepContains, builtin.NewFirewallControlInterface())
	c.Check(all, DeepContains, builtin.NewFuseSupportInterface())
	c.Check(all, DeepContains, builtin.NewGsettingsInterface())
	c.Check(all, DeepContains, builtin.NewHomeInterface())
	c.Check(all, DeepContains, builtin.NewKernelModuleControlInterface())
	c.Check(all, DeepContains, builtin.NewKubernetesSupportInterface())
	c.Check(all, DeepContains, builtin.NewLocaleControlInterface())
	c.Check(all, DeepContains, builtin.NewLogObserveInterface())
	c.Check(all, DeepContains, builtin.NewMountObserveInterface())
	c.Check(all, DeepContains, builtin.NewNetlinkAuditInterface())
	c.Check(all, DeepContains, builtin.NewNetlinkConnectorInterface())
	c.Check(all, DeepContains, builtin.NewNetworkBindInterface())
	c.Check(all, DeepContains, builtin.NewNetworkControlInterface())
	c.Check(all, DeepContains, builtin.NewNetworkInterface())
	c.Check(all, DeepContains, builtin.NewNetworkObserveInterface())
	c.Check(all, DeepContains, builtin.NewOpenglInterface())
	c.Check(all, DeepContains, builtin.NewOpenvSwitchInterface())
	c.Check(all, DeepContains, builtin.NewOpenvSwitchSupportInterface())
	c.Check(all, DeepContains, builtin.NewOpticalDriveInterface())
	c.Check(all, DeepContains, builtin.NewProcessControlInterface())
	c.Check(all, DeepContains, builtin.NewRawUsbInterface())
	c.Check(all, DeepContains, builtin.NewRemovableMediaInterface())
	c.Check(all, DeepContains, builtin.NewScreenInhibitControlInterface())
	c.Check(all, DeepContains, builtin.NewSnapdControlInterface())
	c.Check(all, DeepContains, builtin.NewSystemObserveInterface())
	c.Check(all, DeepContains, builtin.NewSystemTraceInterface())
	c.Check(all, DeepContains, builtin.NewTimeserverControlInterface())
	c.Check(all, DeepContains, builtin.NewTimezoneControlInterface())
	c.Check(all, DeepContains, builtin.NewTpmInterface())
	c.Check(all, DeepContains, builtin.NewUnity8CalendarInterface())
	c.Check(all, DeepContains, builtin.NewUnity8ContactsInterface())
	c.Check(all, DeepContains, builtin.NewX11Interface())
}
