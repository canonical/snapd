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

package interfaces_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	. "github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type RepositorySuite struct {
	iface     Interface
	plug      *Plug
	slot      *Slot
	coreSnap  *snap.Info
	emptyRepo *Repository
	// Repository pre-populated with s.iface
	testRepo *Repository
}

var _ = Suite(&RepositorySuite{
	iface: &ifacetest.TestInterface{
		InterfaceName: "interface",
	},
})

func (s *RepositorySuite) SetUpTest(c *C) {
	consumer := snaptest.MockInfo(c, `
name: consumer
apps:
    app:
hooks:
    configure:
plugs:
    plug:
        interface: interface
        label: label
        attr: value
`, nil)
	s.plug = &Plug{PlugInfo: consumer.Plugs["plug"]}
	producer := snaptest.MockInfo(c, `
name: producer
apps:
    app:
hooks:
    configure:
slots:
    slot:
        interface: interface
        label: label
        attr: value
`, nil)
	s.slot = &Slot{SlotInfo: producer.Slots["slot"]}
	// NOTE: The core snap has a slot so that it shows up in the
	// repository. The repository doesn't record snaps unless they
	// have at least one interface.
	s.coreSnap = snaptest.MockInfo(c, `
name: core
type: os
slots:
    network:
        interface: interface
`, nil)
	s.emptyRepo = NewRepository()
	s.testRepo = NewRepository()
	err := s.testRepo.AddInterface(s.iface)
	c.Assert(err, IsNil)
}

func addPlugsSlots(c *C, repo *Repository, yamls ...string) []*snap.Info {
	result := make([]*snap.Info, len(yamls))
	for i, yaml := range yamls {
		info := snaptest.MockInfo(c, yaml, nil)
		result[i] = info
		for _, plugInfo := range info.Plugs {
			err := repo.AddPlug(&Plug{PlugInfo: plugInfo})
			c.Assert(err, IsNil)
		}
		for _, slotInfo := range info.Slots {
			err := repo.AddSlot(&Slot{SlotInfo: slotInfo})
			c.Assert(err, IsNil)
		}
	}
	return result
}

// Tests for Repository.AddInterface()

func (s *RepositorySuite) TestAddInterface(c *C) {
	// Adding a valid interfaces works
	err := s.emptyRepo.AddInterface(s.iface)
	c.Assert(err, IsNil)
	c.Assert(s.emptyRepo.Interface(s.iface.Name()), Equals, s.iface)
}

func (s *RepositorySuite) TestAddInterfaceClash(c *C) {
	iface1 := &ifacetest.TestInterface{InterfaceName: "iface"}
	iface2 := &ifacetest.TestInterface{InterfaceName: "iface"}
	err := s.emptyRepo.AddInterface(iface1)
	c.Assert(err, IsNil)
	// Adding an interface with the same name as another interface is not allowed
	err = s.emptyRepo.AddInterface(iface2)
	c.Assert(err, ErrorMatches, `cannot add interface: "iface", interface name is in use`)
	c.Assert(s.emptyRepo.Interface(iface1.Name()), Equals, iface1)
}

func (s *RepositorySuite) TestAddInterfaceInvalidName(c *C) {
	iface := &ifacetest.TestInterface{InterfaceName: "bad-name-"}
	// Adding an interface with invalid name is not allowed
	err := s.emptyRepo.AddInterface(iface)
	c.Assert(err, ErrorMatches, `invalid interface name: "bad-name-"`)
	c.Assert(s.emptyRepo.Interface(iface.Name()), IsNil)
}

func (s *RepositorySuite) TestAddBackend(c *C) {
	backend := &ifacetest.TestSecurityBackend{BackendName: "test"}
	c.Assert(s.emptyRepo.AddBackend(backend), IsNil)
	err := s.emptyRepo.AddBackend(backend)
	c.Assert(err, ErrorMatches, `cannot add backend "test", security system name is in use`)
}

func (s *RepositorySuite) TestBackends(c *C) {
	b1 := &ifacetest.TestSecurityBackend{BackendName: "b1"}
	b2 := &ifacetest.TestSecurityBackend{BackendName: "b2"}
	c.Assert(s.emptyRepo.AddBackend(b2), IsNil)
	c.Assert(s.emptyRepo.AddBackend(b1), IsNil)
	c.Assert(s.emptyRepo.Backends(), DeepEquals, []SecurityBackend{b1, b2})
}

// Tests for Repository.Interface()

func (s *RepositorySuite) TestInterface(c *C) {
	// Interface returns nil when it cannot be found
	iface := s.emptyRepo.Interface(s.iface.Name())
	c.Assert(iface, IsNil)
	c.Assert(s.emptyRepo.Interface(s.iface.Name()), IsNil)
	err := s.emptyRepo.AddInterface(s.iface)
	c.Assert(err, IsNil)
	// Interface returns the found interface
	iface = s.emptyRepo.Interface(s.iface.Name())
	c.Assert(iface, Equals, s.iface)
}

func (s *RepositorySuite) TestInterfaceSearch(c *C) {
	ifaceA := &ifacetest.TestInterface{InterfaceName: "a"}
	ifaceB := &ifacetest.TestInterface{InterfaceName: "b"}
	ifaceC := &ifacetest.TestInterface{InterfaceName: "c"}
	err := s.emptyRepo.AddInterface(ifaceA)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddInterface(ifaceB)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddInterface(ifaceC)
	c.Assert(err, IsNil)
	// Interface correctly finds interfaces
	c.Assert(s.emptyRepo.Interface("a"), Equals, ifaceA)
	c.Assert(s.emptyRepo.Interface("b"), Equals, ifaceB)
	c.Assert(s.emptyRepo.Interface("c"), Equals, ifaceC)
}

// Tests for Repository.AddPlug()

func (s *RepositorySuite) TestAddPlug(c *C) {
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 0)
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 1)
	c.Assert(s.testRepo.Plug(s.plug.Snap.Name(), s.plug.Name), DeepEquals, s.plug)
}

func (s *RepositorySuite) TestAddPlugClash(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, ErrorMatches, `cannot add plug, snap "consumer" already has plug "plug"`)
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 1)
	c.Assert(s.testRepo.Plug(s.plug.Snap.Name(), s.plug.Name), DeepEquals, s.plug)
}

