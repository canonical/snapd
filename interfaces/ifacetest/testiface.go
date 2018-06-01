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
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
)

// TestInterface is a interface for various kind of tests.
// It is public so that it can be consumed from other packages.
type TestInterface struct {
	// InterfaceName is the name of this interface
	InterfaceName       string
	InterfaceStaticInfo interfaces.StaticInfo
	// AutoConnectCallback is the callback invoked inside AutoConnect
	AutoConnectCallback func(*snap.PlugInfo, *snap.SlotInfo) bool
	// BeforePreparePlugCallback is the callback invoked inside BeforePreparePlug()
	BeforePreparePlugCallback func(plug *snap.PlugInfo) error
	// BeforePrepareSlotCallback is the callback invoked inside BeforePrepareSlot()
	BeforePrepareSlotCallback func(slot *snap.SlotInfo) error

	BeforeConnectPlugCallback func(plug *interfaces.ConnectedPlug) error
	BeforeConnectSlotCallback func(slot *interfaces.ConnectedSlot) error

	// Support for interacting with the test backend.

	TestConnectedPlugCallback func(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	TestConnectedSlotCallback func(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	TestPermanentPlugCallback func(spec *Specification, plug *snap.PlugInfo) error
	TestPermanentSlotCallback func(spec *Specification, slot *snap.SlotInfo) error

	// Support for interacting with the mount backend.

	MountConnectedPlugCallback func(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	MountConnectedSlotCallback func(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	MountPermanentPlugCallback func(spec *mount.Specification, plug *snap.PlugInfo) error
	MountPermanentSlotCallback func(spec *mount.Specification, slot *snap.SlotInfo) error

	// Support for interacting with the udev backend.

	UDevConnectedPlugCallback func(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	UDevConnectedSlotCallback func(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	UDevPermanentPlugCallback func(spec *udev.Specification, plug *snap.PlugInfo) error
	UDevPermanentSlotCallback func(spec *udev.Specification, slot *snap.SlotInfo) error

	// Support for interacting with the apparmor backend.

	AppArmorConnectedPlugCallback func(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	AppArmorConnectedSlotCallback func(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	AppArmorPermanentPlugCallback func(spec *apparmor.Specification, plug *snap.PlugInfo) error
	AppArmorPermanentSlotCallback func(spec *apparmor.Specification, slot *snap.SlotInfo) error

	// Support for interacting with the kmod backend.

	KModConnectedPlugCallback func(spec *kmod.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	KModConnectedSlotCallback func(spec *kmod.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	KModPermanentPlugCallback func(spec *kmod.Specification, plug *snap.PlugInfo) error
	KModPermanentSlotCallback func(spec *kmod.Specification, slot *snap.SlotInfo) error

	// Support for interacting with the seccomp backend.

	SecCompConnectedPlugCallback func(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	SecCompConnectedSlotCallback func(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	SecCompPermanentPlugCallback func(spec *seccomp.Specification, plug *snap.PlugInfo) error
	SecCompPermanentSlotCallback func(spec *seccomp.Specification, slot *snap.SlotInfo) error

	// Support for interacting with the dbus backend.

	DBusConnectedPlugCallback func(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	DBusConnectedSlotCallback func(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	DBusPermanentPlugCallback func(spec *dbus.Specification, plug *snap.PlugInfo) error
	DBusPermanentSlotCallback func(spec *dbus.Specification, slot *snap.SlotInfo) error

	// Support for interacting with the systemd backend.

	SystemdConnectedPlugCallback func(spec *systemd.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	SystemdConnectedSlotCallback func(spec *systemd.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	SystemdPermanentPlugCallback func(spec *systemd.Specification, plug *snap.PlugInfo) error
	SystemdPermanentSlotCallback func(spec *systemd.Specification, slot *snap.SlotInfo) error
}

// String() returns the same value as Name().
func (t *TestInterface) String() string {
	return t.Name()
}

// Name returns the name of the test interface.
func (t *TestInterface) Name() string {
	return t.InterfaceName
}

func (t *TestInterface) StaticInfo() interfaces.StaticInfo {
	return t.InterfaceStaticInfo
}

// BeforePreparePlug checks and possibly modifies a plug.
func (t *TestInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	if t.BeforePreparePlugCallback != nil {
		return t.BeforePreparePlugCallback(plug)
	}
	return nil
}

// BeforePrepareSlot checks and possibly modifies a slot.
func (t *TestInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	if t.BeforePrepareSlotCallback != nil {
		return t.BeforePrepareSlotCallback(slot)
	}
	return nil
}

func (t *TestInterface) BeforeConnectPlug(plug *interfaces.ConnectedPlug) error {
	if t.BeforeConnectPlugCallback != nil {
		return t.BeforeConnectPlugCallback(plug)
	}
	return nil
}

func (t *TestInterface) BeforeConnectSlot(slot *interfaces.ConnectedSlot) error {
	if t.BeforeConnectSlotCallback != nil {
		return t.BeforeConnectSlotCallback(slot)
	}
	return nil
}

// AutoConnect returns whether plug and slot should be implicitly
// auto-connected assuming they will be an unambiguous connection
// candidate.
func (t *TestInterface) AutoConnect(plug *snap.PlugInfo, slot *snap.SlotInfo) bool {
	if t.AutoConnectCallback != nil {
		return t.AutoConnectCallback(plug, slot)
	}
	return true
}

// Support for interacting with the test backend.

func (t *TestInterface) TestConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if t.TestConnectedPlugCallback != nil {
		return t.TestConnectedPlugCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) TestConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if t.TestConnectedSlotCallback != nil {
		return t.TestConnectedSlotCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) TestPermanentPlug(spec *Specification, plug *snap.PlugInfo) error {
	if t.TestPermanentPlugCallback != nil {
		return t.TestPermanentPlugCallback(spec, plug)
	}
	return nil
}

func (t *TestInterface) TestPermanentSlot(spec *Specification, slot *snap.SlotInfo) error {
	if t.TestPermanentSlotCallback != nil {
		return t.TestPermanentSlotCallback(spec, slot)
	}
	return nil
}

// Support for interacting with the mount backend.

func (t *TestInterface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if t.MountConnectedPlugCallback != nil {
		return t.MountConnectedPlugCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) MountConnectedSlot(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if t.MountConnectedSlotCallback != nil {
		return t.MountConnectedSlotCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) MountPermanentPlug(spec *mount.Specification, plug *snap.PlugInfo) error {
	if t.MountPermanentPlugCallback != nil {
		return t.MountPermanentPlugCallback(spec, plug)
	}
	return nil
}

func (t *TestInterface) MountPermanentSlot(spec *mount.Specification, slot *snap.SlotInfo) error {
	if t.MountPermanentSlotCallback != nil {
		return t.MountPermanentSlotCallback(spec, slot)
	}
	return nil
}

// Support for interacting with the udev backend.

func (t *TestInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if t.UDevConnectedPlugCallback != nil {
		return t.UDevConnectedPlugCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) UDevPermanentPlug(spec *udev.Specification, plug *snap.PlugInfo) error {
	if t.UDevPermanentPlugCallback != nil {
		return t.UDevPermanentPlugCallback(spec, plug)
	}
	return nil
}

func (t *TestInterface) UDevPermanentSlot(spec *udev.Specification, slot *snap.SlotInfo) error {
	if t.UDevPermanentSlotCallback != nil {
		return t.UDevPermanentSlotCallback(spec, slot)
	}
	return nil
}

func (t *TestInterface) UDevConnectedSlot(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if t.UDevConnectedSlotCallback != nil {
		return t.UDevConnectedSlotCallback(spec, plug, slot)
	}
	return nil
}

// Support for interacting with the apparmor backend.

func (t *TestInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if t.AppArmorConnectedPlugCallback != nil {
		return t.AppArmorConnectedPlugCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	if t.AppArmorPermanentSlotCallback != nil {
		return t.AppArmorPermanentSlotCallback(spec, slot)
	}
	return nil
}

func (t *TestInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if t.AppArmorConnectedSlotCallback != nil {
		return t.AppArmorConnectedSlotCallback(spec, plug, slot)

	}
	return nil
}

func (t *TestInterface) AppArmorPermanentPlug(spec *apparmor.Specification, plug *snap.PlugInfo) error {
	if t.AppArmorPermanentPlugCallback != nil {
		return t.AppArmorPermanentPlugCallback(spec, plug)
	}
	return nil
}

// Support for interacting with the seccomp backend.

func (t *TestInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if t.SecCompConnectedPlugCallback != nil {
		return t.SecCompConnectedPlugCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) SecCompConnectedSlot(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if t.SecCompConnectedSlotCallback != nil {
		return t.SecCompConnectedSlotCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	if t.SecCompPermanentSlotCallback != nil {
		return t.SecCompPermanentSlotCallback(spec, slot)
	}
	return nil
}

func (t *TestInterface) SecCompPermanentPlug(spec *seccomp.Specification, plug *snap.PlugInfo) error {
	if t.SecCompPermanentPlugCallback != nil {
		return t.SecCompPermanentPlugCallback(spec, plug)
	}
	return nil
}

// Support for interacting with the kmod backend.

func (t *TestInterface) KModConnectedPlug(spec *kmod.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if t.KModConnectedPlugCallback != nil {
		return t.KModConnectedPlugCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) KModConnectedSlot(spec *kmod.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if t.KModConnectedSlotCallback != nil {
		return t.KModConnectedSlotCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) KModPermanentPlug(spec *kmod.Specification, plug *snap.PlugInfo) error {
	if t.KModPermanentPlugCallback != nil {
		return t.KModPermanentPlugCallback(spec, plug)
	}
	return nil
}

func (t *TestInterface) KModPermanentSlot(spec *kmod.Specification, slot *snap.SlotInfo) error {
	if t.KModPermanentSlotCallback != nil {
		return t.KModPermanentSlotCallback(spec, slot)
	}
	return nil
}

// Support for interacting with the dbus backend.

func (t *TestInterface) DBusConnectedPlug(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if t.DBusConnectedPlugCallback != nil {
		return t.DBusConnectedPlugCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) DBusConnectedSlot(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if t.DBusConnectedSlotCallback != nil {
		return t.DBusConnectedSlotCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) DBusPermanentSlot(spec *dbus.Specification, slot *snap.SlotInfo) error {
	if t.DBusPermanentSlotCallback != nil {
		return t.DBusPermanentSlotCallback(spec, slot)
	}
	return nil
}

func (t *TestInterface) DBusPermanentPlug(spec *dbus.Specification, plug *snap.PlugInfo) error {
	if t.DBusPermanentPlugCallback != nil {
		return t.DBusPermanentPlugCallback(spec, plug)
	}
	return nil
}

// Support for interacting with the systemd backend.

func (t *TestInterface) SystemdConnectedPlug(spec *systemd.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if t.SystemdConnectedPlugCallback != nil {
		return t.SystemdConnectedPlugCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) SystemdConnectedSlot(spec *systemd.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if t.SystemdConnectedSlotCallback != nil {
		return t.SystemdConnectedSlotCallback(spec, plug, slot)
	}
	return nil
}

func (t *TestInterface) SystemdPermanentSlot(spec *systemd.Specification, slot *snap.SlotInfo) error {
	if t.SystemdPermanentSlotCallback != nil {
		return t.SystemdPermanentSlotCallback(spec, slot)
	}
	return nil
}

func (t *TestInterface) SystemdPermanentPlug(spec *systemd.Specification, plug *snap.PlugInfo) error {
	if t.SystemdPermanentPlugCallback != nil {
		return t.SystemdPermanentPlugCallback(spec, plug)
	}
	return nil
}
