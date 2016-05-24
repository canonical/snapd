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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type RepositorySuite struct {
	iface     Interface
	plug      *Plug
	slot      *Slot
	emptyRepo *Repository
	// Repository pre-populated with s.iface
	testRepo *Repository
}

var _ = Suite(&RepositorySuite{
	iface: &TestInterface{
		InterfaceName: "interface",
	},
})

func (s *RepositorySuite) SetUpTest(c *C) {
	consumer, err := snap.InfoFromSnapYaml([]byte(`
name: consumer
apps:
    app:
plugs:
    plug:
        interface: interface
        label: label
        attr: value
`))
	c.Assert(err, IsNil)
	s.plug = &Plug{PlugInfo: consumer.Plugs["plug"]}
	producer, err := snap.InfoFromSnapYaml([]byte(`
name: producer
apps:
    app:
slots:
    slot:
        interface: interface
        label: label
        attr: value
`))
	c.Assert(err, IsNil)
	s.slot = &Slot{SlotInfo: producer.Slots["slot"]}
	s.emptyRepo = NewRepository()
	s.testRepo = NewRepository()
	err = s.testRepo.AddInterface(s.iface)
	c.Assert(err, IsNil)
}

func addPlugsSlots(c *C, repo *Repository, yamls ...string) []*snap.Info {
	result := make([]*snap.Info, len(yamls))
	for i, yaml := range yamls {
		info, err := snap.InfoFromSnapYaml([]byte(yaml))
		c.Assert(err, IsNil)
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
	iface1 := &TestInterface{InterfaceName: "iface"}
	iface2 := &TestInterface{InterfaceName: "iface"}
	err := s.emptyRepo.AddInterface(iface1)
	c.Assert(err, IsNil)
	// Adding an interface with the same name as another interface is not allowed
	err = s.emptyRepo.AddInterface(iface2)
	c.Assert(err, ErrorMatches, `cannot add interface: "iface", interface name is in use`)
	c.Assert(s.emptyRepo.Interface(iface1.Name()), Equals, iface1)
}

func (s *RepositorySuite) TestAddInterfaceInvalidName(c *C) {
	iface := &TestInterface{InterfaceName: "bad-name-"}
	// Adding an interface with invalid name is not allowed
	err := s.emptyRepo.AddInterface(iface)
	c.Assert(err, ErrorMatches, `invalid interface name: "bad-name-"`)
	c.Assert(s.emptyRepo.Interface(iface.Name()), IsNil)
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
	ifaceA := &TestInterface{InterfaceName: "a"}
	ifaceB := &TestInterface{InterfaceName: "b"}
	ifaceC := &TestInterface{InterfaceName: "c"}
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
	iface := &TestInterface{
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
	err = s.testRepo.Connect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
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
	err := s.testRepo.AddInterface(&TestInterface{InterfaceName: "other-interface"})
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
	err := s.testRepo.AddInterface(&TestInterface{InterfaceName: "other-interface"})
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
	iface := &TestInterface{
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
	err = s.testRepo.Connect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, IsNil)
	// Removing a slot occupied by a plug returns an appropriate error
	err = s.testRepo.RemoveSlot(s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot remove slot "slot" from snap "producer", it is still connected`)
	// The slot is still there
	slot := s.testRepo.Slot(s.slot.Snap.Name(), s.slot.Name)
	c.Assert(slot, Not(IsNil))
}

// Tests for Repository.Connect()

func (s *RepositorySuite) TestConnectFailsWhenPlugDoesNotExist(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Connecting an unknown plug returns an appropriate error
	err = s.testRepo.Connect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot connect plug "plug" from snap "consumer", no such plug`)
}

func (s *RepositorySuite) TestConnectFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Connecting to an unknown slot returns an error
	err = s.testRepo.Connect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot connect plug to slot "slot" from snap "producer", no such slot`)
}

func (s *RepositorySuite) TestConnectSucceedsWhenIdenticalConnectExists(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, IsNil)
	// Connecting exactly the same thing twice succeeds without an error but does nothing.
	err = s.testRepo.Connect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, IsNil)
	// Only one connection is actually present.
	c.Assert(s.testRepo.Interfaces(), DeepEquals, &Interfaces{
		Plugs: []*Plug{{
			PlugInfo:    s.plug.PlugInfo,
			Connections: []SlotRef{{s.slot.Snap.Name(), s.slot.Name}},
		}},
		Slots: []*Slot{{
			SlotInfo:    s.slot.SlotInfo,
			Connections: []PlugRef{{s.plug.Snap.Name(), s.plug.Name}},
		}},
	})
}

func (s *RepositorySuite) TestConnectFailsWhenSlotAndPlugAreIncompatible(c *C) {
	otherInterface := &TestInterface{InterfaceName: "other-interface"}
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
	err = s.testRepo.Connect(plug.Snap.Name(), plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot connect plug "consumer:plug" \(interface "other-interface"\) to "producer:slot" \(interface "interface"\)`)
}