func (s *RepositorySuite) TestAddPlugFailsWithInvalidSnapName(c *C) {
	plug := &Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "bad-snap-"},
			Name:      "interface",
			Interface: "interface",
		},
	}
	err := s.testRepo.AddPlug(plug)
	c.Assert(err, ErrorMatches, `invalid snap name: "bad-snap-"`)
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddPlugFailsWithInvalidPlugName(c *C) {
	plug := &Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "snap"},
			Name:      "bad-name-",
			Interface: "interface",
		},
	}
	err := s.testRepo.AddPlug(plug)
	c.Assert(err, ErrorMatches, `invalid interface name: "bad-name-"`)
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddPlugFailsWithUnknownInterface(c *C) {
	err := s.emptyRepo.AddPlug(s.plug)
	c.Assert(err, ErrorMatches, `cannot add plug, interface "interface" is not known`)
	c.Assert(s.emptyRepo.AllPlugs(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddPlugFailsWithUnsanitizedPlug(c *C) {
	iface := &ifacetest.TestInterface{
		InterfaceName: "interface",
		SanitizePlugCallback: func(plug *Plug) error {
			return fmt.Errorf("plug is dirty")
		},
	}
	err := s.emptyRepo.AddInterface(iface)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddPlug(s.plug)
	c.Assert(err, ErrorMatches, "cannot add plug: plug is dirty")
	c.Assert(s.emptyRepo.AllPlugs(""), HasLen, 0)
}

// Tests for Repository.Plug()

func (s *RepositorySuite) TestPlug(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	c.Assert(s.emptyRepo.Plug(s.plug.Snap.Name(), s.plug.Name), IsNil)
	c.Assert(s.testRepo.Plug(s.plug.Snap.Name(), s.plug.Name), DeepEquals, s.plug)
}

func (s *RepositorySuite) TestPlugSearch(c *C) {
	addPlugsSlots(c, s.testRepo, `
name: x
plugs:
    a: interface
    b: interface
    c: interface
`, `
name: y
plugs:
    a: interface
    b: interface
    c: interface
`)
	// Plug() correctly finds plugs
	c.Assert(s.testRepo.Plug("x", "a"), Not(IsNil))
	c.Assert(s.testRepo.Plug("x", "b"), Not(IsNil))
	c.Assert(s.testRepo.Plug("x", "c"), Not(IsNil))
	c.Assert(s.testRepo.Plug("y", "a"), Not(IsNil))
	c.Assert(s.testRepo.Plug("y", "b"), Not(IsNil))
	c.Assert(s.testRepo.Plug("y", "c"), Not(IsNil))
}

// Tests for Repository.RemovePlug()

func (s *RepositorySuite) TestRemovePlugSucceedsWhenPlugExistsAndDisconnected(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.RemovePlug(s.plug.Snap.Name(), s.plug.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 0)
}

func (s *RepositorySuite) TestRemovePlugFailsWhenPlugDoesntExist(c *C) {
	err := s.emptyRepo.RemovePlug(s.plug.Snap.Name(), s.plug.Name)
	c.Assert(err, ErrorMatches, `cannot remove plug "plug" from snap "consumer", no such plug`)
}

func (s *RepositorySuite) TestRemovePlugFailsWhenPlugIsConnected(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	connRef := ConnRef{PlugRef: s.plug.Ref(), SlotRef: s.slot.Ref()}
	err = s.testRepo.Connect(connRef)
	c.Assert(err, IsNil)
	// Removing a plug used by a slot returns an appropriate error
	err = s.testRepo.RemovePlug(s.plug.Snap.Name(), s.plug.Name)
	c.Assert(err, ErrorMatches, `cannot remove plug "plug" from snap "consumer", it is still connected`)
	// The plug is still there
	slot := s.testRepo.Plug(s.plug.Snap.Name(), s.plug.Name)
	c.Assert(slot, Not(IsNil))
}

// Tests for Repository.AllPlugs()

func (s *RepositorySuite) TestAllPlugsWithoutInterfaceName(c *C) {
	snaps := addPlugsSlots(c, s.testRepo, `
name: snap-a
plugs:
    name-a: interface
`, `
name: snap-b
plugs:
    name-a: interface
    name-b: interface
    name-c: interface
`)
	// The result is sorted by snap and name
	c.Assert(s.testRepo.AllPlugs(""), DeepEquals, []*Plug{
		{PlugInfo: snaps[0].Plugs["name-a"]},
		{PlugInfo: snaps[1].Plugs["name-a"]},
		{PlugInfo: snaps[1].Plugs["name-b"]},
		{PlugInfo: snaps[1].Plugs["name-c"]},
	})
}

func (s *RepositorySuite) TestAllPlugsWithInterfaceName(c *C) {
	// Add another interface so that we can look for it
	err := s.testRepo.AddInterface(&ifacetest.TestInterface{InterfaceName: "other-interface"})
	c.Assert(err, IsNil)
	snaps := addPlugsSlots(c, s.testRepo, `
name: snap-a
plugs:
    name-a: interface
`, `
name: snap-b
plugs:
    name-a: interface
    name-b: other-interface
    name-c: interface
`)
	c.Assert(s.testRepo.AllPlugs("other-interface"), DeepEquals, []*Plug{
		{PlugInfo: snaps[1].Plugs["name-b"]},
	})
}

// Tests for Repository.Plugs()

func (s *RepositorySuite) TestPlugs(c *C) {
	snaps := addPlugsSlots(c, s.testRepo, `
name: snap-a
plugs:
    name-a: interface
`, `
name: snap-b
plugs:
    name-a: interface
    name-b: interface
    name-c: interface
`)
	// The result is sorted by snap and name
	c.Assert(s.testRepo.Plugs("snap-b"), DeepEquals, []*Plug{
		{PlugInfo: snaps[1].Plugs["name-a"]},
		{PlugInfo: snaps[1].Plugs["name-b"]},
		{PlugInfo: snaps[1].Plugs["name-c"]},
	})
	// The result is empty if the snap is not known
	c.Assert(s.testRepo.Plugs("snap-x"), HasLen, 0)
}

// Tests for Repository.AllSlots()

func (s *RepositorySuite) TestAllSlots(c *C) {
	err := s.testRepo.AddInterface(&ifacetest.TestInterface{InterfaceName: "other-interface"})
	c.Assert(err, IsNil)
	snaps := addPlugsSlots(c, s.testRepo, `
name: snap-a
slots:
    name-a: interface
    name-b: interface
`, `
name: snap-b
slots:
    name-a: other-interface
`)
	// AllSlots("") returns all slots, sorted by snap and slot name
	c.Assert(s.testRepo.AllSlots(""), DeepEquals, []*Slot{
		{SlotInfo: snaps[0].Slots["name-a"]},
		{SlotInfo: snaps[0].Slots["name-b"]},
		{SlotInfo: snaps[1].Slots["name-a"]},
	})
	// AllSlots("") returns all slots, sorted by snap and slot name
	c.Assert(s.testRepo.AllSlots("other-interface"), DeepEquals, []*Slot{
		{SlotInfo: snaps[1].Slots["name-a"]},
	})
}

// Tests for Repository.Slots()

func (s *RepositorySuite) TestSlots(c *C) {
	snaps := addPlugsSlots(c, s.testRepo, `
name: snap-a
slots:
    name-a: interface
    name-b: interface
`, `
name: snap-b
slots:
    name-a: interface
`)
	// Slots("snap-a") returns slots present in that snap
	c.Assert(s.testRepo.Slots("snap-a"), DeepEquals, []*Slot{
		{SlotInfo: snaps[0].Slots["name-a"]},
		{SlotInfo: snaps[0].Slots["name-b"]},
	})
	// Slots("snap-b") returns slots present in that snap
	c.Assert(s.testRepo.Slots("snap-b"), DeepEquals, []*Slot{
		{SlotInfo: snaps[1].Slots["name-a"]},
	})
	// Slots("snap-c") returns no slots (because that snap doesn't exist)
	c.Assert(s.testRepo.Slots("snap-c"), HasLen, 0)
	// Slots("") returns no slots
	c.Assert(s.testRepo.Slots(""), HasLen, 0)
}

// Tests for Repository.Slot()

func (s *RepositorySuite) TestSlotSucceedsWhenSlotExists(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	slot := s.testRepo.Slot(s.slot.Snap.Name(), s.slot.Name)
	c.Assert(slot, DeepEquals, s.slot)
}

func (s *RepositorySuite) TestSlotFailsWhenSlotDoesntExist(c *C) {
	slot := s.testRepo.Slot(s.slot.Snap.Name(), s.slot.Name)
	c.Assert(slot, IsNil)
}

// Tests for Repository.AddSlot()

func (s *RepositorySuite) TestAddSlotFailsWhenInterfaceIsUnknown(c *C) {
	err := s.emptyRepo.AddSlot(s.slot)
	c.Assert(err, ErrorMatches, `cannot add slot, interface "interface" is not known`)
}

func (s *RepositorySuite) TestAddSlotFailsWhenSlotNameIsInvalid(c *C) {
	slot := &Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "snap"},
			Name:      "bad-name-",
			Interface: "interface",
		},
	}
	err := s.emptyRepo.AddSlot(slot)
	c.Assert(err, ErrorMatches, `invalid interface name: "bad-name-"`)
	c.Assert(s.emptyRepo.AllSlots(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddSlotFailsWithInvalidSnapName(c *C) {
	slot := &Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "bad-snap-"},
			Name:      "slot",
			Interface: "interface",
		},
	}
	err := s.emptyRepo.AddSlot(slot)
	c.Assert(err, ErrorMatches, `invalid snap name: "bad-snap-"`)
	c.Assert(s.emptyRepo.AllSlots(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddSlotFailsForDuplicates(c *C) {
	// Adding the first slot succeeds
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Adding the slot again fails with appropriate error
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, ErrorMatches, `cannot add slot, snap "producer" already has slot "slot"`)
}

func (s *RepositorySuite) TestAddSlotFailsWithUnsanitizedSlot(c *C) {
	iface := &ifacetest.TestInterface{
		InterfaceName: "interface",
		SanitizeSlotCallback: func(slot *Slot) error {
			return fmt.Errorf("slot is dirty")
		},
	}
	err := s.emptyRepo.AddInterface(iface)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddSlot(s.slot)
	c.Assert(err, ErrorMatches, "cannot add slot: slot is dirty")
	c.Assert(s.emptyRepo.AllSlots(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddSlotStoresCorrectData(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	slot := s.testRepo.Slot(s.slot.Snap.Name(), s.slot.Name)
	// The added slot has the same data
	c.Assert(slot, DeepEquals, s.slot)
}

// Tests for Repository.RemoveSlot()

func (s *RepositorySuite) TestRemoveSlotSuccedsWhenSlotExistsAndDisconnected(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Removing a vacant slot simply works
	err = s.testRepo.RemoveSlot(s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, IsNil)
	// The slot is gone now
	slot := s.testRepo.Slot(s.slot.Snap.Name(), s.slot.Name)
	c.Assert(slot, IsNil)
}

func (s *RepositorySuite) TestRemoveSlotFailsWhenSlotDoesntExist(c *C) {
	// Removing a slot that doesn't exist returns an appropriate error
	err := s.testRepo.RemoveSlot(s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, `cannot remove slot "slot" from snap "producer", no such slot`)
}

func (s *RepositorySuite) TestRemoveSlotFailsWhenSlotIsConnected(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	connRef := ConnRef{PlugRef: s.plug.Ref(), SlotRef: s.slot.Ref()}
	err = s.testRepo.Connect(connRef)
	c.Assert(err, IsNil)
	// Removing a slot occupied by a plug returns an appropriate error
	err = s.testRepo.RemoveSlot(s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot remove slot "slot" from snap "producer", it is still connected`)
	// The slot is still there
	slot := s.testRepo.Slot(s.slot.Snap.Name(), s.slot.Name)
	c.Assert(slot, Not(IsNil))
}

// Tests for Repository.ResolveConnect()

func (s *RepositorySuite) TestResolveConnectExplicit(c *C) {
	c.Assert(s.testRepo.AddSlot(s.slot), IsNil)
	c.Assert(s.testRepo.AddPlug(s.plug), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "producer", "slot")
	c.Check(err, IsNil)
	c.Check(conn, Equals, ConnRef{
		PlugRef: PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: SlotRef{Snap: "producer", Name: "slot"},
	})
}

// ResolveConnect uses the "core" snap when slot snap name is empty
func (s *RepositorySuite) TestResolveConnectImplicitCoreSlot(c *C) {
	coreSnap := snaptest.MockInfo(c, `
name: core
type: os
slots:
    slot:
        interface: interface
`, nil)
	c.Assert(s.testRepo.AddSnap(coreSnap), IsNil)
	c.Assert(s.testRepo.AddPlug(s.plug), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "", "slot")
	c.Check(err, IsNil)
	c.Check(conn, Equals, ConnRef{
		PlugRef: PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: SlotRef{Snap: "core", Name: "slot"},
	})
}

// ResolveConnect uses the "ubuntu-core" snap when slot snap name is empty
func (s *RepositorySuite) TestResolveConnectImplicitUbuntuCoreSlot(c *C) {
	ubuntuCoreSnap := snaptest.MockInfo(c, `
name: ubuntu-core
type: os
slots:
    slot:
        interface: interface
`, nil)
	c.Assert(s.testRepo.AddSnap(ubuntuCoreSnap), IsNil)
	c.Assert(s.testRepo.AddPlug(s.plug), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "", "slot")
	c.Check(err, IsNil)
	c.Check(conn, Equals, ConnRef{
		PlugRef: PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: SlotRef{Snap: "ubuntu-core", Name: "slot"},
	})
}

// ResolveConnect prefers the "core" snap if (by any chance) both are available
func (s *RepositorySuite) TestResolveConnectImplicitSlotPrefersCore(c *C) {
	coreSnap := snaptest.MockInfo(c, `
name: core
type: os
slots:
    slot:
        interface: interface
`, nil)
	ubuntuCoreSnap := snaptest.MockInfo(c, `
name: ubuntu-core
type: os
slots:
    slot:
        interface: interface
`, nil)
	c.Assert(s.testRepo.AddSnap(coreSnap), IsNil)
	c.Assert(s.testRepo.AddSnap(ubuntuCoreSnap), IsNil)
	c.Assert(s.testRepo.AddPlug(s.plug), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "", "slot")
	c.Check(err, IsNil)
	c.Check(conn.SlotRef.Snap, Equals, "core")
}

// ResolveConnect detects lack of candidates
func (s *RepositorySuite) TestResolveConnectNoImplicitCandidates(c *C) {
	err := s.testRepo.AddInterface(&ifacetest.TestInterface{InterfaceName: "other-interface"})
	c.Assert(err, IsNil)
	coreSnap := snaptest.MockInfo(c, `
name: core
type: os
slots:
    slot:
        interface: other-interface
`, nil)
	c.Assert(s.testRepo.AddSnap(coreSnap), IsNil)
	c.Assert(s.testRepo.AddPlug(s.plug), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "", "")
	c.Check(err, ErrorMatches, `snap "core" has no "interface" interface slots`)
	c.Check(conn, Equals, ConnRef{})
}

// ResolveConnect detects ambiguities when slot snap name is empty
func (s *RepositorySuite) TestResolveConnectAmbiguity(c *C) {
	coreSnap := snaptest.MockInfo(c, `
name: core
type: os
slots:
    slot-a:
        interface: interface
    slot-b:
        interface: interface
`, nil)
	c.Assert(s.testRepo.AddSnap(coreSnap), IsNil)
	c.Assert(s.testRepo.AddSlot(s.slot), IsNil)
	c.Assert(s.testRepo.AddPlug(s.plug), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "", "")
	c.Check(err, ErrorMatches, `snap "core" has multiple "interface" interface slots: slot-a, slot-b`)
	c.Check(conn, Equals, ConnRef{})
}

// Pug snap name cannot be empty
func (s *RepositorySuite) TestResolveConnectEmptyPlugSnapName(c *C) {
	conn, err := s.testRepo.ResolveConnect("", "plug", "producer", "slot")
	c.Check(err, ErrorMatches, "cannot resolve connection, plug snap name is empty")
	c.Check(conn, Equals, ConnRef{})
}

// Plug name cannot be empty
func (s *RepositorySuite) TestResolveConnectEmptyPlugName(c *C) {
	conn, err := s.testRepo.ResolveConnect("consumer", "", "producer", "slot")
	c.Check(err, ErrorMatches, "cannot resolve connection, plug name is empty")
	c.Check(conn, Equals, ConnRef{})
}

// Plug must exist
func (s *RepositorySuite) TestResolveNoSuchPlug(c *C) {
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "consumer", "slot")
	c.Check(err, ErrorMatches, `snap "consumer" has no plug named "plug"`)
	c.Check(conn, Equals, ConnRef{})
}

// Slot snap name cannot be empty if there's no core snap around
func (s *RepositorySuite) TestResolveConnectEmptySlotSnapName(c *C) {
	c.Assert(s.testRepo.AddPlug(s.plug), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "", "slot")
	c.Check(err, ErrorMatches, "cannot resolve connection, slot snap name is empty")
	c.Check(conn, Equals, ConnRef{})
}

// Slot name cannot be empty if there's no core snap around
func (s *RepositorySuite) TestResolveConnectEmptySlotName(c *C) {
	c.Assert(s.testRepo.AddPlug(s.plug), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "producer", "")
	c.Check(err, ErrorMatches, `snap "producer" has no "interface" interface slots`)
	c.Check(conn, Equals, ConnRef{})
}

// Slot must exists
func (s *RepositorySuite) TestResolveNoSuchSlot(c *C) {
	c.Assert(s.testRepo.AddPlug(s.plug), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "producer", "slot")
	c.Check(err, ErrorMatches, `snap "producer" has no slot named "slot"`)
	c.Check(conn, Equals, ConnRef{})
}

// Plug and slot must have matching types
func (s *RepositorySuite) TestResolveIncompatibleTypes(c *C) {
	c.Assert(s.testRepo.AddInterface(&ifacetest.TestInterface{InterfaceName: "other-interface"}), IsNil)
	plug := &Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "consumer"},
			Name:      "plug",
			Interface: "other-interface",
		},
	}
	c.Assert(s.testRepo.AddPlug(plug), IsNil)
	c.Assert(s.testRepo.AddSlot(s.slot), IsNil)
	// Connecting a plug to an incompatible slot fails with an appropriate error
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "producer", "slot")
	c.Check(err, ErrorMatches,
		`cannot connect consumer:plug \("other-interface" interface\) to producer:slot \("interface" interface\)`)
	c.Check(conn, Equals, ConnRef{})
}

// Tests for Repository.ResolveDisconnect()

// All the was to resolve a 'snap disconnect' between two snaps.
// The actual snaps are not installed though.
func (s *RepositorySuite) TestResolveDisconnectMatrixNoSnaps(c *C) {
	scenarios := []struct {
		plugSnapName, plugName, slotSnapName, slotName string
		errMsg                                         string
	}{
		// Case 0 (INVALID)
		// Nothing is provided
		{"", "", "", "", "allowed forms are .*"},
		// Case 1 (FAILURE)
		// Disconnect anything connected to a specific plug or slot.
		// The snap name is implicit and refers to the core snap.
		{"", "", "", "slot", `snap "core" has no plug or slot named "slot"`},
		// Case 2 (INVALID)
		// The slot name is not provided.
		{"", "", "producer", "", "allowed forms are .*"},
		// Case 3 (FAILURE)
		// Disconnect anything connected to a specific plug or slot
		{"", "", "producer", "slot", `snap "producer" has no plug or slot named "slot"`},
		// Case 4 (FAILURE)
		// Disconnect everything from a specific plug or slot.
		// The plug side implicit refers to the core snap.
		{"", "plug", "", "", `snap "core" has no plug or slot named "plug"`},
		// Case 5 (FAILURE)
		// Disconnect a specific connection.
		// The plug and slot side implicit refers to the core snap.
		{"", "plug", "", "slot", `snap "core" has no plug named "plug"`},
		// Case 6 (INVALID)
		// Slot name is not provided.
		{"", "plug", "producer", "", "allowed forms are .*"},
		// Case 7 (FAILURE)
		// Disconnect a specific connection.
		// The plug side implicit refers to the core snap.
		{"", "plug", "producer", "slot", `snap "core" has no plug named "plug"`},
		// Case 8 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "", "allowed forms are .*"},
		// Case 9 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "slot", "allowed forms are .*"},
		// Case 10 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "", "allowed forms are .*"},
		// Case 11 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "slot", "allowed forms are .*"},
		// Case 12 (FAILURE)
		// Disconnect anything connected to a specific plug
		{"consumer", "plug", "", "", `snap "consumer" has no plug or slot named "plug"`},
		// Case 13 (FAILURE)
		// Disconnect a specific connection.
		// The snap name is implicit and refers to the core snap.
		{"consumer", "plug", "", "slot", `snap "consumer" has no plug named "plug"`},
		// Case 14 (INVALID)
		// The slot name was not provided.
		{"consumer", "plug", "producer", "", "allowed forms are .*"},
		// Case 15 (FAILURE)
		// Disconnect a specific connection.
		{"consumer", "plug", "producer", "slot", `snap "consumer" has no plug named "plug"`},
	}
	for i, scenario := range scenarios {
		c.Logf("checking scenario %d: %q", i, scenario)
		connRefList, err := s.testRepo.ResolveDisconnect(
			scenario.plugSnapName, scenario.plugName, scenario.slotSnapName, scenario.slotName)
		c.Check(err, ErrorMatches, scenario.errMsg)
		c.Check(connRefList, HasLen, 0)
	}
}

// All the was to resolve a 'snap disconnect' between two snaps.
// The actual snaps are not installed though but a core snap is.
func (s *RepositorySuite) TestResolveDisconnectMatrixJustCoreSnap(c *C) {
	c.Assert(s.testRepo.AddSnap(s.coreSnap), IsNil)
	scenarios := []struct {
		plugSnapName, plugName, slotSnapName, slotName string
		errMsg                                         string
	}{
		// Case 0 (INVALID)
		// Nothing is provided
		{"", "", "", "", "allowed forms are .*"},
		// Case 1 (FAILURE)
		// Disconnect anything connected to a specific plug or slot.
		// The snap name is implicit and refers to the core snap.
		{"", "", "", "slot", `snap "core" has no plug or slot named "slot"`},
		// Case 2 (INVALID)
		// The slot name is not provided.
		{"", "", "producer", "", "allowed forms are .*"},
		// Case 3 (FAILURE)
		// Disconnect anything connected to a specific plug or slot
		{"", "", "producer", "slot", `snap "producer" has no plug or slot named "slot"`},
		// Case 4 (FAILURE)
		// Disconnect anything connected to a specific plug or slot
		{"", "plug", "", "", `snap "core" has no plug or slot named "plug"`},
		// Case 5 (FAILURE)
		// Disconnect a specific connection.
		// The plug and slot side implicit refers to the core snap.
		{"", "plug", "", "slot", `snap "core" has no plug named "plug"`},
		// Case 6 (INVALID)
		// Slot name is not provided.
		{"", "plug", "producer", "", "allowed forms are .*"},
		// Case 7 (FAILURE)
		// Disconnect a specific connection.
		// The plug side implicit refers to the core snap.
		{"", "plug", "producer", "slot", `snap "core" has no plug named "plug"`},
		// Case 8 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "", "allowed forms are .*"},
		// Case 9 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "slot", "allowed forms are .*"},
		// Case 10 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "", "allowed forms are .*"},
		// Case 11 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "slot", "allowed forms are .*"},
		// Case 12 (FAILURE)
		// Disconnect anything connected to a specific plug or slot.
		{"consumer", "plug", "", "", `snap "consumer" has no plug or slot named "plug"`},
		// Case 13 (FAILURE)
		// Disconnect a specific connection.
		// The snap name is implicit and refers to the core snap.
		{"consumer", "plug", "", "slot", `snap "consumer" has no plug named "plug"`},
		// Case 14 (INVALID)
		// The slot name was not provided.
		{"consumer", "plug", "producer", "", "allowed forms are .*"},
		// Case 15 (FAILURE)
		// Disconnect a specific connection.
		{"consumer", "plug", "producer", "slot", `snap "consumer" has no plug named "plug"`},
	}
	for i, scenario := range scenarios {
		c.Logf("checking scenario %d: %q", i, scenario)
		connRefList, err := s.testRepo.ResolveDisconnect(
			scenario.plugSnapName, scenario.plugName, scenario.slotSnapName, scenario.slotName)
		c.Check(err, ErrorMatches, scenario.errMsg)
		c.Check(connRefList, HasLen, 0)
	}
}

// All the was to resolve a 'snap disconnect' between two snaps.
// The actual snaps as well as the core snap are installed.
// The snaps are not connected.
func (s *RepositorySuite) TestResolveDisconnectMatrixDisconnectedSnaps(c *C) {
	c.Assert(s.testRepo.AddSnap(s.coreSnap), IsNil)
	c.Assert(s.testRepo.AddPlug(s.plug), IsNil)
	c.Assert(s.testRepo.AddSlot(s.slot), IsNil)
	scenarios := []struct {
		plugSnapName, plugName, slotSnapName, slotName string
		errMsg                                         string
	}{
		// Case 0 (INVALID)
		// Nothing is provided
		{"", "", "", "", "allowed forms are .*"},
		// Case 1 (FAILURE)
		// Disconnect anything connected to a specific plug or slot.
		// The snap name is implicit and refers to the core snap.
		{"", "", "", "slot", `snap "core" has no plug or slot named "slot"`},
		// Case 2 (INVALID)
		// The slot name is not provided.
		{"", "", "producer", "", "allowed forms are .*"},
		// Case 3 (SUCCESS)
		// Disconnect anything connected to a specific plug or slot
		{"", "", "producer", "slot", ""},
		// Case 4 (FAILURE)
		// Disconnect anything connected to a specific plug or slot.
		// The plug side implicit refers to the core snap.
		{"", "plug", "", "", `snap "core" has no plug or slot named "plug"`},
		// Case 5 (FAILURE)
		// Disconnect a specific connection.
		// The plug and slot side implicit refers to the core snap.
		{"", "plug", "", "slot", `snap "core" has no plug named "plug"`},
		// Case 6 (INVALID)
		// Slot name is not provided.
		{"", "plug", "producer", "", "allowed forms are .*"},
		// Case 7 (FAILURE)
		// Disconnect a specific connection.
		// The plug side implicit refers to the core snap.
		{"", "plug", "producer", "slot", `snap "core" has no plug named "plug"`},
		// Case 8 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "", "allowed forms are .*"},
		// Case 9 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "slot", "allowed forms are .*"},
		// Case 10 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "", "allowed forms are .*"},
		// Case 11 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "slot", "allowed forms are .*"},
		// Case 12 (SUCCESS)
		// Disconnect anything connected to a specific plug or slot.
		{"consumer", "plug", "", "", ""},
		// Case 13 (FAILURE)
		// Disconnect a specific connection.
		// The snap name is implicit and refers to the core snap.
		{"consumer", "plug", "", "slot", `snap "core" has no slot named "slot"`},
		// Case 14 (INVALID)
		// The slot name was not provided.
		{"consumer", "plug", "producer", "", "allowed forms are .*"},
		// Case 15 (FAILURE)
		// Disconnect a specific connection (but it is not connected).
		{"consumer", "plug", "producer", "slot", `cannot disconnect consumer:plug from producer:slot, it is not connected`},
	}
	for i, scenario := range scenarios {
		c.Logf("checking scenario %d: %q", i, scenario)
		connRefList, err := s.testRepo.ResolveDisconnect(
			scenario.plugSnapName, scenario.plugName, scenario.slotSnapName, scenario.slotName)
		if scenario.errMsg != "" {
			c.Check(err, ErrorMatches, scenario.errMsg)
		} else {
			c.Check(err, IsNil)
		}
		c.Check(connRefList, HasLen, 0)
	}
}

// All the was to resolve a 'snap disconnect' between two snaps.
// The actual snaps as well as the core snap are installed.
// The snaps are connected.
func (s *RepositorySuite) TestResolveDisconnectMatrixTypical(c *C) {
	c.Assert(s.testRepo.AddSnap(s.coreSnap), IsNil)
	c.Assert(s.testRepo.AddPlug(s.plug), IsNil)
	c.Assert(s.testRepo.AddSlot(s.slot), IsNil)
	connRef := ConnRef{s.plug.Ref(), s.slot.Ref()}
	c.Assert(s.testRepo.Connect(connRef), IsNil)

	scenarios := []struct {
		plugSnapName, plugName, slotSnapName, slotName string
		errMsg                                         string
	}{
		// Case 0 (INVALID)
		// Nothing is provided
		{"", "", "", "", "allowed forms are .*"},
		// Case 1 (FAILURE)
		// Disconnect anything connected to a specific plug or slot.
		// The snap name is implicit and refers to the core snap.
		{"", "", "", "slot", `snap "core" has no plug or slot named "slot"`},
		// Case 2 (INVALID)
		// The slot name is not provided.
		{"", "", "producer", "", "allowed forms are .*"},
		// Case 3 (SUCCESS)
		// Disconnect anything connected to a specific plug or slot
		{"", "", "producer", "slot", ""},
		// Case 4 (FAILURE)
		// Disconnect anything connected to a specific plug or slot.
		// The plug side implicit refers to the core snap.
		{"", "plug", "", "", `snap "core" has no plug or slot named "plug"`},
		// Case 5 (FAILURE)
		// Disconnect a specific connection.
		// The plug and slot side implicit refers to the core snap.
		{"", "plug", "", "slot", `snap "core" has no plug named "plug"`},
		// Case 6 (INVALID)
		// Slot name is not provided.
		{"", "plug", "producer", "", "allowed forms are .*"},
		// Case 7 (FAILURE)
		// Disconnect a specific connection.
		// The plug side implicit refers to the core snap.
		{"", "plug", "producer", "slot", `snap "core" has no plug named "plug"`},
		// Case 8 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "", "allowed forms are .*"},
		// Case 9 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "slot", "allowed forms are .*"},
		// Case 10 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "", "allowed forms are .*"},
		// Case 11 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "slot", "allowed forms are .*"},
		// Case 12 (SUCCESS)
		// Disconnect anything connected to a specific plug or slot.
		{"consumer", "plug", "", "", ""},
		// Case 13 (FAILURE)
		// Disconnect a specific connection.
		// The snap name is implicit and refers to the core snap.
		{"consumer", "plug", "", "slot", `snap "core" has no slot named "slot"`},
		// Case 14 (INVALID)
		// The slot name was not provided.
		{"consumer", "plug", "producer", "", "allowed forms are .*"},
		// Case 15 (SUCCESS)
		// Disconnect a specific connection.
		{"consumer", "plug", "producer", "slot", ""},
	}
	for i, scenario := range scenarios {
		c.Logf("checking scenario %d: %q", i, scenario)
		connRefList, err := s.testRepo.ResolveDisconnect(
			scenario.plugSnapName, scenario.plugName, scenario.slotSnapName, scenario.slotName)
		if scenario.errMsg != "" {
			c.Check(err, ErrorMatches, scenario.errMsg)
			c.Check(connRefList, HasLen, 0)
		} else {
			c.Check(err, IsNil)
			c.Check(connRefList, DeepEquals, []ConnRef{connRef})
		}
	}
}

// Tests for Repository.Connect()

func (s *RepositorySuite) TestConnectFailsWhenPlugDoesNotExist(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Connecting an unknown plug returns an appropriate error
	connRef := ConnRef{PlugRef: s.plug.Ref(), SlotRef: s.slot.Ref()}
	err = s.testRepo.Connect(connRef)
	c.Assert(err, ErrorMatches, `cannot connect plug "plug" from snap "consumer", no such plug`)
}

func (s *RepositorySuite) TestConnectFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Connecting to an unknown slot returns an error
	connRef := ConnRef{PlugRef: s.plug.Ref(), SlotRef: s.slot.Ref()}
	err = s.testRepo.Connect(connRef)
	c.Assert(err, ErrorMatches, `cannot connect plug to slot "slot" from snap "producer", no such slot`)
}

func (s *RepositorySuite) TestConnectSucceedsWhenIdenticalConnectExists(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	connRef := ConnRef{PlugRef: s.plug.Ref(), SlotRef: s.slot.Ref()}
	err = s.testRepo.Connect(connRef)
	c.Assert(err, IsNil)
	// Connecting exactly the same thing twice succeeds without an error but does nothing.
	err = s.testRepo.Connect(connRef)
	c.Assert(err, IsNil)
	// Only one connection is actually present.
	c.Assert(s.testRepo.Interfaces(), DeepEquals, &Interfaces{
		Plugs: []*Plug{{
			PlugInfo:    s.plug.PlugInfo,
			Connections: []SlotRef{{Snap: s.slot.Snap.Name(), Name: s.slot.Name}},
		}},
		Slots: []*Slot{{
			SlotInfo:    s.slot.SlotInfo,
			Connections: []PlugRef{{Snap: s.plug.Snap.Name(), Name: s.plug.Name}},
		}},
	})
}

func (s *RepositorySuite) TestConnectFailsWhenSlotAndPlugAreIncompatible(c *C) {
	otherInterface := &ifacetest.TestInterface{InterfaceName: "other-interface"}
	err := s.testRepo.AddInterface(otherInterface)
	plug := &Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "consumer"},
			Name:      "plug",
			Interface: "other-interface",
		},
	}
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Connecting a plug to an incompatible slot fails with an appropriate error
	connRef := ConnRef{PlugRef: s.plug.Ref(), SlotRef: s.slot.Ref()}
	err = s.testRepo.Connect(connRef)
	c.Assert(err, ErrorMatches, `cannot connect plug "consumer:plug" \(interface "other-interface"\) to "producer:slot" \(interface "interface"\)`)
}

func (s *RepositorySuite) TestConnectSucceeds(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Connecting a plug works okay
	connRef := ConnRef{PlugRef: s.plug.Ref(), SlotRef: s.slot.Ref()}
	err = s.testRepo.Connect(connRef)
	c.Assert(err, IsNil)
}

// Tests for Repository.Disconnect() and DisconnectAll()

// Disconnect fails if any argument is empty
func (s *RepositorySuite) TestDisconnectFailsOnEmptyArgs(c *C) {
	err1 := s.testRepo.Disconnect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), "")
	err2 := s.testRepo.Disconnect(s.plug.Snap.Name(), s.plug.Name, "", s.slot.Name)
	err3 := s.testRepo.Disconnect(s.plug.Snap.Name(), "", s.slot.Snap.Name(), s.slot.Name)
	err4 := s.testRepo.Disconnect("", s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err1, ErrorMatches, `cannot disconnect, slot name is empty`)
	c.Assert(err2, ErrorMatches, `cannot disconnect, slot snap name is empty`)
	c.Assert(err3, ErrorMatches, `cannot disconnect, plug name is empty`)
	c.Assert(err4, ErrorMatches, `cannot disconnect, plug snap name is empty`)
}

// Disconnect fails if plug doesn't exist
func (s *RepositorySuite) TestDisconnectFailsWithoutPlug(c *C) {
	c.Assert(s.testRepo.AddSlot(s.slot), IsNil)
	err := s.testRepo.Disconnect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, ErrorMatches, `snap "consumer" has no plug named "plug"`)
}

// Disconnect fails if slot doesn't exist
func (s *RepositorySuite) TestDisconnectFailsWithutSlot(c *C) {
	c.Assert(s.testRepo.AddPlug(s.plug), IsNil)
	err := s.testRepo.Disconnect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, ErrorMatches, `snap "producer" has no slot named "slot"`)
}

// Disconnect fails if there's no connection to disconnect
func (s *RepositorySuite) TestDisconnectFailsWhenNotConnected(c *C) {
	c.Assert(s.testRepo.AddPlug(s.plug), IsNil)
	c.Assert(s.testRepo.AddSlot(s.slot), IsNil)
	err := s.testRepo.Disconnect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect consumer:plug from producer:slot, it is not connected`)
}

// Disconnect works when plug and slot exist and are connected
func (s *RepositorySuite) TestDisconnectSucceeds(c *C) {
	c.Assert(s.testRepo.AddPlug(s.plug), IsNil)
	c.Assert(s.testRepo.AddSlot(s.slot), IsNil)
	c.Assert(s.testRepo.Connect(ConnRef{PlugRef: s.plug.Ref(), SlotRef: s.slot.Ref()}), IsNil)
	err := s.testRepo.Disconnect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.Interfaces(), DeepEquals, &Interfaces{
		Plugs: []*Plug{{PlugInfo: s.plug.PlugInfo}},
		Slots: []*Slot{{SlotInfo: s.slot.SlotInfo}},
	})
}

// Tests for Repository.Connected

// Connected fails if snap name is empty and there's no core snap around
func (s *RepositorySuite) TestConnectedFailsWithEmptySnapName(c *C) {
	_, err := s.testRepo.Connected("", s.plug.Name)
	c.Check(err, ErrorMatches, "snap name is empty")
}

// Connected fails if plug or slot name is empty
func (s *RepositorySuite) TestConnectedFailsWithEmptyPlugSlotName(c *C) {
	_, err := s.testRepo.Connected(s.plug.Snap.Name(), "")
	c.Check(err, ErrorMatches, "plug or slot name is empty")
}

// Connected fails if plug or slot doesn't exist
func (s *RepositorySuite) TestConnectedFailsWithoutPlugOrSlot(c *C) {
	_, err1 := s.testRepo.Connected(s.plug.Snap.Name(), s.plug.Name)
	_, err2 := s.testRepo.Connected(s.slot.Snap.Name(), s.slot.Name)
	c.Check(err1, ErrorMatches, `snap "consumer" has no plug or slot named "plug"`)
	c.Check(err2, ErrorMatches, `snap "producer" has no plug or slot named "slot"`)
}

// Connected finds connections when asked from plug or from slot side
func (s *RepositorySuite) TestConnectedFindsConnections(c *C) {
	c.Assert(s.testRepo.AddPlug(s.plug), IsNil)
	c.Assert(s.testRepo.AddSlot(s.slot), IsNil)
	c.Assert(s.testRepo.Connect(ConnRef{PlugRef: s.plug.Ref(), SlotRef: s.slot.Ref()}), IsNil)

	conns, err := s.testRepo.Connected(s.plug.Snap.Name(), s.plug.Name)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, []ConnRef{
		{s.plug.Ref(), s.slot.Ref()},
	})

	conns, err = s.testRepo.Connected(s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, []ConnRef{
		{s.plug.Ref(), s.slot.Ref()},
	})
}

// Connected uses the core snap if snap name is empty
func (s *RepositorySuite) TestConnectedFindsCoreSnap(c *C) {
	slot := &Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
			Name:      "slot",
			Interface: "interface",
		},
	}
	c.Assert(s.testRepo.AddPlug(s.plug), IsNil)
	c.Assert(s.testRepo.AddSlot(slot), IsNil)
	c.Assert(s.testRepo.Connect(ConnRef{PlugRef: s.plug.Ref(), SlotRef: slot.Ref()}), IsNil)

	conns, err := s.testRepo.Connected("", s.slot.Name)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, []ConnRef{
		{s.plug.Ref(), slot.Ref()},
	})
}

