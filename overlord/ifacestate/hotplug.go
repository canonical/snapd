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

package ifacestate

import (
	"fmt"

	"github.com/snapcore/snapd/interfaces"
)

func (m *InterfaceManager) HotplugDeviceAdded(devinfo *interfaces.HotplugDeviceInfo) {
	deviceKey := fmt.Sprintf("%s:%s:%s", devinfo.IdVendor(), devinfo.IdProduct(), devinfo.Serial())
	for _, iface := range m.repo.AllInterfaces() {
		if hotplugHandler, ok := iface.(interfaces.HotplugDeviceHandler); ok {
			spec, err := interfaces.NewHotplugSpec(deviceKey)
			if err != nil {
				// TODO: log
			}
			if hotplugHandler.HotplugDeviceAdd(devinfo, spec) != nil {
				continue
			}
		}
	}
}

func (m *InterfaceManager) HotplugDeviceRemoved(deviceInfo *interfaces.HotplugDeviceInfo) {
	// TODO
}
