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
	"fmt"
	"os"
	"strconv"

	"github.com/snapcore/snapd/interfaces"
)

var gpioSysfsGpioBase = "/sys/class/gpio/gpio"
var gpioSysfsExport = "/sys/class/gpio/export"

// GpioInterface type
type GpioInterface struct{}

// String returns the same value as Name().
func (iface *GpioInterface) String() string {
	return iface.Name()
}

// Name of the GpioInterface
func (iface *GpioInterface) Name() string {
	return "gpio"
}

// SanitizeSlot checks the slot definition is valid
func (iface *GpioInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// Paranoid check this right interface type
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	// We will only allow creation of this type of slot by a gadget or OS snap
	if !(slot.Snap.Type == "gadget" || slot.Snap.Type == "os") {
		return fmt.Errorf("gpio slots only allowed on gadget or core snaps")
	}

	// Must have a GPIO number
	number, ok := slot.Attrs["number"]
	if !ok {
		return fmt.Errorf("gpio slot must have a number attribute")
	}

	// Valid values of number
	if _, ok := number.(int); !ok {
		return fmt.Errorf("gpio slot number attribute must be an int")
	}

	// Slot is good
	return nil
}

// SanitizePlug checks the plug definition is valid
func (iface *GpioInterface) SanitizePlug(plug *interfaces.Plug) error {
	// Make sure right interface type
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}

	// Plug is good
	return nil
}

// PermanentPlugSnippet returns security snippets for plug at install
func (iface *GpioInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount, interfaces.SecurityKMod:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

var reallyExportGPIOsToUserspace = true

// ConnectedPlugSnippet returns security snippets for plug at connection
func (iface *GpioInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount, interfaces.SecurityKMod:
		return nil, nil
	case interfaces.SecurityAppArmor:
		path := fmt.Sprint(gpioSysfsGpioBase, slot.Attrs["number"])
		if reallyExportGPIOsToUserspace {
			// Entries in /sys/class/gpio for single GPIO's are just symlinks
			// to their correct device part in the sysfs tree. Given AppArmor
			// requires symlinks to be dereferenced, evaluate the GPIO
			// path and add the correct absolute path to the AppArmor snippet.
			var err error
			path, err = evalSymlinks(path)
			if err != nil {
				return nil, err
			}
		} else {
			path = "/fake/path/to/gpio"
		}
		return []byte(fmt.Sprintf("%s/* rwk,\n", path)), nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// PermanentSlotSnippet - no slot snippets provided
func (iface *GpioInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount, interfaces.SecurityKMod:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *GpioInterface) exportToUserspace(gpioNum int) error {
	// Check if the gpio symlink is present, if not it needs exporting. Attempting
	// to export a gpio again will cause an error on the Write() call
	if _, err := os.Stat(fmt.Sprint(gpioSysfsGpioBase, gpioNum)); os.IsNotExist(err) {
		fileExport, err := os.OpenFile(gpioSysfsExport, os.O_WRONLY, 0200)
		if err != nil {
			return err
		}
		defer fileExport.Close()
		numBytes := []byte(strconv.Itoa(gpioNum))
		_, err = fileExport.Write(numBytes)
		if err != nil {
			return err
		}
	}
	return nil
}

// ConnectedSlotSnippet - no slot snippets provided
func (iface *GpioInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	// We need to export the GPIO so that it becomes as entry in sysfs
	// available and we can assign it to a connecting plug.
	numInt, ok := slot.Attrs["number"].(int)
	if !ok {
		return nil, fmt.Errorf("gpio slot has invalid number attribute")
	}
	if reallyExportGPIOsToUserspace {
		if err := iface.exportToUserspace(numInt); err != nil {
			return nil, err
		}
	} else {
		msg := []byte(fmt.Sprintf("# GPIO %d mock-exposed to userspace\n", numInt))
		return msg, nil
	}
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecurityDBus, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount, interfaces.SecurityKMod:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// AutoConnect returns whether interface should be auto-connected by default
func (iface *GpioInterface) AutoConnect() bool {
	return false
}