// Tests for Repository.DisconnectAll()

func (s *RepositorySuite) TestDisconnectAll(c *C) {
	c.Assert(s.testRepo.AddPlug(s.plug), IsNil)
	c.Assert(s.testRepo.AddSlot(s.slot), IsNil)
	c.Assert(s.testRepo.Connect(ConnRef{PlugRef: s.plug.Ref(), SlotRef: s.slot.Ref()}), IsNil)

	conns := []ConnRef{{s.plug.Ref(), s.slot.Ref()}}
	s.testRepo.DisconnectAll(conns)
	c.Assert(s.testRepo.Interfaces(), DeepEquals, &Interfaces{
		Plugs: []*Plug{{PlugInfo: s.plug.PlugInfo}},
		Slots: []*Slot{{SlotInfo: s.slot.SlotInfo}},
	})
}

// Tests for Repository.Interfaces()

func (s *RepositorySuite) TestInterfacesSmokeTest(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// After connecting the result is as expected
	connRef := ConnRef{PlugRef: s.plug.Ref(), SlotRef: s.slot.Ref()}
	err = s.testRepo.Connect(connRef)
	c.Assert(err, IsNil)
	ifaces := s.testRepo.Interfaces()
	c.Assert(ifaces, DeepEquals, &Interfaces{
		Plugs: []*Plug{{
			PlugInfo:    s.plug.PlugInfo,
			Connections: []SlotRef{{Snap: s.slot.Snap.Name(), Name: s.slot.Name}},
		}},
		Slots: []*Slot{{
			SlotInfo:    s.slot.SlotInfo,
			Connections: []PlugRef{{Snap: s.plug.Snap.Name(), Name: s.plug.Name}},
		}},
	})
	// After disconnecting the connections become empty
	err = s.testRepo.Disconnect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, IsNil)
	ifaces = s.testRepo.Interfaces()
	c.Assert(ifaces, DeepEquals, &Interfaces{
		Plugs: []*Plug{{PlugInfo: s.plug.PlugInfo}},
		Slots: []*Slot{{SlotInfo: s.slot.SlotInfo}},
	})
}

