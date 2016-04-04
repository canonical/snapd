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

	. "github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/testutil"
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
	// NOTE: the names provider/consumer are confusing. They will be fixed shortly.
	provider, err := snap.InfoFromSnapYaml([]byte(`
name: provider
apps:
    app:
plugs:
    plug:
        interface: interface
        label: label
        attr: value
`))
	c.Assert(err, IsNil)
	s.plug = &Plug{PlugInfo: provider.Plugs["plug"]}
	consumer, err := snap.InfoFromSnapYaml([]byte(`
name: consumer
apps:
    app:
slots:
    slot:
        interface: interface
        label: label
        attr: value
`))
	c.Assert(err, IsNil)
	s.slot = &Slot{SlotInfo: consumer.Slots["slot"]}
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
	c.Assert(s.testRepo.Plug(s.plug.Snap.Name, s.plug.Name), DeepEquals, s.plug)
}

func (s *RepositorySuite) TestAddPlugClash(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, ErrorMatches, `cannot add plug, snap "provider" already has plug "plug"`)
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 1)
	c.Assert(s.testRepo.Plug(s.plug.Snap.Name, s.plug.Name), DeepEquals, s.plug)
}

func (s *RepositorySuite) TestAddPlugFailsWithInvalidSnapName(c *C) {
	plug := &Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{Name: "bad-snap-"},
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
			Snap:      &snap.Info{Name: "snap"},
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
	c.Assert(s.emptyRepo.Plug(s.plug.Snap.Name, s.plug.Name), IsNil)
	c.Assert(s.testRepo.Plug(s.plug.Snap.Name, s.plug.Name), DeepEquals, s.plug)
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
	err = s.testRepo.RemovePlug(s.plug.Snap.Name, s.plug.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 0)
}

func (s *RepositorySuite) TestRemovePlugFailsWhenPlugDoesntExist(c *C) {
	err := s.emptyRepo.RemovePlug(s.plug.Snap.Name, s.plug.Name)
	c.Assert(err, ErrorMatches, `cannot remove plug "plug" from snap "provider", no such plug`)
}

func (s *RepositorySuite) TestRemovePlugFailsWhenPlugIsConnected(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, IsNil)
	// Removing a plug used by a slot returns an appropriate error
	err = s.testRepo.RemovePlug(s.plug.Snap.Name, s.plug.Name)
	c.Assert(err, ErrorMatches, `cannot remove plug "plug" from snap "provider", it is still connected`)
	// The plug is still there
	slot := s.testRepo.Plug(s.plug.Snap.Name, s.plug.Name)
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
	slot := s.testRepo.Slot(s.slot.Snap.Name, s.slot.Name)
	c.Assert(slot, DeepEquals, s.slot)
}

func (s *RepositorySuite) TestSlotFailsWhenSlotDoesntExist(c *C) {
	slot := s.testRepo.Slot(s.slot.Snap.Name, s.slot.Name)
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
			Snap:      &snap.Info{Name: "snap"},
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
			Snap:      &snap.Info{Name: "bad-snap-"},
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
	c.Assert(err, ErrorMatches, `cannot add slot, snap "consumer" already has slot "slot"`)
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
	slot := s.testRepo.Slot(s.slot.Snap.Name, s.slot.Name)
	// The added slot has the same data
	c.Assert(slot, DeepEquals, s.slot)
}

// Tests for Repository.RemoveSlot()

func (s *RepositorySuite) TestRemoveSlotSuccedsWhenSlotExistsAndDisconnected(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Removing a vacant slot simply works
	err = s.testRepo.RemoveSlot(s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, IsNil)
	// The slot is gone now
	slot := s.testRepo.Slot(s.slot.Snap.Name, s.slot.Name)
	c.Assert(slot, IsNil)
}

func (s *RepositorySuite) TestRemoveSlotFailsWhenSlotDoesntExist(c *C) {
	// Removing a slot that doesn't exist returns an appropriate error
	err := s.testRepo.RemoveSlot(s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, `cannot remove plug slot "slot" from snap "consumer", no such slot`)
}