func (s *RepositorySuite) TestConnectSucceeds(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Connecting a plug works okay
	err = s.testRepo.Connect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, IsNil)
}

// Tests for Repository.Disconnect()

func (s *RepositorySuite) TestDisconnectFailsWhenPlugDoesNotExist(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Disconnecting an unknown plug returns and appropriate error
	err = s.testRepo.Disconnect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect plug "plug" from snap "consumer", no such plug`)
}

func (s *RepositorySuite) TestDisconnectFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Disconnecting from an unknown slot returns an appropriate error
	err = s.testRepo.Disconnect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect plug from slot "slot" from snap "producer", no such slot`)
}

func (s *RepositorySuite) TestDisconnectFromSlotFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Disconnecting everything form an unknown slot returns an appropriate error
	err = s.testRepo.Disconnect("", "", s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect plug from slot "slot" from snap "producer", no such slot`)
}

func (s *RepositorySuite) TestDisconnectFromSnapFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Disconnecting all plugs from a snap that is not known returns an appropriate error
	err = s.testRepo.Disconnect("", "", s.slot.Snap.Name(), "")
	c.Assert(err, ErrorMatches, `cannot disconnect plug from snap "producer", no such snap`)
}

func (s *RepositorySuite) TestDisconnectFailsWhenNotConnected(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Disconnecting a plug that is not connected returns an appropriate error
	err = s.testRepo.Disconnect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect plug "plug" from snap "consumer" from slot "slot" from snap "producer", it is not connected`)
}

func (s *RepositorySuite) TestDisconnectFromSnapDoesNothingWhenNotConnected(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Disconnecting a all plugs from a snap that uses nothing is not an error.
	err = s.testRepo.Disconnect("", "", s.slot.Snap.Name(), "")
	c.Assert(err, IsNil)
}

func (s *RepositorySuite) TestDisconnectFromSlotDoesNothingWhenNotConnected(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Disconnecting a all plugs from a slot that uses nothing is not an error.
	err = s.testRepo.Disconnect("", "", s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, IsNil)
}

func (s *RepositorySuite) TestDisconnectSucceeds(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, IsNil)
	// Disconnecting a connected plug works okay
	err = s.testRepo.Disconnect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.Interfaces(), DeepEquals, &Interfaces{
		Plugs: []*Plug{{PlugInfo: s.plug.PlugInfo}},
		Slots: []*Slot{{SlotInfo: s.slot.SlotInfo}},
	})
}

func (s *RepositorySuite) TestDisconnectFromSnap(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, IsNil)
	// Disconnecting everything from a snap works OK
	err = s.testRepo.Disconnect("", "", s.slot.Snap.Name(), "")
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.Interfaces(), DeepEquals, &Interfaces{
		Plugs: []*Plug{{PlugInfo: s.plug.PlugInfo}},
		Slots: []*Slot{{SlotInfo: s.slot.SlotInfo}},
	})
}