// Tests for Repository.SnapSpecification

const testSecurity SecuritySystem = "test"

var testInterface = &ifacetest.TestInterface{
	InterfaceName: "interface",
	TestPermanentPlugCallback: func(spec *ifacetest.Specification, plug *Plug) error {
		spec.AddSnippet("static plug snippet")
		return nil
	},
	TestConnectedPlugCallback: func(spec *ifacetest.Specification, plug *Plug, plugAttrs map[string]interface{}, slot *Slot, slotAttrs map[string]interface{}) error {
		spec.AddSnippet("connection-specific plug snippet")
		return nil
	},
	TestPermanentSlotCallback: func(spec *ifacetest.Specification, slot *Slot) error {
		spec.AddSnippet("static slot snippet")
		return nil
	},
	TestConnectedSlotCallback: func(spec *ifacetest.Specification, plug *Plug, plugAttrs map[string]interface{}, slot *Slot, slotAttrs map[string]interface{}) error {
		spec.AddSnippet("connection-specific slot snippet")
		return nil
	},
}

func (s *RepositorySuite) TestSnapSpecification(c *C) {
	repo := s.emptyRepo
	backend := &ifacetest.TestSecurityBackend{BackendName: testSecurity}
	c.Assert(repo.AddBackend(backend), IsNil)
	c.Assert(repo.AddInterface(testInterface), IsNil)
	c.Assert(repo.AddPlug(s.plug), IsNil)
	c.Assert(repo.AddSlot(s.slot), IsNil)

	// Snaps should get static security now
	spec, err := repo.SnapSpecification(testSecurity, s.plug.Snap.Name())
	c.Assert(err, IsNil)
	c.Check(spec.(*ifacetest.Specification).Snippets, DeepEquals, []string{"static plug snippet"})

	spec, err = repo.SnapSpecification(testSecurity, s.slot.Snap.Name())
	c.Assert(err, IsNil)
	c.Check(spec.(*ifacetest.Specification).Snippets, DeepEquals, []string{"static slot snippet"})

	// Establish connection between plug and slot
	connRef := ConnRef{PlugRef: s.plug.Ref(), SlotRef: s.slot.Ref()}
	err = repo.Connect(connRef)
	c.Assert(err, IsNil)

	// Snaps should get static and connection-specific security now
	spec, err = repo.SnapSpecification(testSecurity, s.plug.Snap.Name())
	c.Assert(err, IsNil)
	c.Check(spec.(*ifacetest.Specification).Snippets, DeepEquals, []string{
		"static plug snippet",
		"connection-specific plug snippet",
	})

	spec, err = repo.SnapSpecification(testSecurity, s.slot.Snap.Name())
	c.Assert(err, IsNil)
	c.Check(spec.(*ifacetest.Specification).Snippets, DeepEquals, []string{
		"static slot snippet",
		"connection-specific slot snippet",
	})
}