func (s *RepositorySuite) TestRemoveSlotFailsWhenSlotIsConnected(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, IsNil)
	// Removing a slot occupied by a plug returns an appropriate error
	err = s.testRepo.RemoveSlot(s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot remove slot "slot" from snap "consumer", it is still connected`)
	// The slot is still there
	slot := s.testRepo.Slot(s.slot.Snap.Name, s.slot.Name)
	c.Assert(slot, Not(IsNil))
}

// Tests for Repository.Connect()

func (s *RepositorySuite) TestConnectFailsWhenPlugDoesNotExist(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Connecting an unknown plug returns an appropriate error
	err = s.testRepo.Connect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot connect plug "plug" from snap "provider", no such plug`)
}

func (s *RepositorySuite) TestConnectFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Connecting to an unknown slot returns an error
	err = s.testRepo.Connect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot connect plug to slot "slot" from snap "consumer", no such slot`)
}

func (s *RepositorySuite) TestConnectSucceedsWhenIdenticalConnectExists(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, IsNil)
	// Connecting exactly the same thing twice succeeds without an error but does nothing.
	err = s.testRepo.Connect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, IsNil)
	// Only one connection is actually present.
	c.Assert(s.testRepo.Interfaces(), DeepEquals, &Interfaces{
		Plugs: []*Plug{{
			PlugInfo:    s.plug.PlugInfo,
			Connections: []SlotRef{{s.slot.Snap.Name, s.slot.Name}},
		}},
		Slots: []*Slot{{
			SlotInfo:    s.slot.SlotInfo,
			Connections: []PlugRef{{s.plug.Snap.Name, s.plug.Name}},
		}},
	})
}

func (s *RepositorySuite) TestConnectFailsWhenSlotAndPlugAreIncompatible(c *C) {
	otherInterface := &TestInterface{InterfaceName: "other-interface"}
	err := s.testRepo.AddInterface(otherInterface)
	plug := &Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{Name: "provider"},
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
	err = s.testRepo.Connect(plug.Snap.Name, plug.Name, s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot connect plug "provider:plug" \(interface "other-interface"\) to "consumer:slot" \(interface "interface"\)`)
}

func (s *RepositorySuite) TestConnectSucceeds(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Connecting a plug works okay
	err = s.testRepo.Connect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, IsNil)
}

// Tests for Repository.Disconnect()

func (s *RepositorySuite) TestDisconnectFailsWhenPlugDoesNotExist(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Disconnecting an unknown plug returns and appropriate error
	err = s.testRepo.Disconnect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect plug "plug" from snap "provider", no such plug`)
}

func (s *RepositorySuite) TestDisconnectFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Disconnecting from an unknown slot returns an appropriate error
	err = s.testRepo.Disconnect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect plug from slot "slot" from snap "consumer", no such slot`)
}

func (s *RepositorySuite) TestDisconnectFromSlotFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Disconnecting everything form an unknown slot returns an appropriate error
	err = s.testRepo.Disconnect("", "", s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect plug from slot "slot" from snap "consumer", no such slot`)
}

func (s *RepositorySuite) TestDisconnectFromSnapFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Disconnecting all plugs from a snap that is not known returns an appropriate error
	err = s.testRepo.Disconnect("", "", s.slot.Snap.Name, "")
	c.Assert(err, ErrorMatches, `cannot disconnect plug from snap "consumer", no such snap`)
}

func (s *RepositorySuite) TestDisconnectFailsWhenNotConnected(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Disconnecting a plug that is not connected returns an appropriate error
	err = s.testRepo.Disconnect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect plug "plug" from snap "provider" from slot "slot" from snap "consumer", it is not connected`)
}

func (s *RepositorySuite) TestDisconnectFromSnapDoesNothingWhenNotConnected(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Disconnecting a all plugs from a snap that uses nothing is not an error.
	err = s.testRepo.Disconnect("", "", s.slot.Snap.Name, "")
	c.Assert(err, IsNil)
}

