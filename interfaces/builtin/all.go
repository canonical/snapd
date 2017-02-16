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
	NewClassicSupportInterface(),
	&ContentInterface{},
	&DbusInterface{},
	&DockerInterface{},
	&DockerSupportInterface{},
	&FwupdInterface{},
	&GpioInterface{},
	&HidrawInterface{},
	&I2cInterface{},
	&IioInterface{},
	&IioPortsControlInterface{},
	&LocationControlInterface{},
	&LocationObserveInterface{},
	&LxdInterface{},
	&LxdSupportInterface{},
	&MirInterface{},
	&ModemManagerInterface{},
	&MprisInterface{},
	&NetworkManagerInterface{},
	&OfonoInterface{},
	&PhysicalMemoryControlInterface{},
	&PhysicalMemoryObserveInterface{},
	&PppInterface{},
	&PulseAudioInterface{},
	&SerialPortInterface{},
	&TimeControlInterface{},
	&UDisks2Interface{},
	&UbuntuDownloadManagerInterface{},
	&UpowerObserveInterface{},
	&UhidInterface{},
	NewAccountControlInterface(),
	NewAlsaInterface(),
	NewAvahiObserveInterface(),
	NewBluetoothControlInterface(),
	NewCameraInterface(),
	NewCoreSupportInterface(),
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
	NewNetworkSetupControlInterface(),
	NewNetworkSetupObserveInterface(),
	NewOpenglInterface(),
	NewOpenvSwitchInterface(),
	NewOpenvSwitchSupportInterface(),
	NewOpticalDriveInterface(),
	NewProcessControlInterface(),
	NewRawUsbInterface(),
	NewRemovableMediaInterface(),
	NewScreenInhibitControlInterface(),
	NewShutdownInterface(),
	NewSnapdControlInterface(),
	NewSystemObserveInterface(),
	NewSystemTraceInterface(),
	NewTimeserverControlInterface(),
	NewTimezoneControlInterface(),
	NewTpmInterface(),
	NewUnity7Interface(),
	NewUnity8CalendarInterface(),
	NewUnity8ContactsInterface(),
	NewX11Interface(),
}

// Interfaces returns all of the built-in interfaces.
func Interfaces() []interfaces.Interface {
	return allInterfaces
}