func (s *RepositorySuite) TestSnapSpecificationFailureWithConnectionSnippets(c *C) {
	var testSecurity SecuritySystem = "security"
	backend := &ifacetest.TestSecurityBackend{BackendName: testSecurity}
	iface := &ifacetest.TestInterface{
		InterfaceName: "interface",
		TestConnectedSlotCallback: func(spec *ifacetest.Specification, plug *Plug, plugAttrs map[string]interface{}, slot *Slot, slotAttrs map[string]interface{}) error {
			return fmt.Errorf("cannot compute snippet for provider")
		},
		TestConnectedPlugCallback: func(spec *ifacetest.Specification, plug *Plug, plugAttrs map[string]interface{}, slot *Slot, slotAttrs map[string]interface{}) error {
			return fmt.Errorf("cannot compute snippet for consumer")
		},
	}
	repo := s.emptyRepo

	c.Assert(repo.AddBackend(backend), IsNil)
	c.Assert(repo.AddInterface(iface), IsNil)
	c.Assert(repo.AddPlug(s.plug), IsNil)
	c.Assert(repo.AddSlot(s.slot), IsNil)
	connRef := ConnRef{PlugRef: s.plug.Ref(), SlotRef: s.slot.Ref()}
	c.Assert(repo.Connect(connRef), IsNil)

	spec, err := repo.SnapSpecification(testSecurity, s.plug.Snap.Name())
	c.Assert(err, ErrorMatches, "cannot compute snippet for consumer")
	c.Assert(spec, IsNil)

	spec, err = repo.SnapSpecification(testSecurity, s.slot.Snap.Name())
	c.Assert(err, ErrorMatches, "cannot compute snippet for provider")
	c.Assert(spec, IsNil)
}