func (s *RepositorySuite) TestDisconnectFromSlotDoesNothingWhenNotConnected(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Disconnecting a all plugs from a slot that uses nothing is not an error.
	err = s.testRepo.Disconnect("", "", s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, IsNil)
}

func (s *RepositorySuite) TestDisconnectSucceeds(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, IsNil)
	// Disconnecting a connected plug works okay
	err = s.testRepo.Disconnect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name)
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
	err = s.testRepo.Connect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, IsNil)
	// Disconnecting everything from a snap works OK
	err = s.testRepo.Disconnect("", "", s.slot.Snap.Name, "")
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
	err = s.testRepo.Connect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, IsNil)
	// Disconnecting everything from a plug slot works OK
	err = s.testRepo.Disconnect("", "", s.slot.Snap.Name, s.slot.Name)
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
	err = s.testRepo.Connect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name)
	c.Assert(err, IsNil)
	ifaces := s.testRepo.Interfaces()
	c.Assert(ifaces, DeepEquals, &Interfaces{
		Plugs: []*Plug{{
			PlugInfo:    s.plug.PlugInfo,
			Connections: []SlotRef{{s.slot.Snap.Name, s.slot.Name}},
		}},
		Slots: []*Slot{{
			SlotInfo:    s.slot.SlotInfo,
			Connections: []PlugRef{{s.plug.Snap.Name, s.plug.Name}},
		}},
	})
	// After disconnecting the connections become empty
	err = s.testRepo.Disconnect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name)
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
	snippets, err := repo.SecuritySnippetsForSnap(s.plug.Snap.Name, testSecurity)
	c.Assert(err, IsNil)
	c.Check(snippets, DeepEquals, map[string][][]byte{
		"app": [][]byte{
			[]byte(`static plug snippet`),
		},
	})
	snippets, err = repo.SecuritySnippetsForSnap(s.slot.Snap.Name, testSecurity)
	c.Assert(err, IsNil)
	c.Check(snippets, DeepEquals, map[string][][]byte{
		"app": [][]byte{
			[]byte(`static slot snippet`),
		},
	})
	// Establish connection between plug and slot
	c.Assert(repo.Connect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name), IsNil)
	// Snaps should get static and connection-specific security now
	snippets, err = repo.SecuritySnippetsForSnap(s.plug.Snap.Name, testSecurity)
	c.Assert(err, IsNil)
	c.Check(snippets, DeepEquals, map[string][][]byte{
		"app": [][]byte{
			[]byte(`static plug snippet`),
			[]byte(`connection-specific plug snippet`),
		},
	})
	snippets, err = repo.SecuritySnippetsForSnap(s.slot.Snap.Name, testSecurity)
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
	c.Assert(repo.Connect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name), IsNil)
	var snippets map[string][][]byte
	snippets, err := repo.SecuritySnippetsForSnap(s.plug.Snap.Name, testSecurity)
	c.Assert(err, ErrorMatches, "cannot compute snippet for provider")
	c.Check(snippets, IsNil)
	snippets, err = repo.SecuritySnippetsForSnap(s.slot.Snap.Name, testSecurity)
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
	c.Assert(repo.Connect(s.plug.Snap.Name, s.plug.Name, s.slot.Snap.Name, s.slot.Name), IsNil)
	var snippets map[string][][]byte
	snippets, err := repo.SecuritySnippetsForSnap(s.plug.Snap.Name, testSecurity)
	c.Assert(err, ErrorMatches, "cannot compute static snippet for provider")
	c.Check(snippets, IsNil)
	snippets, err = repo.SecuritySnippetsForSnap(s.slot.Snap.Name, testSecurity)
	c.Assert(err, ErrorMatches, "cannot compute static snippet for consumer")
	c.Check(snippets, IsNil)
}

// Tests for Refresh()

type RefreshSuite struct {
	repo *Repository
}

var _ = Suite(&RefreshSuite{})

