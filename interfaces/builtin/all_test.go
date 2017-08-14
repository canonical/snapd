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

package builtin_test

import (
	"reflect"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/interfaces/udev"

	. "gopkg.in/check.v1"
)

type AllSuite struct{}

var _ = Suite(&AllSuite{})

// This section contains a list of *valid* defines that represent methods that
// backends recognize and call. They are in individual interfaces as each snapd
// interface can define a subset that it is interested in providing. Those are,
// essentially, the only valid methods that a snapd interface can have, apart
// from what is defined in the Interface golang interface.
type apparmorDefiner1 interface {
	AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.PlugData, slot *interfaces.SlotData) error
}
type apparmorDefiner2 interface {
	AppArmorConnestedSlot(spec *apparmor.Specification, plug *interfaces.PlugData, slot *interfaces.SlotData) error
}
type apparmorDefiner3 interface {
	AppArmorPermanentPlug(spec *apparmor.Specification, plug *interfaces.PlugData) error
}
type apparmorDefiner4 interface {
	AppArmorPermanentSlot(spec *apparmor.Specification, slot *interfaces.SlotData) error
}

type dbusDefiner1 interface {
	DBusConnectedPlug(spec *dbus.Specification, plug *interfaces.PlugData, slot *interfaces.SlotData) error
}
type dbusDefiner2 interface {
	DBusConnectedSlot(spec *dbus.Specification, plug *interfaces.PlugData, slot *interfaces.SlotData) error
}
type dbusDefiner3 interface {
	DBusPermanestPlug(spec *dbus.Specification, plug *interfaces.PlugData) error
}
type dbusDefiner4 interface {
	DBusPermanentSlot(spec *dbus.Specification, slot *interfaces.SlotData) error
}

type kmodDefiner1 interface {
	KModConnectedPlug(spec *kmod.Specification, plug *interfaces.PlugData, slot *interfaces.SlotData) error
}
type kmodDefiner2 interface {
	KModConnectedSlot(spec *kmod.Specification, plug *interfaces.PlugData, slot *interfaces.SlotData) error
}
type kmodDefiner3 interface {
	KModPermanentPlug(spec *kmod.Specification, plug *interfaces.PlugData) error
}
type kmodDefiner4 interface {
	KModPermanentSlot(spec *kmod.Specification, slot *interfaces.SlotData) error
}

type mountDefiner1 interface {
	MountConnectedPlug(spec *mount.Specification, plug *interfaces.PlugData, slot *interfaces.SlotData) error
}
type mountDefiner2 interface {
	MountConnectedSlot(spec *mount.Specification, plug *interfaces.PlugData, slot *interfaces.SlotData) error
}
type mountDefiner3 interface {
	MountPermanentPlug(spec *mount.Specification, plug *interfaces.PlugData) error
}
type mountDefiner4 interface {
	MountPermanentSlot(spec *mount.Specification, slot *interfaces.SlotData) error
}

type seccompDefiner1 interface {
	SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.PlugData, slot *interfaces.SlotData) error
}
type seccompDefiner2 interface {
	SecCompConnectedSlot(spec *seccomp.Specification, plug *interfaces.PlugData, slot *interfaces.SlotData) error
}
type seccompDefiner3 interface {
	SecCompPermanentPlug(spec *seccomp.Specification, plug *interfaces.PlugData) error
}
type seccompDefiner4 interface {
	SecCompPermanentSlot(spec *seccomp.Specification, slot *interfaces.SlotData) error
}

type systemdDefiner1 interface {
	SystemdConnectedPlug(spec *systemd.Specification, plug *interfaces.PlugData, slot *interfaces.SlotData) error
}
type systemdDefiner2 interface {
	SystemdConnectedSlot(spec *systemd.Specification, plug *interfaces.PlugData, slot *interfaces.SlotData) error
}
type systemdDefiner3 interface {
	SystemdPermanentPlug(spec *systemd.Specification, plug *interfaces.PlugData) error
}
type systemdDefiner4 interface {
	SystemdPermanentSlot(spec *systemd.Specification, slot *interfaces.SlotData) error
}

type udevDefiner1 interface {
	UDevConnectedPlug(spec *udev.Specification, plug *interfaces.PlugData, slot *interfaces.SlotData) error
}
type udevDefiner2 interface {
	UDevConnectedSlot(spec *udev.Specification, plug *interfaces.PlugData, slot *interfaces.SlotData) error
}
type udevDefiner3 interface {
	UDevPermanentPlug(spec *udev.Specification, plug *interfaces.PlugData) error
}
type udevDefiner4 interface {
	UDevPermanentSlot(spec *udev.Specification, slot *interfaces.SlotData) error
}