func (s *RepositorySuite) TestSnapSpecificationFailureWithPermanentSnippets(c *C) {
	var testSecurity SecuritySystem = "security"
	iface := &ifacetest.TestInterface{
		InterfaceName: "interface",
		TestPermanentSlotCallback: func(spec *ifacetest.Specification, slot *Slot) error {
			return fmt.Errorf("cannot compute snippet for provider")
		},
		TestPermanentPlugCallback: func(spec *ifacetest.Specification, plug *Plug) error {
			return fmt.Errorf("cannot compute snippet for consumer")
		},
	}
	backend := &ifacetest.TestSecurityBackend{BackendName: testSecurity}
	repo := s.emptyRepo
	c.Assert(repo.AddBackend(backend), IsNil)
	c.Assert(repo.AddInterface(iface), IsNil)
	c.Assert(repo.AddPlug(s.plug), IsNil)
	c.Assert(repo.AddSlot(s.slot), IsNil)
	connRef := ConnRef{PlugRef: s.plug.Ref(), SlotRef: s.slot.Ref()}
	c.Assert(repo.Connect(connRef), IsNil)

	spec, err := repo.SnapSpecification(testSecurity, s.plug.Snap.Name())
	c.Assert(err, ErrorMatches, "cannot compute snippet for consumer")
	c.Assert(spec, IsNil)

	spec, err = repo.SnapSpecification(testSecurity, s.slot.Snap.Name())
	c.Assert(err, ErrorMatches, "cannot compute snippet for provider")
	c.Assert(spec, IsNil)
}

