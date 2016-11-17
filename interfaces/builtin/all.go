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
	&BluezInterface{},
	&BoolFileInterface{},
	&BrowserSupportInterface{},
	&ContentInterface{},
	&DockerInterface{},
	&DockerSupportInterface{},
	&FwupdInterface{},
	&GpioInterface{},
	&HidrawInterface{},
	&I2cInterface{},
	&LocationControlInterface{},
	&LocationObserveInterface{},
	&LxdInterface{},
	&LxdSupportInterface{},
	&MirInterface{},
	&ModemManagerInterface{},
	&MprisInterface{},
	&NetworkManagerInterface{},
	&OfonoInterface{},
	&PppInterface{},
	&PulseAudioInterface{},
	&SerialPortInterface{},
	&UDisks2Interface{},
	NewAlsaInterface(),
	NewAvahiObserveInterface(),
	NewBluetoothControlInterface(),
	NewCameraInterface(),
	NewCupsControlInterface(),
	NewDcdbasControlInterface(),
	NewFirewallControlInterface(),
	NewFuseSupportInterface(),
	NewGsettingsInterface(),
	NewHardwareObserveInterface(),
	NewHomeInterface(),
	NewKernelModuleControlInterface(),
	NewLibvirtInterface(),
	NewLocaleControlInterface(),
	NewLogObserveInterface(),
	NewMountObserveInterface(),
	NewNetworkBindInterface(),
	NewNetworkControlInterface(),
	NewNetworkInterface(),
	NewNetworkObserveInterface(),
	NewNetworkSetupObserveInterface(),
	NewOpenglInterface(),
	NewOpticalDriveInterface(),
	NewProcessControlInterface(),
	NewRemovableMediaInterface(),
	NewScreenInhibitControlInterface(),
	NewShutdownInterface(),
	NewSnapdControlInterface(),
	NewSystemObserveInterface(),
	NewSystemTraceInterface(),
	NewTimeControlInterface(),
	NewTimeserverControlInterface(),
	NewTimezoneControlInterface(),
	NewTpmInterface(),
	NewUPowerObserveInterface(),
	NewUnity7Interface(),
	NewUsbRawInterface(),
	NewX11Interface(),
}

// Interfaces returns all of the built-in interfaces.
func Interfaces() []interfaces.Interface {
	return allInterfaces
}