func (s *RepositorySuite) TestDisconnectFromSlot(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, IsNil)
	// Disconnecting everything from a slot works OK
	err = s.testRepo.Disconnect("", "", s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, IsNil)
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
	err = s.testRepo.Connect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name)
	c.Assert(err, IsNil)
	ifaces := s.testRepo.Interfaces()
	c.Assert(ifaces, DeepEquals, &Interfaces{
		Plugs: []*Plug{{
			PlugInfo:    s.plug.PlugInfo,
			Connections: []SlotRef{{s.slot.Snap.Name(), s.slot.Name}},
		}},
		Slots: []*Slot{{
			SlotInfo:    s.slot.SlotInfo,
			Connections: []PlugRef{{s.plug.Snap.Name(), s.plug.Name}},
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

// Tests for Repository.SecuritySnippetsForSnap()

func (s *RepositorySuite) TestSlotSnippetsForSnapSuccess(c *C) {
	const testSecurity SecuritySystem = "security"
	iface := &TestInterface{
		InterfaceName: "interface",
		PermanentPlugSnippetCallback: func(plug *Plug, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == testSecurity {
				return []byte(`static plug snippet`), nil
			}
			return nil, ErrUnknownSecurity
		},
		PlugSnippetCallback: func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == testSecurity {
				return []byte(`connection-specific plug snippet`), nil
			}
			return nil, ErrUnknownSecurity
		},
		PermanentSlotSnippetCallback: func(slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == testSecurity {
				return []byte(`static slot snippet`), nil
			}
			return nil, ErrUnknownSecurity
		},
		SlotSnippetCallback: func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == testSecurity {
				return []byte(`connection-specific slot snippet`), nil
			}
			return nil, ErrUnknownSecurity
		},
	}
	repo := s.emptyRepo
	c.Assert(repo.AddInterface(iface), IsNil)
	c.Assert(repo.AddPlug(s.plug), IsNil)
	c.Assert(repo.AddSlot(s.slot), IsNil)
	// Snaps should get static security now
	var snippets map[string][][]byte
	snippets, err := repo.SecuritySnippetsForSnap(s.plug.Snap.Name(), testSecurity)
	c.Assert(err, IsNil)
	c.Check(snippets, DeepEquals, map[string][][]byte{
		"app": [][]byte{
			[]byte(`static plug snippet`),
		},
	})
	snippets, err = repo.SecuritySnippetsForSnap(s.slot.Snap.Name(), testSecurity)
	c.Assert(err, IsNil)
	c.Check(snippets, DeepEquals, map[string][][]byte{
		"app": [][]byte{
			[]byte(`static slot snippet`),
		},
	})
	// Establish connection between plug and slot
	c.Assert(repo.Connect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name), IsNil)
	// Snaps should get static and connection-specific security now
	snippets, err = repo.SecuritySnippetsForSnap(s.plug.Snap.Name(), testSecurity)
	c.Assert(err, IsNil)
	c.Check(snippets, DeepEquals, map[string][][]byte{
		"app": [][]byte{
			[]byte(`static plug snippet`),
			[]byte(`connection-specific plug snippet`),
		},
	})
	snippets, err = repo.SecuritySnippetsForSnap(s.slot.Snap.Name(), testSecurity)
	c.Assert(err, IsNil)
	c.Check(snippets, DeepEquals, map[string][][]byte{
		"app": [][]byte{
			[]byte(`static slot snippet`),
			[]byte(`connection-specific slot snippet`),
		},
	})
}

func (s *RepositorySuite) TestSecuritySnippetsForSnapFailureWithConnectionSnippets(c *C) {
	var testSecurity SecuritySystem = "security"
	iface := &TestInterface{
		InterfaceName: "interface",
		SlotSnippetCallback: func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			return nil, fmt.Errorf("cannot compute snippet for consumer")
		},
		PlugSnippetCallback: func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			return nil, fmt.Errorf("cannot compute snippet for provider")
		},
	}
	repo := s.emptyRepo
	c.Assert(repo.AddInterface(iface), IsNil)
	c.Assert(repo.AddPlug(s.plug), IsNil)
	c.Assert(repo.AddSlot(s.slot), IsNil)
	c.Assert(repo.Connect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name), IsNil)
	var snippets map[string][][]byte
	snippets, err := repo.SecuritySnippetsForSnap(s.plug.Snap.Name(), testSecurity)
	c.Assert(err, ErrorMatches, "cannot compute snippet for provider")
	c.Check(snippets, IsNil)
	snippets, err = repo.SecuritySnippetsForSnap(s.slot.Snap.Name(), testSecurity)
	c.Assert(err, ErrorMatches, "cannot compute snippet for consumer")
	c.Check(snippets, IsNil)
}