func (s *RepositorySuite) TestAutoConnectCandidatePlugsAndSlots(c *C) {
	// Add two interfaces, one with automatic connections, one with manual
	repo := s.emptyRepo
	err := repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "auto"})
	c.Assert(err, IsNil)
	err = repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "manual"})
	c.Assert(err, IsNil)

	policyCheck := func(plug *Plug, slot *Slot) bool {
		return slot.Interface == "auto"
	}

	// Add a pair of snaps with plugs/slots using those two interfaces
	consumer := snaptest.MockInfo(c, `
name: consumer
plugs:
    auto:
    manual:
`, nil)
	producer := snaptest.MockInfo(c, `
name: producer
type: os
slots:
    auto:
    manual:
`, nil)
	err = repo.AddSnap(producer)
	c.Assert(err, IsNil)
	err = repo.AddSnap(consumer)
	c.Assert(err, IsNil)

	candidateSlots := repo.AutoConnectCandidateSlots("consumer", "auto", policyCheck)
	c.Assert(candidateSlots, HasLen, 1)
	c.Check(candidateSlots[0].Snap.Name(), Equals, "producer")
	c.Check(candidateSlots[0].Interface, Equals, "auto")
	c.Check(candidateSlots[0].Name, Equals, "auto")

	candidatePlugs := repo.AutoConnectCandidatePlugs("producer", "auto", policyCheck)
	c.Assert(candidatePlugs, HasLen, 1)
	c.Check(candidatePlugs[0].Snap.Name(), Equals, "consumer")
	c.Check(candidatePlugs[0].Interface, Equals, "auto")
	c.Check(candidatePlugs[0].Name, Equals, "auto")
}

// Tests for AddSnap and RemoveSnap

type AddRemoveSuite struct {
	repo *Repository
}

var _ = Suite(&AddRemoveSuite{})

func (s *AddRemoveSuite) SetUpTest(c *C) {
	s.repo = NewRepository()
	err := s.repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "iface"})
	c.Assert(err, IsNil)
	err = s.repo.AddInterface(&ifacetest.TestInterface{
		InterfaceName:        "invalid",
		SanitizePlugCallback: func(plug *Plug) error { return fmt.Errorf("plug is invalid") },
		SanitizeSlotCallback: func(slot *Slot) error { return fmt.Errorf("slot is invalid") },
	})
	c.Assert(err, IsNil)
}

func (s *AddRemoveSuite) TestAddSnapComplexErrorHandling(c *C) {
	err := s.repo.AddInterface(&ifacetest.TestInterface{
		InterfaceName:        "invalid-plug-iface",
		SanitizePlugCallback: func(plug *Plug) error { return fmt.Errorf("plug is invalid") },
		SanitizeSlotCallback: func(slot *Slot) error { return fmt.Errorf("slot is invalid") },
	})
	c.Assert(err, IsNil)
	err = s.repo.AddInterface(&ifacetest.TestInterface{
		InterfaceName:        "invalid-slot-iface",
		SanitizePlugCallback: func(plug *Plug) error { return fmt.Errorf("plug is invalid") },
		SanitizeSlotCallback: func(slot *Slot) error { return fmt.Errorf("slot is invalid") },
	})
	c.Assert(err, IsNil)
	snapInfo := snaptest.MockInfo(c, `
name: complex
plugs:
    invalid-plug-iface:
    unknown-plug-iface:
slots:
    invalid-slot-iface:
    unknown-slot-iface:
`, nil)
	err = s.repo.AddSnap(snapInfo)
	c.Check(err, ErrorMatches,
		`snap "complex" has bad plugs or slots: invalid-plug-iface \(plug is invalid\); invalid-slot-iface \(slot is invalid\); unknown-plug-iface, unknown-slot-iface \(unknown interface\)`)
	// Nothing was added
	c.Check(s.repo.Plug("complex", "invalid-plug-iface"), IsNil)
	c.Check(s.repo.Plug("complex", "unknown-plug-iface"), IsNil)
	c.Check(s.repo.Slot("complex", "invalid-slot-iface"), IsNil)
	c.Check(s.repo.Slot("complex", "unknown-slot-iface"), IsNil)
}

const testConsumerYaml = `
name: consumer
apps:
    app:
        plugs: [iface]
`
const testProducerYaml = `
name: producer
apps:
    app:
        slots: [iface]
`

func (s *AddRemoveSuite) addSnap(c *C, yaml string) (*snap.Info, error) {
	snapInfo := snaptest.MockInfo(c, yaml, nil)
	return snapInfo, s.repo.AddSnap(snapInfo)
}

func (s *AddRemoveSuite) TestAddSnapAddsPlugs(c *C) {
	_, err := s.addSnap(c, testConsumerYaml)
	c.Assert(err, IsNil)
	// The plug was added
	c.Assert(s.repo.Plug("consumer", "iface"), Not(IsNil))
}

func (s *AddRemoveSuite) TestAddSnapErrorsOnExistingSnapPlugs(c *C) {
	_, err := s.addSnap(c, testConsumerYaml)
	c.Assert(err, IsNil)
	_, err = s.addSnap(c, testConsumerYaml)
	c.Assert(err, ErrorMatches, `cannot register interfaces for snap "consumer" more than once`)
}

func (s *AddRemoveSuite) TestAddSnapAddsSlots(c *C) {
	_, err := s.addSnap(c, testProducerYaml)
	c.Assert(err, IsNil)
	// The slot was added
	c.Assert(s.repo.Slot("producer", "iface"), Not(IsNil))
}

func (s *AddRemoveSuite) TestAddSnapErrorsOnExistingSnapSlots(c *C) {
	_, err := s.addSnap(c, testProducerYaml)
	c.Assert(err, IsNil)
	_, err = s.addSnap(c, testProducerYaml)
	c.Assert(err, ErrorMatches, `cannot register interfaces for snap "producer" more than once`)
}

func (s AddRemoveSuite) TestRemoveRemovesPlugs(c *C) {
	_, err := s.addSnap(c, testConsumerYaml)
	c.Assert(err, IsNil)
	s.repo.RemoveSnap("consumer")
	c.Assert(s.repo.Plug("consumer", "iface"), IsNil)
}

func (s AddRemoveSuite) TestRemoveRemovesSlots(c *C) {
	_, err := s.addSnap(c, testProducerYaml)
	c.Assert(err, IsNil)
	s.repo.RemoveSnap("producer")
	c.Assert(s.repo.Plug("producer", "iface"), IsNil)
}