func (s *RefreshSuite) SetUpTest(c *C) {
	s.repo = NewRepository()
	err := s.repo.AddInterface(&TestInterface{InterfaceName: "iface"})
	c.Assert(err, IsNil)
	err = s.repo.AddInterface(&TestInterface{InterfaceName: "other-iface"})
	c.Assert(err, IsNil)
	err = s.repo.AddInterface(&TestInterface{
		InterfaceName:        "invalid",
		SanitizePlugCallback: func(plug *Plug) error { return fmt.Errorf("invalid") },
		SanitizeSlotCallback: func(slot *Slot) error { return fmt.Errorf("invalid") },
	})
	c.Assert(err, IsNil)
}

func (s *RefreshSuite) refresh(c *C, yaml string) (*snap.Info, []*snap.Info) {
	snapInfo, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)
	return snapInfo, s.repo.Refresh(snapInfo)
}

const testConsumerYaml = `
name: consumer
apps:
    app:
        plugs: [iface]
`
const testDifferentIfaceConsumerYaml = `
name: consumer
apps:
    app:
        plugs: [iface]
plugs:
    iface:
        interface: other-iface
`
const testDifferentAttrsConsumerYaml = `
name: consumer
apps:
    app:
        plugs: [iface]
plugs:
    iface:
        attr: extra
`
const testUnknownIfaceConsumerYaml = `
name: consumer
apps:
    app:
        plugs: [unknown]
`
const testInvalidIfaceConsumerYaml = `
name: consumer
apps:
    app:
        plugs: [invalid]
`
const testDifferentLabelConsumerYaml = `
name: consumer
apps:
    app:
        plugs: [iface]
plugs:
    iface:
        label: New Label
`
const testBareConsumerYaml = `
name: consumer
apps:
    app:
`

func (s *RefreshSuite) TestRefreshAddsPlugsOnInstall(c *C) {
	// Installing a snap can add a new plug.
	consumer, affected := s.refresh(c, testConsumerYaml)
	// The plug was added
	c.Assert(s.repo.Plug("consumer", "iface"), Not(IsNil))
	// The snap itself was affected
	c.Check(affected, DeepEquals, []*snap.Info{consumer})
}

func (s *RefreshSuite) TestRefreshAddsPlugsOnUpgrade(c *C) {
	// Updating a snap can add a new plug.
	_, _ = s.refresh(c, testBareConsumerYaml)
	consumer, affected := s.refresh(c, testConsumerYaml)
	// The plug was added
	c.Assert(s.repo.Plug("consumer", "iface"), Not(IsNil))
	// The snap itself was affected
	c.Check(affected, DeepEquals, []*snap.Info{consumer})
}

func (s *RefreshSuite) TestRefreshRemovesPlugsOnUpgrade(c *C) {
	// Updating a snap can remove an existing plug.
	_, _ = s.refresh(c, testConsumerYaml)
	consumer, affected := s.refresh(c, testBareConsumerYaml)
	// The plug was removed
	c.Check(s.repo.Plug("consumer", "iface"), IsNil)
	// The snap itself was affected
	c.Check(affected, DeepEquals, []*snap.Info{consumer})
}

func (s *RefreshSuite) TestRefreshRemovesPlugsWithUnknownInterfaceOnUpgrade(c *C) {
	// Updating a snap can change an existing plug to use an unknown interface.
	// This removes the plug.
	_, _ = s.refresh(c, testConsumerYaml)
	consumer, affected := s.refresh(c, testUnknownIfaceConsumerYaml)
	// The plug was removed
	c.Check(s.repo.Plug("consumer", "iface"), IsNil)
	// The snap itself was affected
	c.Check(affected, DeepEquals, []*snap.Info{consumer})
}

func (s *RefreshSuite) TestRefreshRemovesPlugsWithInvalidInterfaceOnUpgrade(c *C) {
	// Updating a snap can change an existing plug in a way which causes it not
	// to validate. This removes the plug.
	_, _ = s.refresh(c, testConsumerYaml)
	consumer, affected := s.refresh(c, testInvalidIfaceConsumerYaml)
	// The plug was removed
	c.Check(s.repo.Plug("consumer", "iface"), IsNil)
	// The snap itself was affected
	c.Check(affected, DeepEquals, []*snap.Info{consumer})
}

