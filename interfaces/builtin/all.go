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

package builtin

import (
	"github.com/snapcore/snapd/interfaces"
)

var allInterfaces = []interfaces.Interface{
	&BoolFileInterface{},
	&BluezInterface{},
	&BrowserSupportInterface{},
	&ContentInterface{},
	&DockerInterface{},
	&GpioInterface{},
	&HidrawInterface{},
	&LocationControlInterface{},
	&LocationObserveInterface{},
	&LxdSupportInterface{},
	&MirInterface{},
	&ModemManagerInterface{},
	&MprisInterface{},
	&NetworkManagerInterface{},
	&PppInterface{},
	&SerialPortInterface{},
	&PulseAudioInterface{},
	&UDisks2Interface{},
	&FwupdInterface{},
	NewFirewallControlInterface(),
	NewGsettingsInterface(),
	NewHardwareObserveInterface(),
	NewHomeInterface(),
	NewLocaleControlInterface(),
	NewLogObserveInterface(),
	NewMountObserveInterface(),
	NewNetworkInterface(),
	NewNetworkBindInterface(),
	NewNetworkControlInterface(),
	NewNetworkObserveInterface(),
	NewProcessControlInterface(),
	NewRemovableMediaInterface(),
	NewScreenInhibitControlInterface(),
	NewSnapdControlInterface(),
	NewSystemObserveInterface(),
	NewSystemTraceInterface(),
	NewTimeserverControlInterface(),
	NewTimezoneControlInterface(),
	NewTpmInterface(),
	NewUnity7Interface(),
	NewUPowerObserveInterface(),
	NewX11Interface(),
	NewOpenglInterface(),
	NewCupsControlInterface(),
	NewOpticalDriveInterface(),
	NewCameraInterface(),
	NewBluetoothControlInterface(),
	NewKernelModuleControlInterface(),
	NewFuseSupportInterface(),
	NewLibvirtInterface(),
}

// Interfaces returns all of the built-in interfaces.
func Interfaces() []interfaces.Interface {
	return allInterfaces
}