func (s *AddRemoveSuite) TestRemoveSnapErrorsOnStillConnectedPlug(c *C) {
	_, err := s.addSnap(c, testConsumerYaml)
	c.Assert(err, IsNil)
	_, err = s.addSnap(c, testProducerYaml)
	c.Assert(err, IsNil)
	connRef := ConnRef{PlugRef: PlugRef{Snap: "consumer", Name: "iface"}, SlotRef: SlotRef{Snap: "producer", Name: "iface"}}
	err = s.repo.Connect(connRef)
	c.Assert(err, IsNil)
	err = s.repo.RemoveSnap("consumer")
	c.Assert(err, ErrorMatches, "cannot remove connected plug consumer.iface")
}

func (s *AddRemoveSuite) TestRemoveSnapErrorsOnStillConnectedSlot(c *C) {
	_, err := s.addSnap(c, testConsumerYaml)
	c.Assert(err, IsNil)
	_, err = s.addSnap(c, testProducerYaml)
	c.Assert(err, IsNil)
	connRef := ConnRef{PlugRef: PlugRef{Snap: "consumer", Name: "iface"}, SlotRef: SlotRef{Snap: "producer", Name: "iface"}}
	err = s.repo.Connect(connRef)
	c.Assert(err, IsNil)
	err = s.repo.RemoveSnap("producer")
	c.Assert(err, ErrorMatches, "cannot remove connected slot producer.iface")
}

type DisconnectSnapSuite struct {
	repo   *Repository
	s1, s2 *snap.Info
}

var _ = Suite(&DisconnectSnapSuite{})

func (s *DisconnectSnapSuite) SetUpTest(c *C) {
	s.repo = NewRepository()

	err := s.repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "iface-a"})
	c.Assert(err, IsNil)
	err = s.repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "iface-b"})
	c.Assert(err, IsNil)

	s.s1 = snaptest.MockInfo(c, `
name: s1
plugs:
    iface-a:
slots:
    iface-b:
`, nil)
	err = s.repo.AddSnap(s.s1)
	c.Assert(err, IsNil)

	s.s2 = snaptest.MockInfo(c, `
name: s2
plugs:
    iface-b:
slots:
    iface-a:
`, nil)
	c.Assert(err, IsNil)
	err = s.repo.AddSnap(s.s2)
	c.Assert(err, IsNil)
}

func (s *DisconnectSnapSuite) TestNotConnected(c *C) {
	affected, err := s.repo.DisconnectSnap("s1")
	c.Assert(err, IsNil)
	c.Check(affected, HasLen, 0)
}

func (s *DisconnectSnapSuite) TestOutgoingConnection(c *C) {
	connRef := ConnRef{PlugRef: PlugRef{Snap: "s1", Name: "iface-a"}, SlotRef: SlotRef{Snap: "s2", Name: "iface-a"}}
	err := s.repo.Connect(connRef)
	c.Assert(err, IsNil)
	// Disconnect s1 with which has an outgoing connection to s2
	affected, err := s.repo.DisconnectSnap("s1")
	c.Assert(err, IsNil)
	c.Check(affected, testutil.Contains, "s1")
	c.Check(affected, testutil.Contains, "s2")
}

func (s *DisconnectSnapSuite) TestIncomingConnection(c *C) {
	connRef := ConnRef{PlugRef: PlugRef{Snap: "s2", Name: "iface-b"}, SlotRef: SlotRef{Snap: "s1", Name: "iface-b"}}
	err := s.repo.Connect(connRef)
	c.Assert(err, IsNil)
	// Disconnect s1 with which has an incoming connection from s2
	affected, err := s.repo.DisconnectSnap("s1")
	c.Assert(err, IsNil)
	c.Check(affected, testutil.Contains, "s1")
	c.Check(affected, testutil.Contains, "s2")
}

func (s *DisconnectSnapSuite) TestCrossConnection(c *C) {
	// This test is symmetric wrt s1 <-> s2 connections
	for _, snapName := range []string{"s1", "s2"} {
		connRef1 := ConnRef{PlugRef: PlugRef{Snap: "s1", Name: "iface-a"}, SlotRef: SlotRef{Snap: "s2", Name: "iface-a"}}
		err := s.repo.Connect(connRef1)
		c.Assert(err, IsNil)
		connRef2 := ConnRef{PlugRef: PlugRef{Snap: "s2", Name: "iface-b"}, SlotRef: SlotRef{Snap: "s1", Name: "iface-b"}}
		err = s.repo.Connect(connRef2)
		c.Assert(err, IsNil)
		affected, err := s.repo.DisconnectSnap(snapName)
		c.Assert(err, IsNil)
		c.Check(affected, testutil.Contains, "s1")
		c.Check(affected, testutil.Contains, "s2")
	}
}

func contentPolicyCheck(plug *Plug, slot *Slot) bool {
	return plug.Snap.PublisherID == slot.Snap.PublisherID
}

func contentAutoConnect(plug *Plug, slot *Slot) bool {
	return plug.Attrs["content"] == slot.Attrs["content"]
}

// internal helper that creates a new repository with two snaps, one
// is a content plug and one a content slot
func makeContentConnectionTestSnaps(c *C, plugContentToken, slotContentToken string) (*Repository, *snap.Info, *snap.Info) {
	repo := NewRepository()
	err := repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "content", AutoConnectCallback: contentAutoConnect})
	c.Assert(err, IsNil)

	plugSnap := snaptest.MockInfo(c, fmt.Sprintf(`
name: content-plug-snap
plugs:
  imported-content:
    interface: content
    content: %s
`, plugContentToken), nil)
	slotSnap := snaptest.MockInfo(c, fmt.Sprintf(`
name: content-slot-snap
slots:
  exported-content:
    interface: content
    content: %s
`, slotContentToken), nil)

	err = repo.AddSnap(plugSnap)
	c.Assert(err, IsNil)
	err = repo.AddSnap(slotSnap)
	c.Assert(err, IsNil)

	return repo, plugSnap, slotSnap
}

func (s *RepositorySuite) TestAutoConnectContentInterfaceSimple(c *C) {
	repo, _, _ := makeContentConnectionTestSnaps(c, "mylib", "mylib")
	candidateSlots := repo.AutoConnectCandidateSlots("content-plug-snap", "imported-content", contentPolicyCheck)
	c.Assert(candidateSlots, HasLen, 1)
	c.Check(candidateSlots[0].Name, Equals, "exported-content")
	candidatePlugs := repo.AutoConnectCandidatePlugs("content-slot-snap", "exported-content", contentPolicyCheck)
	c.Assert(candidatePlugs, HasLen, 1)
	c.Check(candidatePlugs[0].Name, Equals, "imported-content")
}

func (s *RepositorySuite) TestAutoConnectContentInterfaceOSWorksCorrectly(c *C) {
	repo, _, slotSnap := makeContentConnectionTestSnaps(c, "mylib", "otherlib")
	slotSnap.Type = snap.TypeOS

	candidateSlots := repo.AutoConnectCandidateSlots("content-plug-snap", "imported-content", contentPolicyCheck)
	c.Check(candidateSlots, HasLen, 0)
	candidatePlugs := repo.AutoConnectCandidatePlugs("content-slot-snap", "exported-content", contentPolicyCheck)
	c.Assert(candidatePlugs, HasLen, 0)
}

func (s *RepositorySuite) TestAutoConnectContentInterfaceNoMatchingContent(c *C) {
	repo, _, _ := makeContentConnectionTestSnaps(c, "mylib", "otherlib")
	candidateSlots := repo.AutoConnectCandidateSlots("content-plug-snap", "imported-content", contentPolicyCheck)
	c.Check(candidateSlots, HasLen, 0)
	candidatePlugs := repo.AutoConnectCandidatePlugs("content-slot-snap", "exported-content", contentPolicyCheck)
	c.Assert(candidatePlugs, HasLen, 0)
}

func (s *RepositorySuite) TestAutoConnectContentInterfaceNoMatchingDeveloper(c *C) {
	repo, plugSnap, slotSnap := makeContentConnectionTestSnaps(c, "mylib", "mylib")
	// real code will use the assertions, this is just for emulation
	plugSnap.PublisherID = "fooid"
	slotSnap.PublisherID = "barid"

	candidateSlots := repo.AutoConnectCandidateSlots("content-plug-snap", "imported-content", contentPolicyCheck)
	c.Check(candidateSlots, HasLen, 0)
	candidatePlugs := repo.AutoConnectCandidatePlugs("content-slot-snap", "exported-content", contentPolicyCheck)
	c.Assert(candidatePlugs, HasLen, 0)
}