func (s *RefreshSuite) TestRefreshRemovesPlugAndSeversConnectionOnUpgrade(c *C) {
	// Updating a snap can remove an existing plug.
	// This severs connections made from that plug.
	_, _ = s.refresh(c, testConsumerYaml)
	producer, _ := s.refresh(c, testProducerYaml)
	err := s.repo.Connect("consumer", "iface", "producer", "iface")
	c.Assert(err, IsNil)
	consumer, affected := s.refresh(c, testBareConsumerYaml)
	// The consumer plug was removed
	c.Check(s.repo.Plug("consumer", "iface"), IsNil)
	// The connection was severed
	slot := s.repo.Slot("producer", "iface")
	c.Check(slot.Connections, HasLen, 0)
	// Both snaps were affected
	c.Check(affected, testutil.Contains, consumer)
	c.Check(affected, testutil.Contains, producer)
}

func (s *RefreshSuite) TestRefreshUpdatesPlugToNewInterfaceAndSeversConnectionOnUpgrade(c *C) {
	// Updating a snap can change a plug interface.
	// This severs connections made from that plug.
	_, _ = s.refresh(c, testConsumerYaml)
	producer, _ := s.refresh(c, testProducerYaml)
	err := s.repo.Connect("consumer", "iface", "producer", "iface")
	c.Assert(err, IsNil)
	consumer, affected := s.refresh(c, testDifferentIfaceConsumerYaml)
	// The consumer plug was updated
	plug := s.repo.Plug("consumer", "iface")
	c.Assert(plug, Not(IsNil))
	c.Check(plug.Interface, Equals, "other-iface")
	// The connection was severed
	slot := s.repo.Slot("producer", "iface")
	c.Check(slot.Connections, HasLen, 0)
	c.Check(plug.Connections, HasLen, 0)
	// Both snaps were affected
	c.Check(affected, testutil.Contains, consumer)
	c.Check(affected, testutil.Contains, producer)
}

func (s *RefreshSuite) TestRefreshUpdatesPlugToNewAttrsAndSeversConnectionOnUpgrade(c *C) {
	// Updating a snap can change a plug attributes.
	// This severs connections made from that plug.
	_, _ = s.refresh(c, testConsumerYaml)
	producer, _ := s.refresh(c, testProducerYaml)
	err := s.repo.Connect("consumer", "iface", "producer", "iface")
	c.Assert(err, IsNil)
	consumer, affected := s.refresh(c, testDifferentAttrsConsumerYaml)
	// The consumer plug was updated
	plug := s.repo.Plug("consumer", "iface")
	c.Assert(plug, Not(IsNil))
	c.Check(plug.Attrs, DeepEquals, map[string]interface{}{"attr": "extra"})
	// The connection was severed
	slot := s.repo.Slot("producer", "iface")
	c.Check(slot.Connections, HasLen, 0)
	c.Check(plug.Connections, HasLen, 0)
	// Both snaps were affected
	c.Check(affected, testutil.Contains, consumer)
	c.Check(affected, testutil.Contains, producer)
}

func (s *RefreshSuite) TestRefreshUpdatesPlugToNewLabelAndKeepsConnectionOnUpgrade(c *C) {
	// Updating a snap can change a plug label.
	// This doesn't sever existing connection
	_, _ = s.refresh(c, testConsumerYaml)
	_, _ = s.refresh(c, testProducerYaml)
	err := s.repo.Connect("consumer", "iface", "producer", "iface")
	c.Assert(err, IsNil)
	_, affected := s.refresh(c, testDifferentLabelConsumerYaml)
	// The consumer plug was updated
	plug := s.repo.Plug("consumer", "iface")
	c.Assert(plug, Not(IsNil))
	c.Check(plug.Label, Equals, "New Label")
	// The connection was not severed
	slot := s.repo.Slot("producer", "iface")
	c.Check(slot.Connections, HasLen, 1)
	c.Check(plug.Connections, HasLen, 1)
	// No snaps are affected by label changes.
	c.Check(affected, HasLen, 0)
}

