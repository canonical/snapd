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
    "github.com/ubuntu-core/snappy/interfaces"
)

type BluezInterface struct { }

func (iface *BluezInterface) Name() string {
    return "bluez"
}

func (iface *BluezInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
    switch securitySystem {
    case interfaces.SecurityDBus:
        return []byte(`
<policy user="root">
    <allow own="org.bluez"/>
    <allow send_destination="org.bluez"/>
    <allow send_interface="org.bluez.Agent1"/>
    <allow send_interface="org.bluez.ThermometerWatcher1"/>
    <allow send_interface="org.bluez.AlertAgent1"/>
    <allow send_interface="org.bluez.Profile1"/>
    <allow send_interface="org.bluez.HeartRateWatcher1"/>
    <allow send_interface="org.bluez.CyclingSpeedWatcher1"/>
    <allow send_interface="org.bluez.GattCharacteristic1"/>
    <allow send_interface="org.bluez.GattDescriptor1"/>
    <allow send_interface="org.freedesktop.DBus.ObjectManager"/>
    <allow send_interface="org.freedesktop.DBus.Properties"/>
  </policy>
  <policy at_console="true">
    <allow send_destination="org.bluez"/>
  </policy>
  <policy context="default">
    <deny send_destination="org.bluez"/>
  </policy>`), nil
    case interfaces.SecurityAppArmor, interfaces.SecuritySecComp,
interfaces.SecurityUDev:
        return nil, nil
    default:
        return nil, interfaces.ErrUnknownSecurity
    }
}

func (iface *BluezInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
    return nil, nil
}

func (iface *BluezInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
    return nil, nil
}

func (iface *BluezInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
    return nil, nil
}

func (iface *BluezInterface) SanitizePlug(slot *interfaces.Plug) error {
    return nil
}

func (iface *BluezInterface) SanitizeSlot(slot *interfaces.Slot) error {
    return nil
}
