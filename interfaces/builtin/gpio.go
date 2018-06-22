// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/snap"
)

const gpioSummary = `allows access to specifc GPIO pin`

const gpioBaseDeclarationSlots = `
  gpio:
    allow-installation:
      slot-snap-type:
        - core
        - gadget
    deny-auto-connection: true
`

var gpioSysfsGpioBase = "/sys/class/gpio/gpio"
var gpioSysfsExport = "/sys/class/gpio/export"

// gpioInterface type
type gpioInterface struct{}

// String returns the same value as Name().
func (iface *gpioInterface) String() string {
	return iface.Name()
}

// Name of the gpioInterface
func (iface *gpioInterface) Name() string {
	return "gpio"
}

func (iface *gpioInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              gpioSummary,
		BaseDeclarationSlots: gpioBaseDeclarationSlots,
	}
}

// BeforePrepareSlot checks the slot definition is valid
func (iface *gpioInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	if err := sanitizeSlotReservedForOSOrGadget(iface, slot); err != nil {
		return err
	}

	// Must have a GPIO number
	number, ok := slot.Attrs["number"]
	if !ok {
		return fmt.Errorf("gpio slot must have a number attribute")
	}

	// Valid values of number
	if _, ok := number.(int64); !ok {
		return fmt.Errorf("gpio slot number attribute must be an int")
	}

	// Slot is good
	return nil
}

func (iface *gpioInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var number int64
	if err := slot.Attr("number", &number); err != nil {
		return err
	}
	path := fmt.Sprint(gpioSysfsGpioBase, number)
	// Entries in /sys/class/gpio for single GPIO's are just symlinks
	// to their correct device part in the sysfs tree. Given AppArmor
	// requires symlinks to be dereferenced, evaluate the GPIO
	// path and add the correct absolute path to the AppArmor snippet.
	dereferencedPath, err := evalSymlinks(path)
	if err != nil {
		return err
	}
	spec.AddSnippet(fmt.Sprintf("%s/* rwk,", dereferencedPath))
	return nil

}

func (iface *gpioInterface) SystemdConnectedSlot(spec *systemd.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var gpioNum int64
	if err := slot.Attr("number", &gpioNum); err != nil {
		return err
	}

	serviceName := interfaces.InterfaceServiceName(slot.Snap().InstanceName(), fmt.Sprintf("gpio-%d", gpioNum))
	service := &systemd.Service{
		Type:            "oneshot",
		RemainAfterExit: true,
		ExecStart:       fmt.Sprintf("/bin/sh -c 'test -e /sys/class/gpio/gpio%d || echo %d > /sys/class/gpio/export'", gpioNum, gpioNum),
		ExecStop:        fmt.Sprintf("/bin/sh -c 'test ! -e /sys/class/gpio/gpio%d || echo %d > /sys/class/gpio/unexport'", gpioNum, gpioNum),
	}
	return spec.AddService(serviceName, service)
}

func (iface *gpioInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&gpioInterface{})
}