func (s *RepositorySuite) TestSecuritySnippetsForSnapFailureWithPermanentSnippets(c *C) {
	var testSecurity SecuritySystem = "security"
	iface := &TestInterface{
		InterfaceName: "interface",
		PermanentSlotSnippetCallback: func(slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			return nil, fmt.Errorf("cannot compute static snippet for consumer")
		},
		PermanentPlugSnippetCallback: func(plug *Plug, securitySystem SecuritySystem) ([]byte, error) {
			return nil, fmt.Errorf("cannot compute static snippet for provider")
		},
	}
	repo := s.emptyRepo
	c.Assert(repo.AddInterface(iface), IsNil)
	c.Assert(repo.AddPlug(s.plug), IsNil)
	c.Assert(repo.AddSlot(s.slot), IsNil)
	c.Assert(repo.Connect(s.plug.Snap.Name(), s.plug.Name, s.slot.Snap.Name(), s.slot.Name), IsNil)
	var snippets map[string][][]byte
	snippets, err := repo.SecuritySnippetsForSnap(s.plug.Snap.Name(), testSecurity)
	c.Assert(err, ErrorMatches, "cannot compute static snippet for provider")
	c.Check(snippets, IsNil)
	snippets, err = repo.SecuritySnippetsForSnap(s.slot.Snap.Name(), testSecurity)
	c.Assert(err, ErrorMatches, "cannot compute static snippet for consumer")
	c.Check(snippets, IsNil)
}

func (s *RepositorySuite) TestAutoConnectBlacklist(c *C) {
	// Add two interfaces, one with automatic connections, one with manual
	repo := s.emptyRepo
	err := repo.AddInterface(&TestInterface{InterfaceName: "auto", AutoConnectFlag: true})
	c.Assert(err, IsNil)
	err = repo.AddInterface(&TestInterface{InterfaceName: "manual"})
	c.Assert(err, IsNil)

	// Add a pair of snaps with plugs/slots using those two interfaces
	consumer, err := snap.InfoFromSnapYaml([]byte(`
name: consumer
plugs:
    auto:
    manual:
`))
	c.Assert(err, IsNil)
	producer, err := snap.InfoFromSnapYaml([]byte(`
name: producer
type: os
slots:
    auto:
    manual:
`))
	c.Assert(err, IsNil)
	err = repo.AddSnap(producer)
	c.Assert(err, IsNil)
	err = repo.AddSnap(consumer)
	c.Assert(err, IsNil)

	// Sanity check, our test is valid because plug "auto" is a candidate
	// for auto-connection
	c.Assert(repo.AutoConnectCandidates("consumer", "auto"), HasLen, 1)

	// Without any connections in place, the plug "auto" is blacklisted
	// because in normal circumstances it would be auto-connected.
	blacklist := repo.AutoConnectBlacklist("consumer")
	c.Check(blacklist, DeepEquals, map[string]bool{"auto": true})

	// Connect the "auto" plug and slots together
	err = repo.Connect("consumer", "auto", "producer", "auto")
	c.Assert(err, IsNil)

	// With the connection in place the "auto" plug is not blacklisted.
	blacklist = repo.AutoConnectBlacklist("consumer")
	c.Check(blacklist, IsNil)
}

// Tests for AddSnap and RemoveSnap

type AddRemoveSuite struct {
	repo *Repository
}

var _ = Suite(&AddRemoveSuite{})