// allGoodDefiners contains all valid specification definers for all known backends.
var allGoodDefiners = []reflect.Type{
	// apparmor
	reflect.TypeOf((*apparmorDefiner1)(nil)).Elem(),
	reflect.TypeOf((*apparmorDefiner2)(nil)).Elem(),
	reflect.TypeOf((*apparmorDefiner3)(nil)).Elem(),
	reflect.TypeOf((*apparmorDefiner4)(nil)).Elem(),
	// dbus
	reflect.TypeOf((*dbusDefiner1)(nil)).Elem(),
	reflect.TypeOf((*dbusDefiner2)(nil)).Elem(),
	reflect.TypeOf((*dbusDefiner3)(nil)).Elem(),
	reflect.TypeOf((*dbusDefiner4)(nil)).Elem(),
	// kmod
	reflect.TypeOf((*kmodDefiner1)(nil)).Elem(),
	reflect.TypeOf((*kmodDefiner2)(nil)).Elem(),
	reflect.TypeOf((*kmodDefiner3)(nil)).Elem(),
	reflect.TypeOf((*kmodDefiner4)(nil)).Elem(),
	// mount
	reflect.TypeOf((*mountDefiner1)(nil)).Elem(),
	reflect.TypeOf((*mountDefiner2)(nil)).Elem(),
	reflect.TypeOf((*mountDefiner3)(nil)).Elem(),
	reflect.TypeOf((*mountDefiner4)(nil)).Elem(),
	// seccomp
	reflect.TypeOf((*seccompDefiner1)(nil)).Elem(),
	reflect.TypeOf((*seccompDefiner2)(nil)).Elem(),
	reflect.TypeOf((*seccompDefiner3)(nil)).Elem(),
	reflect.TypeOf((*seccompDefiner4)(nil)).Elem(),
	// systemd
	reflect.TypeOf((*systemdDefiner1)(nil)).Elem(),
	reflect.TypeOf((*systemdDefiner2)(nil)).Elem(),
	reflect.TypeOf((*systemdDefiner3)(nil)).Elem(),
	reflect.TypeOf((*systemdDefiner4)(nil)).Elem(),
	// udev
	reflect.TypeOf((*udevDefiner1)(nil)).Elem(),
	reflect.TypeOf((*udevDefiner2)(nil)).Elem(),
	reflect.TypeOf((*udevDefiner3)(nil)).Elem(),
	reflect.TypeOf((*udevDefiner4)(nil)).Elem(),
}

// Check that each interface defines at least one definer method we recognize.
func (s *AllSuite) TestEachInterfaceImplementsSomeBackendMethods(c *C) {
	for _, iface := range builtin.Interfaces() {
		bogus := true
		for _, definer := range allGoodDefiners {
			if reflect.TypeOf(iface).Implements(definer) {
				bogus = false
				break
			}
		}
		c.Assert(bogus, Equals, false,
			Commentf("interface %q does not implement any specification methods", iface.Name()))
	}
}

// pre-specification snippet functions
type snippetDefiner1 interface {
	ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, sec interfaces.SecuritySystem) error
}
type snippetDefiner2 interface {
	ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, sec interfaces.SecuritySystem) error
}
type snippetDefiner3 interface {
	PermanentPlugSnippet(plug *interfaces.Plug, sec interfaces.SecuritySystem) error
}
type snippetDefiner4 interface {
	PermanentSlotSnippet(slot *interfaces.Slot, sec interfaces.SecuritySystem) error
}

// old auto-connect function
type legacyAutoConnect interface {
	LegacyAutoConnect() bool
}