// NOTE: The following tests are copy-pasted from the tests above
//
// They are fixed using the following vim expression
// :'<,'>s/Consumer/000/g | '<,'>s/Producer/Consumer/g | '<,'>s/000/Producer/g | '<,'>s/consumer/AAA/g | '<,'>s/producer/consumer/g | '<,'>s/AAA/producer/g | '<,'>s/Plug/BBB/g |
//  '<,'>s/Slot/Plug/g | '<,'>s/BBB/Slot/g | '<,'>s/plug/CCC/g | '<,'>s/slot/plug/ | '<,'>s/CCC/slot/g
// The only exception is the .Connect() calls that have fixed order of arguments.
// Those are manually changed to be identical as in the test block above.

const testProducerYaml = `
name: producer
apps:
    app:
        slots: [iface]
`
const testDifferentIfaceProducerYaml = `
name: producer
apps:
    app:
        slots: [iface]
slots:
    iface:
        interface: other-iface
`
const testDifferentAttrsProducerYaml = `
name: producer
apps:
    app:
        slots: [iface]
slots:
    iface:
        attr: extra
`
const testUnknownIfaceProducerYaml = `
name: producer
apps:
    app:
        slots: [unknown]
`
const testInvalidIfaceProducerYaml = `
name: producer
apps:
    app:
        slots: [invalid]
`
const testDifferentLabelProducerYaml = `
name: producer
apps:
    app:
        slots: [iface]
slots:
    iface:
        label: New Label
`
const testBareProducerYaml = `
name: producer
apps:
    app:
`

func (s *RefreshSuite) TestRefreshAddsSlotsOnInstall(c *C) {
	// Installing a snap can add a new slot.
	producer, affected := s.refresh(c, testProducerYaml)
	// The slot was added
	c.Assert(s.repo.Slot("producer", "iface"), Not(IsNil))
	// The snap itself was affected
	c.Check(affected, DeepEquals, []*snap.Info{producer})
}

func (s *RefreshSuite) TestRefreshAddsSlotsOnUpgrade(c *C) {
	// Updating a snap can add a new slot.
	_, _ = s.refresh(c, testBareProducerYaml)
	producer, affected := s.refresh(c, testProducerYaml)
	// The slot was added
	c.Assert(s.repo.Slot("producer", "iface"), Not(IsNil))
	// The snap itself was affected
	c.Check(affected, DeepEquals, []*snap.Info{producer})
}

func (s *RefreshSuite) TestRefreshRemovesSlotsOnUpgrade(c *C) {
	// Updating a snap can remove an existing slot.
	_, _ = s.refresh(c, testProducerYaml)
	producer, affected := s.refresh(c, testBareProducerYaml)
	// The slot was removed
	c.Check(s.repo.Slot("producer", "iface"), IsNil)
	// The snap itself was affected
	c.Check(affected, DeepEquals, []*snap.Info{producer})
}

func (s *RefreshSuite) TestRefreshRemovesSlotsWithUnknownInterfaceOnUpgrade(c *C) {
	// Updating a snap can change an existing slot to use an unknown interface.
	// This removes the slot.
	_, _ = s.refresh(c, testProducerYaml)
	producer, affected := s.refresh(c, testUnknownIfaceProducerYaml)
	// The slot was removed
	c.Check(s.repo.Slot("producer", "iface"), IsNil)
	// The snap itself was affected
	c.Check(affected, DeepEquals, []*snap.Info{producer})
}

func (s *RefreshSuite) TestRefreshRemovesSlotsWithInvalidInterfaceOnUpgrade(c *C) {
	// Updating a snap can change an existing slot in a way which causes it not
	// to validate. This removes the slot.
	_, _ = s.refresh(c, testProducerYaml)
	producer, affected := s.refresh(c, testInvalidIfaceProducerYaml)
	// The slot was removed
	c.Check(s.repo.Slot("producer", "iface"), IsNil)
	// The snap itself was affected
	c.Check(affected, DeepEquals, []*snap.Info{producer})
}