func (s *AddRemoveSuite) SetUpTest(c *C) {
	s.repo = NewRepository()
	err := s.repo.AddInterface(&TestInterface{InterfaceName: "iface"})
	c.Assert(err, IsNil)
	err = s.repo.AddInterface(&TestInterface{
		InterfaceName:        "invalid",
		SanitizePlugCallback: func(plug *Plug) error { return fmt.Errorf("plug is invalid") },
		SanitizeSlotCallback: func(slot *Slot) error { return fmt.Errorf("slot is invalid") },
	})
	c.Assert(err, IsNil)
}

func (s *AddRemoveSuite) TestAddSnapComplexErrorHandling(c *C) {
	err := s.repo.AddInterface(&TestInterface{
		InterfaceName:        "invalid-plug-iface",
		SanitizePlugCallback: func(plug *Plug) error { return fmt.Errorf("plug is invalid") },
		SanitizeSlotCallback: func(slot *Slot) error { return fmt.Errorf("slot is invalid") },
	})
	err = s.repo.AddInterface(&TestInterface{
		InterfaceName:        "invalid-slot-iface",
		SanitizePlugCallback: func(plug *Plug) error { return fmt.Errorf("plug is invalid") },
		SanitizeSlotCallback: func(slot *Slot) error { return fmt.Errorf("slot is invalid") },
	})
	snapInfo, err := snap.InfoFromSnapYaml([]byte(`
name: complex
plugs:
    invalid-plug-iface:
    unknown-plug-iface:
slots:
    invalid-slot-iface:
    unknown-slot-iface:
`))
	c.Assert(err, IsNil)
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
	snapInfo, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)
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
	err = s.repo.Connect("consumer", "iface", "producer", "iface")
	c.Assert(err, IsNil)
	err = s.repo.RemoveSnap("consumer")
	c.Assert(err, ErrorMatches, "cannot remove connected plug consumer.iface")
}

func (s *AddRemoveSuite) TestRemoveSnapErrorsOnStillConnectedSlot(c *C) {
	_, err := s.addSnap(c, testConsumerYaml)
	c.Assert(err, IsNil)
	_, err = s.addSnap(c, testProducerYaml)
	c.Assert(err, IsNil)
	err = s.repo.Connect("consumer", "iface", "producer", "iface")
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

	err := s.repo.AddInterface(&TestInterface{InterfaceName: "iface-a"})
	c.Assert(err, IsNil)
	err = s.repo.AddInterface(&TestInterface{InterfaceName: "iface-b"})
	c.Assert(err, IsNil)

	s.s1, err = snap.InfoFromSnapYaml([]byte(`
name: s1
plugs:
    iface-a:
slots:
    iface-b:
`))
	c.Assert(err, IsNil)
	err = s.repo.AddSnap(s.s1)
	c.Assert(err, IsNil)

	s.s2, err = snap.InfoFromSnapYaml([]byte(`
name: s2
plugs:
    iface-b:
slots:
    iface-a:
`))
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
	err := s.repo.Connect("s1", "iface-a", "s2", "iface-a")
	c.Assert(err, IsNil)
	// Disconnect s1 with which has an outgoing connection to s2
	affected, err := s.repo.DisconnectSnap("s1")
	c.Assert(err, IsNil)
	c.Check(affected, testutil.Contains, "s1")
	c.Check(affected, testutil.Contains, "s2")
}

func (s *DisconnectSnapSuite) TestIncomingConnection(c *C) {
	err := s.repo.Connect("s2", "iface-b", "s1", "iface-b")
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
		err := s.repo.Connect("s1", "iface-a", "s2", "iface-a")
		c.Assert(err, IsNil)
		err = s.repo.Connect("s2", "iface-b", "s1", "iface-b")
		c.Assert(err, IsNil)
		affected, err := s.repo.DisconnectSnap(snapName)
		c.Assert(err, IsNil)
		c.Check(affected, testutil.Contains, "s1")
		c.Check(affected, testutil.Contains, "s2")
	}
}
