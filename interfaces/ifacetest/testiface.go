// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2017 Canonical Ltd
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

package ifacetest

import (
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/interfaces/udev"
)

// TestInterface is a interface for various kind of tests.
// It is public so that it can be consumed from other packages.
type TestInterface struct {
	// InterfaceName is the name of this interface
	InterfaceName string
	// AutoConnectCallback is the callback invoked inside AutoConnect
	AutoConnectCallback func(*interfaces.Plug, *interfaces.Slot) bool
	// SanitizePlugCallback is the callback invoked inside SanitizePlug()
	SanitizePlugCallback func(plug *interfaces.Plug) error
	// SanitizeSlotCallback is the callback invoked inside SanitizeSlot()
	SanitizeSlotCallback func(slot *interfaces.Slot) error

	ValidatePlugCallback func(plug *interfaces.Plug, attrs map[string]interface{}) error
	ValidateSlotCallback func(slot *interfaces.Slot, attrs map[string]interface{}) error

	// Support for interacting with the test backend.

	TestConnectedPlugCallback func(spec *Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	TestConnectedSlotCallback func(spec *Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	TestPermanentPlugCallback func(spec *Specification, plug *interfaces.Plug) error
	TestPermanentSlotCallback func(spec *Specification, slot *interfaces.Slot) error

	// Support for interacting with the mount backend.

	MountConnectedPlugCallback func(spec *mount.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	MountConnectedSlotCallback func(spec *mount.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	MountPermanentPlugCallback func(spec *mount.Specification, plug *interfaces.Plug) error
	MountPermanentSlotCallback func(spec *mount.Specification, slot *interfaces.Slot) error

	// Support for interacting with the udev backend.

	UdevConnectedPlugCallback func(spec *udev.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	UdevConnectedSlotCallback func(spec *udev.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	UdevPermanentPlugCallback func(spec *udev.Specification, plug *interfaces.Plug) error
	UdevPermanentSlotCallback func(spec *udev.Specification, slot *interfaces.Slot) error

	// Support for interacting with the apparmor backend.

	AppArmorConnectedPlugCallback func(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	AppArmorConnectedSlotCallback func(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	AppArmorPermanentPlugCallback func(spec *apparmor.Specification, plug *interfaces.Plug) error
	AppArmorPermanentSlotCallback func(spec *apparmor.Specification, slot *interfaces.Slot) error

	// Support for interacting with the kmod backend.

	KModConnectedPlugCallback func(spec *kmod.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	KModConnectedSlotCallback func(spec *kmod.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	KModPermanentPlugCallback func(spec *kmod.Specification, plug *interfaces.Plug) error
	KModPermanentSlotCallback func(spec *kmod.Specification, slot *interfaces.Slot) error

	// Support for interacting with the seccomp backend.

	SecCompConnectedPlugCallback func(spec *seccomp.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	SecCompConnectedSlotCallback func(spec *seccomp.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	SecCompPermanentPlugCallback func(spec *seccomp.Specification, plug *interfaces.Plug) error
	SecCompPermanentSlotCallback func(spec *seccomp.Specification, slot *interfaces.Slot) error

	// Support for interacting with the dbus backend.

	DBusConnectedPlugCallback func(spec *dbus.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	DBusConnectedSlotCallback func(spec *dbus.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	DBusPermanentPlugCallback func(spec *dbus.Specification, plug *interfaces.Plug) error
	DBusPermanentSlotCallback func(spec *dbus.Specification, slot *interfaces.Slot) error

	// Support for interacting with the systemd backend.

	SystemdConnectedPlugCallback func(spec *systemd.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	SystemdConnectedSlotCallback func(spec *systemd.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	SystemdPermanentPlugCallback func(spec *systemd.Specification, plug *interfaces.Plug) error
	SystemdPermanentSlotCallback func(spec *systemd.Specification, slot *interfaces.Slot) error
}

// String() returns the same value as Name().
func (t *TestInterface) String() string {
	return t.Name()
}

// Name returns the name of the test interface.
func (t *TestInterface) Name() string {
	return t.InterfaceName
}

// SanitizePlug checks and possibly modifies a plug.
func (t *TestInterface) SanitizePlug(plug *interfaces.Plug) error {
	if t.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", t))
	}
	if t.SanitizePlugCallback != nil {
		return t.SanitizePlugCallback(plug)
	}
	return nil
}

// SanitizeSlot checks and possibly modifies a slot.
func (t *TestInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if t.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", t))
	}
	if t.SanitizeSlotCallback != nil {
		return t.SanitizeSlotCallback(slot)
	}
	return nil
}

func (t *TestInterface) ValidatePlug(plug *interfaces.Plug, attrs map[string]interface{}) error {
	if t.ValidatePlugCallback != nil {
		return t.ValidatePlugCallback(plug, attrs)
	}
	return nil
}

func (t *TestInterface) ValidateSlot(slot *interfaces.Slot, attrs map[string]interface{}) error {
	if t.ValidateSlotCallback != nil {
		return t.ValidateSlotCallback(slot, attrs)
	}
	return nil
}

// AutoConnect returns whether plug and slot should be implicitly
// auto-connected assuming they will be an unambiguous connection
// candidate.
func (t *TestInterface) AutoConnect(plug *interfaces.Plug, slot *interfaces.Slot) bool {
	if t.AutoConnectCallback != nil {
		return t.AutoConnectCallback(plug, slot)
	}
	return true
}

// Support for interacting with the test backend.

func (t *TestInterface) TestConnectedPlug(spec *Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if t.TestConnectedPlugCallback != nil {
		return t.TestConnectedPlugCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) TestConnectedSlot(spec *Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if t.TestConnectedSlotCallback != nil {
		return t.TestConnectedSlotCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) TestPermanentPlug(spec *Specification, plug *interfaces.Plug) error {
	if t.TestPermanentPlugCallback != nil {
		return t.TestPermanentPlugCallback(spec, plug)
	}
	return nil
}

func (t *TestInterface) TestPermanentSlot(spec *Specification, slot *interfaces.Slot) error {
	if t.TestPermanentSlotCallback != nil {
		return t.TestPermanentSlotCallback(spec, slot)
	}
	return nil
}

// Support for interacting with the mount backend.

func (t *TestInterface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if t.MountConnectedPlugCallback != nil {
		return t.MountConnectedPlugCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) MountConnectedSlot(spec *mount.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if t.MountConnectedSlotCallback != nil {
		return t.MountConnectedSlotCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) MountPermanentPlug(spec *mount.Specification, plug *interfaces.Plug) error {
	if t.MountPermanentPlugCallback != nil {
		return t.MountPermanentPlugCallback(spec, plug)
	}
	return nil
}

func (t *TestInterface) MountPermanentSlot(spec *mount.Specification, slot *interfaces.Slot) error {
	if t.MountPermanentSlotCallback != nil {
		return t.MountPermanentSlotCallback(spec, slot)
	}
	return nil
}

// Support for interacting with the udev backend.

func (t *TestInterface) UdevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if t.UdevConnectedPlugCallback != nil {
		return t.UdevConnectedPlugCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) UdevPermanentPlug(spec *udev.Specification, plug *interfaces.Plug) error {
	if t.UdevPermanentPlugCallback != nil {
		return t.UdevPermanentPlugCallback(spec, plug)
	}
	return nil
}

func (t *TestInterface) UdevPermanentSlot(spec *udev.Specification, slot *interfaces.Slot) error {
	if t.UdevPermanentSlotCallback != nil {
		return t.UdevPermanentSlotCallback(spec, slot)
	}
	return nil
}

func (t *TestInterface) UdevConnectedSlot(spec *udev.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if t.UdevConnectedSlotCallback != nil {
		return t.UdevConnectedSlotCallback(spec, plug, slot)
	}
	return nil
}

// Support for interacting with the apparmor backend.

func (t *TestInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if t.AppArmorConnectedPlugCallback != nil {
		return t.AppArmorConnectedPlugCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *interfaces.Slot) error {
	if t.AppArmorPermanentSlotCallback != nil {
		return t.AppArmorPermanentSlotCallback(spec, slot)
	}
	return nil
}

func (t *TestInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if t.AppArmorConnectedSlotCallback != nil {
		return t.AppArmorConnectedSlotCallback(spec, plug, slot)

	}
	return nil
}

func (t *TestInterface) AppArmorPermanentPlug(spec *apparmor.Specification, plug *interfaces.Plug) error {
	if t.AppArmorPermanentPlugCallback != nil {
		return t.AppArmorPermanentPlugCallback(spec, plug)
	}
	return nil
}

// Support for interacting with the seccomp backend.

func (t *TestInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if t.SecCompConnectedPlugCallback != nil {
		return t.SecCompConnectedPlugCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) SecCompConnectedSlot(spec *seccomp.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if t.SecCompConnectedSlotCallback != nil {
		return t.SecCompConnectedSlotCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *interfaces.Slot) error {
	if t.SecCompPermanentSlotCallback != nil {
		return t.SecCompPermanentSlotCallback(spec, slot)
	}
	return nil
}

func (t *TestInterface) SecCompPermanentPlug(spec *seccomp.Specification, plug *interfaces.Plug) error {
	if t.SecCompPermanentPlugCallback != nil {
		return t.SecCompPermanentPlugCallback(spec, plug)
	}
	return nil
}

// Support for interacting with the kmod backend.

func (t *TestInterface) KModConnectedPlug(spec *kmod.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if t.KModConnectedPlugCallback != nil {
		return t.KModConnectedPlugCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) KModConnectedSlot(spec *kmod.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if t.KModConnectedSlotCallback != nil {
		return t.KModConnectedSlotCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) KModPermanentPlug(spec *kmod.Specification, plug *interfaces.Plug) error {
	if t.KModPermanentPlugCallback != nil {
		return t.KModPermanentPlugCallback(spec, plug)
	}
	return nil
}

func (t *TestInterface) KModPermanentSlot(spec *kmod.Specification, slot *interfaces.Slot) error {
	if t.KModPermanentSlotCallback != nil {
		return t.KModPermanentSlotCallback(spec, slot)
	}
	return nil
}

// Support for interacting with the dbus backend.

func (t *TestInterface) DBusConnectedPlug(spec *dbus.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if t.DBusConnectedPlugCallback != nil {
		return t.DBusConnectedPlugCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) DBusConnectedSlot(spec *dbus.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if t.DBusConnectedSlotCallback != nil {
		return t.DBusConnectedSlotCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) DBusPermanentSlot(spec *dbus.Specification, slot *interfaces.Slot) error {
	if t.DBusPermanentSlotCallback != nil {
		return t.DBusPermanentSlotCallback(spec, slot)
	}
	return nil
}

func (t *TestInterface) DBusPermanentPlug(spec *dbus.Specification, plug *interfaces.Plug) error {
	if t.DBusPermanentPlugCallback != nil {
		return t.DBusPermanentPlugCallback(spec, plug)
	}
	return nil
}

// Support for interacting with the systemd backend.

func (t *TestInterface) SystemdConnectedPlug(spec *systemd.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if t.SystemdConnectedPlugCallback != nil {
		return t.SystemdConnectedPlugCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) SystemdConnectedSlot(spec *systemd.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if t.SystemdConnectedSlotCallback != nil {
		return t.SystemdConnectedSlotCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) SystemdPermanentSlot(spec *systemd.Specification, slot *interfaces.Slot) error {
	if t.SystemdPermanentSlotCallback != nil {
		return t.SystemdPermanentSlotCallback(spec, slot)
	}
	return nil
}

func (t *TestInterface) SystemdPermanentPlug(spec *systemd.Specification, plug *interfaces.Plug) error {
	if t.SystemdPermanentPlugCallback != nil {
		return t.SystemdPermanentPlugCallback(spec, plug)
	}
	return nil
}
