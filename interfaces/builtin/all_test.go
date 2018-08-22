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
	"fmt"
	"reflect"
	"strings"

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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"

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
	AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
}
type apparmorDefiner2 interface {
	AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
}
type apparmorDefiner3 interface {
	AppArmorPermanentPlug(spec *apparmor.Specification, plug *snap.PlugInfo) error
}
type apparmorDefiner4 interface {
	AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error
}

type dbusDefiner1 interface {
	DBusConnectedPlug(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
}
type dbusDefiner2 interface {
	DBusConnectedSlot(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
}
type dbusDefiner3 interface {
	DBusPermanentPlug(spec *dbus.Specification, plug *snap.PlugInfo) error
}
type dbusDefiner4 interface {
	DBusPermanentSlot(spec *dbus.Specification, slot *snap.SlotInfo) error
}

type kmodDefiner1 interface {
	KModConnectedPlug(spec *kmod.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
}
type kmodDefiner2 interface {
	KModConnectedSlot(spec *kmod.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
}
type kmodDefiner3 interface {
	KModPermanentPlug(spec *kmod.Specification, plug *snap.PlugInfo) error
}
type kmodDefiner4 interface {
	KModPermanentSlot(spec *kmod.Specification, slot *snap.SlotInfo) error
}

type mountDefiner1 interface {
	MountConnectedPlug(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
}
type mountDefiner2 interface {
	MountConnectedSlot(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
}
type mountDefiner3 interface {
	MountPermanentPlug(spec *mount.Specification, plug *snap.PlugInfo) error
}
type mountDefiner4 interface {
	MountPermanentSlot(spec *mount.Specification, slot *snap.SlotInfo) error
}

type seccompDefiner1 interface {
	SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
}
type seccompDefiner2 interface {
	SecCompConnectedSlot(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
}
type seccompDefiner3 interface {
	SecCompPermanentPlug(spec *seccomp.Specification, plug *snap.PlugInfo) error
}
type seccompDefiner4 interface {
	SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error
}

type systemdDefiner1 interface {
	SystemdConnectedPlug(spec *systemd.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
}
type systemdDefiner2 interface {
	SystemdConnectedSlot(spec *systemd.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
}
type systemdDefiner3 interface {
	SystemdPermanentPlug(spec *systemd.Specification, plug *snap.PlugInfo) error
}
type systemdDefiner4 interface {
	SystemdPermanentSlot(spec *systemd.Specification, slot *snap.SlotInfo) error
}

type udevDefiner1 interface {
	UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
}
type udevDefiner2 interface {
	UDevConnectedSlot(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
}
type udevDefiner3 interface {
	UDevPermanentPlug(spec *udev.Specification, plug *snap.PlugInfo) error
}
type udevDefiner4 interface {
	UDevPermanentSlot(spec *udev.Specification, slot *snap.SlotInfo) error
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
	PermanentPlugSnippet(plug *snap.PlugInfo, sec interfaces.SecuritySystem) error
}
type snippetDefiner2 interface {
	PermanentSlotSnippet(slot *snap.SlotInfo, sec interfaces.SecuritySystem) error
}

// old auto-connect function
type legacyAutoConnect interface {
	LegacyAutoConnect() bool
}

type oldSanitizePlug1 interface {
	SanitizePlug(plug *snap.PlugInfo) error
}
type oldSanitizeSlot1 interface {
	SanitizeSlot(slot *snap.SlotInfo) error
}

// allBadDefiners contains all old/unused specification definers for all known backends.
var allBadDefiners = []reflect.Type{
	// pre-specification snippet methods
	reflect.TypeOf((*snippetDefiner1)(nil)).Elem(),
	reflect.TypeOf((*snippetDefiner2)(nil)).Elem(),
	// old auto-connect function
	reflect.TypeOf((*legacyAutoConnect)(nil)).Elem(),
	// old sanitize methods
	reflect.TypeOf((*oldSanitizePlug1)(nil)).Elem(),
	reflect.TypeOf((*oldSanitizeSlot1)(nil)).Elem(),
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

const testConsumerInvalidSlotNameYaml = `
name: consumer
version: 0
slots:
 ttyS5:
  interface: iface
apps:
    app:
        slots: [iface]
`

const testConsumerInvalidPlugNameYaml = `
name: consumer
version: 0
plugs:
 ttyS3:
  interface: iface
apps:
    app:
        plugs: [iface]
`

const testInvalidSlotInterfaceYaml = `
name: testsnap
version: 0
slots:
 iface:
  interface: iface
apps:
    app:
        slots: [iface]
hooks:
    install:
        slots: [iface]
`

const testInvalidPlugInterfaceYaml = `
name: testsnap
version: 0
plugs:
 iface:
  interface: iface
apps:
    app:
        plugs: [iface]
hooks:
    install:
        plugs: [iface]
`

func (s *AllSuite) TestSanitizeErrorsOnInvalidSlotNames(c *C) {
	restore := builtin.MockInterfaces(map[string]interfaces.Interface{
		"iface": &ifacetest.TestInterface{InterfaceName: "iface"},
	})
	defer restore()

	snapInfo := snaptest.MockInfo(c, testConsumerInvalidSlotNameYaml, nil)
	snap.SanitizePlugsSlots(snapInfo)
	c.Assert(snapInfo.BadInterfaces, HasLen, 1)
	c.Check(snap.BadInterfacesSummary(snapInfo), Matches, `snap "consumer" has bad plugs or slots: ttyS5 \(invalid slot name: "ttyS5"\)`)
}

func (s *AllSuite) TestSanitizeErrorsOnInvalidPlugNames(c *C) {
	restore := builtin.MockInterfaces(map[string]interfaces.Interface{
		"iface": &ifacetest.TestInterface{InterfaceName: "iface"},
	})
	defer restore()

	snapInfo := snaptest.MockInfo(c, testConsumerInvalidPlugNameYaml, nil)
	snap.SanitizePlugsSlots(snapInfo)
	c.Assert(snapInfo.BadInterfaces, HasLen, 1)
	c.Check(snap.BadInterfacesSummary(snapInfo), Matches, `snap "consumer" has bad plugs or slots: ttyS3 \(invalid plug name: "ttyS3"\)`)
}

func (s *AllSuite) TestSanitizeErrorsOnInvalidSlotInterface(c *C) {
	snapInfo, err := snap.InfoFromSnapYaml([]byte(testInvalidSlotInterfaceYaml))
	c.Assert(err, IsNil)
	c.Check(snapInfo.Apps["app"].Slots, HasLen, 0)
	c.Check(snapInfo.Hooks["install"].Slots, HasLen, 0)
	c.Assert(snapInfo.BadInterfaces, HasLen, 1)
	c.Check(snap.BadInterfacesSummary(snapInfo), Matches, `snap "testsnap" has bad plugs or slots: iface \(unknown interface "iface"\)`)
	c.Assert(snapInfo.Plugs, HasLen, 0)
	c.Assert(snapInfo.Slots, HasLen, 0)
}

func (s *AllSuite) TestSanitizeErrorsOnInvalidPlugInterface(c *C) {
	snapInfo := snaptest.MockInfo(c, testInvalidPlugInterfaceYaml, nil)
	c.Check(snapInfo.Apps["app"].Plugs, HasLen, 1)
	c.Check(snapInfo.Hooks["install"].Plugs, HasLen, 1)
	c.Assert(snapInfo.Plugs, HasLen, 1)
	snap.SanitizePlugsSlots(snapInfo)
	c.Assert(snapInfo.Apps["app"].Plugs, HasLen, 0)
	c.Check(snapInfo.Hooks["install"].Plugs, HasLen, 0)
	c.Assert(snapInfo.BadInterfaces, HasLen, 1)
	c.Assert(snap.BadInterfacesSummary(snapInfo), Matches, `snap "testsnap" has bad plugs or slots: iface \(unknown interface "iface"\)`)
	c.Assert(snapInfo.Plugs, HasLen, 0)
	c.Assert(snapInfo.Slots, HasLen, 0)
}

func (s *AllSuite) TestUnexpectedSpecSignatures(c *C) {
	type funcSig struct {
		name string
		in   []string
		out  []string
	}
	var sigs []funcSig

	// All the valid signatures from all the specification definers from all the backends.
	for _, backend := range []string{"AppArmor", "SecComp", "UDev", "DBus", "Systemd", "KMod"} {
		backendLower := strings.ToLower(backend)
		sigs = append(sigs, []funcSig{{
			name: fmt.Sprintf("%sPermanentPlug", backend),
			in: []string{
				fmt.Sprintf("*%s.Specification", backendLower),
				"*snap.PlugInfo",
			},
			out: []string{"error"},
		}, {
			name: fmt.Sprintf("%sPermanentSlot", backend),
			in: []string{
				fmt.Sprintf("*%s.Specification", backendLower),
				"*snap.SlotInfo",
			},
			out: []string{"error"},
		}, {
			name: fmt.Sprintf("%sConnectedPlug", backend),
			in: []string{
				fmt.Sprintf("*%s.Specification", backendLower),
				"*interfaces.ConnectedPlug",
				"*interfaces.ConnectedSlot",
			},
			out: []string{"error"},
		}, {
			name: fmt.Sprintf("%sConnectedSlot", backend),
			in: []string{
				fmt.Sprintf("*%s.Specification", backendLower),
				"*interfaces.ConnectedPlug",
				"*interfaces.ConnectedSlot",
			},
			out: []string{"error"},
		}}...)
	}
	for _, iface := range builtin.Interfaces() {
		ifaceVal := reflect.ValueOf(iface)
		ifaceType := ifaceVal.Type()
		for _, sig := range sigs {
			meth, ok := ifaceType.MethodByName(sig.name)
			if !ok {
				// all specification methods are optional.
				continue
			}
			methType := meth.Type
			// Check that the signature matches our expectation. The -1 and +1 below is for the receiver type.
			c.Assert(methType.NumIn()-1, Equals, len(sig.in), Commentf("expected %s's %s method to take %d arguments", ifaceType, meth.Name, len(sig.in)))
			for i, expected := range sig.in {
				c.Assert(methType.In(i+1).String(), Equals, expected, Commentf("expected %s's %s method %dth argument type to be different", ifaceType, meth.Name, i))
			}
			c.Assert(methType.NumOut(), Equals, len(sig.out), Commentf("expected %s's %s method to return %d values", ifaceType, meth.Name, len(sig.out)))
			for i, expected := range sig.out {
				c.Assert(methType.Out(i).String(), Equals, expected, Commentf("expected %s's %s method %dth return value type to be different", ifaceType, meth.Name, i))
			}
		}
	}
}