func (s *RefreshSuite) TestRefreshRemovesSlotAndSeversConnectionOnUpgrade(c *C) {
	// Updating a snap can remove an existing slot.
	// This severs connections made from that slot.
	_, _ = s.refresh(c, testProducerYaml)
	consumer, _ := s.refresh(c, testConsumerYaml)
	err := s.repo.Connect("consumer", "iface", "producer", "iface")
	c.Assert(err, IsNil)
	producer, affected := s.refresh(c, testBareProducerYaml)
	// The producer slot was removed
	c.Check(s.repo.Slot("producer", "iface"), IsNil)
	// The connection was severed
	plug := s.repo.Plug("consumer", "iface")
	c.Check(plug.Connections, HasLen, 0)
	// Both snaps were affected
	c.Check(affected, testutil.Contains, producer)
	c.Check(affected, testutil.Contains, consumer)
}

func (s *RefreshSuite) TestRefreshUpdatesSlotToNewInterfaceAndSeversConnectionOnUpgrade(c *C) {
	// Updating a snap can change a slot interface.
	// This severs connections made from that slot.
	_, _ = s.refresh(c, testProducerYaml)
	consumer, _ := s.refresh(c, testConsumerYaml)
	err := s.repo.Connect("consumer", "iface", "producer", "iface")
	c.Assert(err, IsNil)
	producer, affected := s.refresh(c, testDifferentIfaceProducerYaml)
	// The producer slot was updated
	slot := s.repo.Slot("producer", "iface")
	c.Assert(slot, Not(IsNil))
	c.Check(slot.Interface, Equals, "other-iface")
	// The connection was severed
	plug := s.repo.Plug("consumer", "iface")
	c.Check(plug.Connections, HasLen, 0)
	c.Check(slot.Connections, HasLen, 0)
	// Both snaps were affected
	c.Check(affected, testutil.Contains, producer)
	c.Check(affected, testutil.Contains, consumer)
}

func (s *RefreshSuite) TestRefreshUpdatesSlotToNewAttrsAndSeversConnectionOnUpgrade(c *C) {
	// Updating a snap can change a slot attributes.
	// This severs connections made from that slot.
	_, _ = s.refresh(c, testProducerYaml)
	consumer, _ := s.refresh(c, testConsumerYaml)
	err := s.repo.Connect("consumer", "iface", "producer", "iface")
	c.Assert(err, IsNil)
	producer, affected := s.refresh(c, testDifferentAttrsProducerYaml)
	// The producer slot was updated
	slot := s.repo.Slot("producer", "iface")
	c.Assert(slot, Not(IsNil))
	c.Check(slot.Attrs, DeepEquals, map[string]interface{}{"attr": "extra"})
	// The connection was severed
	plug := s.repo.Plug("consumer", "iface")
	c.Check(plug.Connections, HasLen, 0)
	c.Check(slot.Connections, HasLen, 0)
	// Both snaps were affected
	c.Check(affected, testutil.Contains, producer)
	c.Check(affected, testutil.Contains, consumer)
}

func (s *RefreshSuite) TestRefreshUpdatesSlotToNewLabelAndKeepsConnectionOnUpgrade(c *C) {
	// Updating a snap can change a slot label.
	// This doesn't sever existing connection
	_, _ = s.refresh(c, testProducerYaml)
	_, _ = s.refresh(c, testConsumerYaml)
	err := s.repo.Connect("consumer", "iface", "producer", "iface")
	c.Assert(err, IsNil)
	_, affected := s.refresh(c, testDifferentLabelProducerYaml)
	// The producer slot was updated
	slot := s.repo.Slot("producer", "iface")
	c.Assert(slot, Not(IsNil))
	c.Check(slot.Label, Equals, "New Label")
	// The connection was not severed
	plug := s.repo.Plug("consumer", "iface")
	c.Check(plug.Connections, HasLen, 1)
	c.Check(slot.Connections, HasLen, 1)
	// No snaps are affected by label changes.
	c.Check(affected, HasLen, 0)
}