// specification definers before the introduction of PlugData/SlotData types.
type oldApparmorDefiner1 interface {
	AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
}
type oldApparmorDefiner2 interface {
	AppArmorConnestedSlot(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
}
type oldApparmorDefiner3 interface {
	AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error
}
type oldApparmorDefiner4 interface {
	AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error
}
type oldApparmorDefiner5 interface {
	AppArmorPermanentPlug(spec *apparmor.Specification, plug *interfaces.Plug) error
}
type oldApparmorDefiner6 interface {
	AppArmorPermanentSlot(spec *apparmor.Specification, slot *interfaces.Slot) error
}

type oldDbusDefiner1 interface {
	DBusConnectedPlug(spec *dbus.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
}
type oldDbusDefiner2 interface {
	DBusConnectedSlot(spec *dbus.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
}
type oldDbusDefiner3 interface {
	DBusConnectedPlug(spec *dbus.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error
}
type oldDbusDefiner4 interface {
	DBusConnectedSlot(spec *dbus.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error
}
type oldDbusDefiner5 interface {
	DBusPermanentPlug(spec *dbus.Specification, plug *interfaces.Plug) error
}
type oldDbusDefiner6 interface {
	DBusPermanentSlot(spec *dbus.Specification, slot *interfaces.Slot) error
}

type oldKmodDefiner1 interface {
	KModConnectedPlug(spec *kmod.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
}
type oldKmodDefiner2 interface {
	KModConnectedSlot(spec *kmod.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
}
type oldKmodDefiner3 interface {
	KModConnectedPlug(spec *kmod.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error
}
type oldKmodDefiner4 interface {
	KModConnectedSlot(spec *kmod.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error
}
type oldKmodDefiner5 interface {
	KModPermanentPlug(spec *kmod.Specification, plug *interfaces.Plug) error
}
type oldKmodDefiner6 interface {
	KModPermanentSlot(spec *kmod.Specification, slot *interfaces.Slot) error
}

type oldMountDefiner1 interface {
	MountConnectedPlug(spec *mount.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
}
type oldMountDefiner2 interface {
	MountConnectedSlot(spec *mount.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
}
type oldMountDefiner3 interface {
	MountConnectedPlug(spec *mount.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error
}
type oldMountDefiner4 interface {
	MountConnectedSlot(spec *mount.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error
}
type oldMountDefiner5 interface {
	MountPermanentPlug(spec *mount.Specification, plug *interfaces.Plug) error
}
type oldMountDefiner6 interface {
	MountPermanentSlot(spec *mount.Specification, slot *interfaces.Slot) error
}

type oldSeccompDefiner1 interface {
	SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
}
type oldSeccompDefiner2 interface {
	SecCompConnectedSlot(spec *seccomp.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
}
type oldSeccompDefiner3 interface {
	SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error
}
type oldSeccompDefiner4 interface {
	SecCompConnectedSlot(spec *seccomp.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error
}
type oldSeccompDefiner5 interface {
	SecCompPermanentPlug(spec *seccomp.Specification, plug *interfaces.Plug) error
}
type oldSeccompDefiner6 interface {
	SecCompPermanentSlot(spec *seccomp.Specification, slot *interfaces.Slot) error
}

type oldSystemdDefiner1 interface {
	SystemdConnectedPlug(spec *systemd.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
}
type oldSystemdDefiner2 interface {
	SystemdConnectedSlot(spec *systemd.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
}
type oldSystemdDefiner3 interface {
	SystemdConnectedPlug(spec *systemd.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error
}
type oldSystemdDefiner4 interface {
	SystemdConnectedSlot(spec *systemd.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error
}
type oldSystemdDefiner5 interface {
	SystemdPermanentPlug(spec *systemd.Specification, plug *interfaces.Plug) error
}
type oldSystemdDefiner6 interface {
	SystemdPermanentSlot(spec *systemd.Specification, slot *interfaces.Slot) error
}

type oldUdevDefiner1 interface {
	UDevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
}
type oldUdevDefiner2 interface {
	UDevConnectedSlot(spec *udev.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
}
type oldUdevDefiner3 interface {
	UDevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error
}
type oldUdevDefiner4 interface {
	UDevConnectedSlot(spec *udev.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error
}
type oldUdevDefiner5 interface {
	UDevPermanentPlug(spec *udev.Specification, plug *interfaces.Plug) error
}
type oldUdevDefiner6 interface {
	UDevPermanentSlot(spec *udev.Specification, slot *interfaces.Slot) error
}

// allBadDefiners contains all old/unused specification definers for all known backends.
var allBadDefiners = []reflect.Type{
	// pre-specification snippet methods
	reflect.TypeOf((*snippetDefiner1)(nil)).Elem(),
	reflect.TypeOf((*snippetDefiner2)(nil)).Elem(),
	reflect.TypeOf((*snippetDefiner3)(nil)).Elem(),
	reflect.TypeOf((*snippetDefiner4)(nil)).Elem(),
	// old auto-connect function
	reflect.TypeOf((*legacyAutoConnect)(nil)).Elem(),
	// pre-attribute definers
	reflect.TypeOf((*oldApparmorDefiner1)(nil)).Elem(),
	reflect.TypeOf((*oldApparmorDefiner2)(nil)).Elem(),
	reflect.TypeOf((*oldApparmorDefiner3)(nil)).Elem(),
	reflect.TypeOf((*oldApparmorDefiner4)(nil)).Elem(),
	reflect.TypeOf((*oldApparmorDefiner5)(nil)).Elem(),
	reflect.TypeOf((*oldApparmorDefiner6)(nil)).Elem(),
	reflect.TypeOf((*oldDbusDefiner1)(nil)).Elem(),
	reflect.TypeOf((*oldDbusDefiner2)(nil)).Elem(),
	reflect.TypeOf((*oldDbusDefiner3)(nil)).Elem(),
	reflect.TypeOf((*oldDbusDefiner4)(nil)).Elem(),
	reflect.TypeOf((*oldDbusDefiner5)(nil)).Elem(),
	reflect.TypeOf((*oldDbusDefiner6)(nil)).Elem(),
	reflect.TypeOf((*oldKmodDefiner1)(nil)).Elem(),
	reflect.TypeOf((*oldKmodDefiner2)(nil)).Elem(),
	reflect.TypeOf((*oldKmodDefiner3)(nil)).Elem(),
	reflect.TypeOf((*oldKmodDefiner4)(nil)).Elem(),
	reflect.TypeOf((*oldKmodDefiner5)(nil)).Elem(),
	reflect.TypeOf((*oldKmodDefiner6)(nil)).Elem(),
	reflect.TypeOf((*oldMountDefiner1)(nil)).Elem(),
	reflect.TypeOf((*oldMountDefiner2)(nil)).Elem(),
	reflect.TypeOf((*oldMountDefiner3)(nil)).Elem(),
	reflect.TypeOf((*oldMountDefiner4)(nil)).Elem(),
	reflect.TypeOf((*oldMountDefiner5)(nil)).Elem(),
	reflect.TypeOf((*oldMountDefiner6)(nil)).Elem(),
	reflect.TypeOf((*oldSeccompDefiner1)(nil)).Elem(),
	reflect.TypeOf((*oldSeccompDefiner2)(nil)).Elem(),
	reflect.TypeOf((*oldSeccompDefiner3)(nil)).Elem(),
	reflect.TypeOf((*oldSeccompDefiner4)(nil)).Elem(),
	reflect.TypeOf((*oldSeccompDefiner5)(nil)).Elem(),
	reflect.TypeOf((*oldSeccompDefiner6)(nil)).Elem(),
	reflect.TypeOf((*oldSystemdDefiner1)(nil)).Elem(),
	reflect.TypeOf((*oldSystemdDefiner2)(nil)).Elem(),
	reflect.TypeOf((*oldSystemdDefiner3)(nil)).Elem(),
	reflect.TypeOf((*oldSystemdDefiner4)(nil)).Elem(),
	reflect.TypeOf((*oldSystemdDefiner5)(nil)).Elem(),
	reflect.TypeOf((*oldSystemdDefiner6)(nil)).Elem(),
	reflect.TypeOf((*oldUdevDefiner1)(nil)).Elem(),
	reflect.TypeOf((*oldUdevDefiner2)(nil)).Elem(),
	reflect.TypeOf((*oldUdevDefiner3)(nil)).Elem(),
	reflect.TypeOf((*oldUdevDefiner4)(nil)).Elem(),
	reflect.TypeOf((*oldUdevDefiner5)(nil)).Elem(),
	reflect.TypeOf((*oldUdevDefiner6)(nil)).Elem(),
}

// Check that no interface defines older definer methods.
func (s *AllSuite) TestNoInterfaceImplementsOldBackendMethods(c *C) {
	for _, iface := range builtin.Interfaces() {
		for _, definer := range allBadDefiners {
			c.Assert(reflect.TypeOf(iface).Implements(definer), Equals, false,
				Commentf("interface %q implements old/unused methods %s", iface.Name(), definer))
		}
	}
}

func (s *AllSuite) TestRegisterIface(c *C) {
	restore := builtin.MockInterfaces(nil)
	defer restore()

	// Registering an interface works correctly.
	iface := &ifacetest.TestInterface{InterfaceName: "foo"}
	builtin.RegisterIface(iface)
	c.Assert(builtin.Interface("foo"), DeepEquals, iface)

	// Duplicates are detected.
	c.Assert(func() { builtin.RegisterIface(iface) }, PanicMatches, `cannot register duplicate interface "foo"`)
}
