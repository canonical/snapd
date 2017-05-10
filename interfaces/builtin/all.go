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
	&DbusInterface{},
	&DockerInterface{},
	&DockerSupportInterface{},
	&FramebufferInterface{},
	&FwupdInterface{},
	&GpioInterface{},
	&HardwareRandomControlInterface{},
	&HardwareRandomObserveInterface{},
	&HidrawInterface{},
	&I2cInterface{},
	&IioInterface{},
	&IioPortsControlInterface{},
	&JoystickInterface{},
	&LocationControlInterface{},
	&LocationObserveInterface{},
	&LxdInterface{},
	&LxdSupportInterface{},
	&MaliitInterface{},
	&MediaHubInterface{},
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
	&StorageFrameworkServiceInterface{},
	&ThumbnailerServiceInterface{},
	&TimeControlInterface{},
	&Unity7Interface{},
	&UDisks2Interface{},
	&UbuntuDownloadManagerInterface{},
	&UhidInterface{},
	&Unity8Interface{},
	&UpowerObserveInterface{},
	NewAccountControlInterface(),
	NewAlsaInterface(),
	NewAutopilotIntrospectionInterface(),
	NewAvahiObserveInterface(),
	NewBluetoothControlInterface(),
	NewCameraInterface(),
	NewClassicSupportInterface(),
	NewCoreSupportInterface(),
	NewCupsControlInterface(),
	NewDcdbasControlInterface(),
	NewFirewallControlInterface(),
	NewFuseSupportInterface(),
	NewGsettingsInterface(),
	NewHardwareObserveInterface(),
	NewHomeInterface(),
	NewKernelModuleControlInterface(),
	NewKubernetesSupportInterface(),
	NewLibvirtInterface(),
	NewLocaleControlInterface(),
	NewLogObserveInterface(),
	NewMountObserveInterface(),
	NewNetlinkAuditInterface(),
	NewNetlinkConnectorInterface(),
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
	NewUnity8CalendarInterface(),
	NewUnity8ContactsInterface(),
	NewX11Interface(),
}

// Interfaces returns all of the built-in interfaces.
func Interfaces() []interfaces.Interface {
	return allInterfaces
}
