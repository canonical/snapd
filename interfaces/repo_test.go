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
	s.plug = &Plug{
		Snap:      "provider",
		Name:      "plug",
		Interface: "interface",
		Label:     "label",
		Attrs:     map[string]interface{}{"attr": "value"},
		Apps:      []string{"meta/hooks/plug"},
	}
	s.slot = &Slot{
		Snap:      "consumer",
		Name:      "slot",
		Interface: "interface",
		Label:     "label",
		Attrs:     map[string]interface{}{"attr": "value"},
		Apps:      []string{"app"},
	}
	s.emptyRepo = NewRepository()
	s.testRepo = NewRepository()
	err := s.testRepo.AddInterface(s.iface)
	c.Assert(err, IsNil)
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
	c.Assert(s.testRepo.Plug(s.plug.Snap, s.plug.Name), DeepEquals, s.plug)
}

func (s *RepositorySuite) TestAddPlugClash(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, ErrorMatches, `cannot add plug, snap "provider" already has plug "plug"`)
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 1)
	c.Assert(s.testRepo.Plug(s.plug.Snap, s.plug.Name), DeepEquals, s.plug)
}

func (s *RepositorySuite) TestAddPlugFailsWithInvalidSnapName(c *C) {
	plug := &Plug{
		Snap:      "bad-snap-",
		Name:      "name",
		Interface: "interface",
	}
	err := s.testRepo.AddPlug(plug)
	c.Assert(err, ErrorMatches, `invalid snap name: "bad-snap-"`)
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddPlugFailsWithInvalidPlugName(c *C) {
	plug := &Plug{
		Snap:      "snap",
		Name:      "bad-name-",
		Interface: "interface",
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
	c.Assert(s.emptyRepo.Plug(s.plug.Snap, s.plug.Name), IsNil)
	c.Assert(s.testRepo.Plug(s.plug.Snap, s.plug.Name), DeepEquals, s.plug)
}

func (s *RepositorySuite) TestPlugSearch(c *C) {
	err := s.testRepo.AddPlug(&Plug{
		Snap:      "x",
		Name:      "a",
		Interface: s.plug.Interface,
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{
		Snap:      "x",
		Name:      "b",
		Interface: s.plug.Interface,
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{
		Snap:      "x",
		Name:      "c",
		Interface: s.plug.Interface,
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{
		Snap:      "y",
		Name:      "a",
		Interface: s.plug.Interface,
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{
		Snap:      "y",
		Name:      "b",
		Interface: s.plug.Interface,
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{
		Snap:      "y",
		Name:      "c",
		Interface: s.plug.Interface,
	})
	c.Assert(err, IsNil)
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
	err = s.testRepo.RemovePlug(s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 0)
}

func (s *RepositorySuite) TestRemovePlugFailsWhenPlugDoesntExist(c *C) {
	err := s.emptyRepo.RemovePlug(s.plug.Snap, s.plug.Name)
	c.Assert(err, ErrorMatches, `cannot remove plug "plug" from snap "provider", no such plug`)
}

func (s *RepositorySuite) TestRemovePlugFailsWhenPlugIsConnected(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Removing a plug used by a slot returns an appropriate error
	err = s.testRepo.RemovePlug(s.plug.Snap, s.plug.Name)
	c.Assert(err, ErrorMatches, `cannot remove plug "plug" from snap "provider", it is still connected`)
	// The plug is still there
	slot := s.testRepo.Plug(s.plug.Snap, s.plug.Name)
	c.Assert(slot, Not(IsNil))
}

// Tests for Repository.AllPlugs()

func (s *RepositorySuite) TestAllPlugsWithoutInterfaceName(c *C) {
	// Note added in non-sorted order
	err := s.testRepo.AddPlug(&Plug{
		Snap:      "snap-b",
		Name:      "name-a",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{
		Snap:      "snap-b",
		Name:      "name-c",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{
		Snap:      "snap-b",
		Name:      "name-b",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{
		Snap:      "snap-a",
		Name:      "name-a",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	// The result is sorted by snap and name
	c.Assert(s.testRepo.AllPlugs(""), DeepEquals, []*Plug{
		&Plug{
			Snap:      "snap-a",
			Name:      "name-a",
			Interface: "interface",
		},
		&Plug{
			Snap:      "snap-b",
			Name:      "name-a",
			Interface: "interface",
		},
		&Plug{
			Snap:      "snap-b",
			Name:      "name-b",
			Interface: "interface",
		},
		&Plug{
			Snap:      "snap-b",
			Name:      "name-c",
			Interface: "interface",
		},
	})
}

func (s *RepositorySuite) TestAllPlugsWithInterfaceName(c *C) {
	// Add another interface so that we can look for it
	err := s.testRepo.AddInterface(&TestInterface{InterfaceName: "other-interface"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{
		Snap:      "snap",
		Name:      "name-a",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{
		Snap:      "snap",
		Name:      "name-b",
		Interface: "other-interface",
	})
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllPlugs("other-interface"), DeepEquals, []*Plug{
		&Plug{
			Snap:      "snap",
			Name:      "name-b",
			Interface: "other-interface",
		},
	})
}

// Tests for Repository.Plugs()

func (s *RepositorySuite) TestPlugs(c *C) {
	// Note added in non-sorted order
	err := s.testRepo.AddPlug(&Plug{
		Snap:      "snap-b",
		Name:      "name-a",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{
		Snap:      "snap-b",
		Name:      "name-c",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{
		Snap:      "snap-b",
		Name:      "name-b",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{
		Snap:      "snap-a",
		Name:      "name-a",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	// The result is sorted by snap and name
	c.Assert(s.testRepo.Plugs("snap-b"), DeepEquals, []*Plug{
		&Plug{
			Snap:      "snap-b",
			Name:      "name-a",
			Interface: "interface",
		},
		&Plug{
			Snap:      "snap-b",
			Name:      "name-b",
			Interface: "interface",
		},
		&Plug{
			Snap:      "snap-b",
			Name:      "name-c",
			Interface: "interface",
		},
	})
	// The result is empty if the snap is not known
	c.Assert(s.testRepo.Plugs("snap-x"), HasLen, 0)
}

// Tests for Repository.AllSlots()

func (s *RepositorySuite) TestAllSlots(c *C) {
	err := s.testRepo.AddInterface(&TestInterface{InterfaceName: "other-interface"})
	c.Assert(err, IsNil)
	// Add some slots
	err = s.testRepo.AddSlot(&Slot{Snap: "snap-a", Name: "slot-b", Interface: "interface"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{Snap: "snap-b", Name: "slot-a", Interface: "other-interface"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{Snap: "snap-a", Name: "slot-a", Interface: "interface"})
	c.Assert(err, IsNil)
	// AllSlots("") returns all slots, sorted by snap and slot name
	c.Assert(s.testRepo.AllSlots(""), DeepEquals, []*Slot{
		&Slot{Snap: "snap-a", Name: "slot-a", Interface: "interface"},
		&Slot{Snap: "snap-a", Name: "slot-b", Interface: "interface"},
		&Slot{Snap: "snap-b", Name: "slot-a", Interface: "other-interface"},
	})
	// AllSlots("") returns all slots, sorted by snap and slot name
	c.Assert(s.testRepo.AllSlots("other-interface"), DeepEquals, []*Slot{
		&Slot{Snap: "snap-b", Name: "slot-a", Interface: "other-interface"},
	})
}

// Tests for Repository.Slots()

func (s *RepositorySuite) TestSlots(c *C) {
	// Add some slots
	err := s.testRepo.AddSlot(&Slot{Snap: "snap-a", Name: "slot-b", Interface: "interface"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{Snap: "snap-b", Name: "slot-a", Interface: "interface"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{Snap: "snap-a", Name: "slot-a", Interface: "interface"})
	c.Assert(err, IsNil)
	// Slots("snap-a") returns slots present in that snap
	c.Assert(s.testRepo.Slots("snap-a"), DeepEquals, []*Slot{
		&Slot{Snap: "snap-a", Name: "slot-a", Interface: "interface"},
		&Slot{Snap: "snap-a", Name: "slot-b", Interface: "interface"},
	})
	// Slots("snap-b") returns slots present in that snap
	c.Assert(s.testRepo.Slots("snap-b"), DeepEquals, []*Slot{
		&Slot{Snap: "snap-b", Name: "slot-a", Interface: "interface"},
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
	slot := s.testRepo.Slot(s.slot.Snap, s.slot.Name)
	c.Assert(slot, DeepEquals, s.slot)
}

func (s *RepositorySuite) TestSlotFailsWhenSlotDoesntExist(c *C) {
	slot := s.testRepo.Slot(s.slot.Snap, s.slot.Name)
	c.Assert(slot, IsNil)
}

// Tests for Repository.AddSlot()

func (s *RepositorySuite) TestAddSlotFailsWhenInterfaceIsUnknown(c *C) {
	err := s.emptyRepo.AddSlot(s.slot)
	c.Assert(err, ErrorMatches, `cannot add slot, interface "interface" is not known`)
}

func (s *RepositorySuite) TestAddSlotFailsWhenSlotNameIsInvalid(c *C) {
	err := s.emptyRepo.AddSlot(&Slot{Snap: s.slot.Snap, Name: "bad-name-", Interface: s.slot.Interface})
	c.Assert(err, ErrorMatches, `invalid interface name: "bad-name-"`)
}

func (s *RepositorySuite) TestAddSlotFailsWithInvalidSnapName(c *C) {
	slot := &Slot{
		Snap:      "bad-snap-",
		Name:      "name",
		Interface: "interface",
	}
	err := s.testRepo.AddSlot(slot)
	c.Assert(err, ErrorMatches, `invalid snap name: "bad-snap-"`)
	c.Assert(s.testRepo.AllSlots(""), HasLen, 0)
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
	slot := s.testRepo.Slot(s.slot.Snap, s.slot.Name)
	// The added slot has the same data
	c.Assert(slot, DeepEquals, s.slot)
}

// Tests for Repository.RemoveSlot()

func (s *RepositorySuite) TestRemoveSlotSuccedsWhenSlotExistsAndDisconnected(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Removing a vacant slot simply works
	err = s.testRepo.RemoveSlot(s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// The slot is gone now
	slot := s.testRepo.Slot(s.slot.Snap, s.slot.Name)
	c.Assert(slot, IsNil)
}

func (s *RepositorySuite) TestRemoveSlotFailsWhenSlotDoesntExist(c *C) {
	// Removing a slot that doesn't exist returns an appropriate error
	err := s.testRepo.RemoveSlot(s.slot.Snap, s.slot.Name)
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, `cannot remove plug slot "slot" from snap "consumer", no such slot`)
}

func (s *RepositorySuite) TestRemoveSlotFailsWhenSlotIsConnected(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Removing a slot occupied by a plug returns an appropriate error
	err = s.testRepo.RemoveSlot(s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot remove slot "slot" from snap "consumer", it is still connected`)
	// The slot is still there
	slot := s.testRepo.Slot(s.slot.Snap, s.slot.Name)
	c.Assert(slot, Not(IsNil))
}

// Tests for Repository.Connect()

func (s *RepositorySuite) TestConnectFailsWhenPlugDoesNotExist(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Connecting an unknown plug returns an appropriate error
	err = s.testRepo.Connect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot connect plug "plug" from snap "provider", no such plug`)
}

func (s *RepositorySuite) TestConnectFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Connecting to an unknown slot returns an error
	err = s.testRepo.Connect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot connect plug to slot "slot" from snap "consumer", no such slot`)
}

func (s *RepositorySuite) TestConnectSucceedsWhenIdenticalConnectExists(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Connecting exactly the same thing twice succeeds without an error but does nothing.
	err = s.testRepo.Connect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Only one "connect" is actually present.
	c.Assert(s.testRepo.Interfaces().Slots[0].Snap, Equals, s.slot.Snap)
	c.Assert(s.testRepo.Interfaces().Slots[0].Name, Equals, s.slot.Name)
	c.Assert(s.testRepo.Interfaces().Slots[0].Connections, HasLen, 1)
	c.Assert(s.testRepo.Interfaces().Slots[0].Connections[0].Snap, Equals, s.plug.Snap)
	c.Assert(s.testRepo.Interfaces().Slots[0].Connections[0].Name, Equals, s.plug.Name)
	c.Assert(s.testRepo.Interfaces().Plugs[0].Snap, Equals, s.plug.Snap)
	c.Assert(s.testRepo.Interfaces().Plugs[0].Name, Equals, s.plug.Name)
	c.Assert(s.testRepo.Interfaces().Plugs[0].Connections, HasLen, 1)
	c.Assert(s.testRepo.Interfaces().Plugs[0].Connections[0].Snap, Equals, s.slot.Snap)
	c.Assert(s.testRepo.Interfaces().Plugs[0].Connections[0].Name, Equals, s.slot.Name)
}

func (s *RepositorySuite) TestConnectFailsWhenSlotAndPlugAreIncompatible(c *C) {
	otherInterface := &TestInterface{InterfaceName: "other-interface"}
	err := s.testRepo.AddInterface(otherInterface)
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{Snap: s.plug.Snap, Name: s.plug.Name, Interface: "other-interface"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Connecting a plug to an incompatible slot fails with an appropriate error
	err = s.testRepo.Connect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot connect plug "provider:plug" \(interface "other-interface"\) to "consumer:slot" \(interface "interface"\)`)
}

func (s *RepositorySuite) TestConnectSucceeds(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Connecting a plug works okay
	err = s.testRepo.Connect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
}

// Tests for Repository.Disconnect()

func (s *RepositorySuite) TestDisconnectFailsWhenPlugDoesNotExist(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Disconnecting an unknown plug returns and appropriate error
	err = s.testRepo.Disconnect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect plug "plug" from snap "provider", no such plug`)
}

func (s *RepositorySuite) TestDisconnectFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Disconnecting from an unknown slot returns an appropriate error
	err = s.testRepo.Disconnect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect plug from slot "slot" from snap "consumer", no such slot`)
}

func (s *RepositorySuite) TestDisconnectFromSlotFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Disconnecting everything form an unknown slot returns an appropriate error
	err = s.testRepo.Disconnect("", "", s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect plug from slot "slot" from snap "consumer", no such slot`)
}

func (s *RepositorySuite) TestDisconnectFromSnapFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Disconnecting all plugs from a snap that is not known returns an appropriate error
	err = s.testRepo.Disconnect("", "", s.slot.Snap, "")
	c.Assert(err, ErrorMatches, `cannot disconnect plug from snap "consumer", no such snap`)
}

func (s *RepositorySuite) TestDisconnectFailsWhenNotConnected(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Disconnecting a plug that is not connected returns an appropriate error
	err = s.testRepo.Disconnect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect plug "plug" from snap "provider" from slot "slot" from snap "consumer", it is not connected`)
}

func (s *RepositorySuite) TestDisconnectFromSnapDoesNothingWhenNotConnected(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Disconnecting a all plugs from a snap that uses nothing is not an error.
	err = s.testRepo.Disconnect("", "", s.slot.Snap, "")
	c.Assert(err, IsNil)
}

func (s *RepositorySuite) TestDisconnectFromSlotDoesNothingWhenNotConnected(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Disconnecting a all plugs from a slot that uses nothing is not an error.
	err = s.testRepo.Disconnect("", "", s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
}

func (s *RepositorySuite) TestDisconnectSucceeds(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Disconnecting a connected plug works okay
	err = s.testRepo.Disconnect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.Interfaces().Slots[0].Snap, Equals, s.slot.Snap)
	c.Assert(s.testRepo.Interfaces().Slots[0].Name, Equals, s.slot.Name)
	c.Assert(s.testRepo.Interfaces().Slots[0].Connections, HasLen, 0)
	c.Assert(s.testRepo.Interfaces().Plugs[0].Snap, Equals, s.plug.Snap)
	c.Assert(s.testRepo.Interfaces().Plugs[0].Name, Equals, s.plug.Name)
	c.Assert(s.testRepo.Interfaces().Plugs[0].Connections, HasLen, 0)
}

func (s *RepositorySuite) TestDisconnectFromSnap(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Disconnecting everything from a snap works OK
	err = s.testRepo.Disconnect("", "", s.slot.Snap, "")
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.Interfaces().Slots[0].Snap, Equals, s.slot.Snap)
	c.Assert(s.testRepo.Interfaces().Slots[0].Name, Equals, s.slot.Name)
	c.Assert(s.testRepo.Interfaces().Slots[0].Connections, HasLen, 0)
	c.Assert(s.testRepo.Interfaces().Plugs[0].Snap, Equals, s.plug.Snap)
	c.Assert(s.testRepo.Interfaces().Plugs[0].Name, Equals, s.plug.Name)
	c.Assert(s.testRepo.Interfaces().Plugs[0].Connections, HasLen, 0)
}

func (s *RepositorySuite) TestDisconnectFromSlot(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Disconnecting everything from a plug slot works OK
	err = s.testRepo.Disconnect("", "", s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.Interfaces().Slots[0].Snap, Equals, s.slot.Snap)
	c.Assert(s.testRepo.Interfaces().Slots[0].Name, Equals, s.slot.Name)
	c.Assert(s.testRepo.Interfaces().Slots[0].Connections, HasLen, 0)
	c.Assert(s.testRepo.Interfaces().Plugs[0].Snap, Equals, s.plug.Snap)
	c.Assert(s.testRepo.Interfaces().Plugs[0].Name, Equals, s.plug.Name)
	c.Assert(s.testRepo.Interfaces().Plugs[0].Connections, HasLen, 0)
}

// Tests for Repository.PlugConnections()

func (s *RepositorySuite) TestPlugConnectionsReturnsNothingForUnknownPlugs(c *C) {
	// Asking about unknown snaps just returns an empty list
	c.Assert(s.testRepo.PlugConnections("unknown", "unknown"), HasLen, 0)
}

func (s *RepositorySuite) TestPlugConnectionsReturnsNothingForEmptyString(c *C) {
	// Asking about the empty string just returns an empty list
	c.Assert(s.testRepo.PlugConnections("", ""), HasLen, 0)
}

func (s *RepositorySuite) TestPlugConnectionsReturnsCorrectData(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// After connecting the result is as expected
	err = s.testRepo.Connect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	users := s.testRepo.PlugConnections(s.plug.Snap, s.plug.Name)
	c.Assert(users, DeepEquals, []*Slot{s.slot})
	// After disconnecting the result is empty again
	err = s.testRepo.Disconnect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.PlugConnections(s.plug.Snap, s.plug.Name), HasLen, 0)
}

// Tests for Repository.Interfaces()

func (s *RepositorySuite) TestInterfacesSmokeTest(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// After connecting the result is as expected
	err = s.testRepo.Connect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	ifaces := s.testRepo.Interfaces()
	c.Assert(ifaces, DeepEquals, &Interfaces{
		Plugs: []*Plug{
			&Plug{
				Name:      s.plug.Name,
				Snap:      s.plug.Snap,
				Interface: s.plug.Interface,
				Label:     s.plug.Label,
				Connections: []SlotRef{
					{
						Snap: s.slot.Snap,
						Name: s.slot.Name,
					},
				},
			},
		},
		Slots: []*Slot{
			&Slot{
				Snap:      s.slot.Snap,
				Name:      s.slot.Name,
				Interface: s.slot.Interface,
				Label:     s.slot.Label,
				Connections: []PlugRef{
					{
						Snap: s.plug.Snap,
						Name: s.plug.Name,
					},
				},
			},
		},
	})
	// After disconnecting the connections become empty
	err = s.testRepo.Disconnect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	ifaces = s.testRepo.Interfaces()
	c.Assert(ifaces, DeepEquals, &Interfaces{
		Plugs: []*Plug{
			&Plug{
				Name:      s.plug.Name,
				Snap:      s.plug.Snap,
				Interface: s.plug.Interface,
				Label:     s.plug.Label,
			},
		},
		Slots: []*Slot{
			&Slot{
				Snap:      s.slot.Snap,
				Name:      s.slot.Name,
				Interface: s.slot.Interface,
				Label:     s.slot.Label,
			},
		},
	})
}

// Tests for Repository.SecuritySnippetsForSnap()

func (s *RepositorySuite) TestSlotSnippetsForSnapSuccess(c *C) {
	const testSecurity SecuritySystem = "security"
	iface := &TestInterface{
		InterfaceName: "interface",
		PlugSecuritySnippetCallback: func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == testSecurity {
				return []byte(`producer snippet`), nil
			}
			return nil, ErrUnknownSecurity
		},
		SlotSecuritySnippetCallback: func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == testSecurity {
				return []byte(`consumer snippet`), nil
			}
			return nil, ErrUnknownSecurity
		},
	}
	repo := s.emptyRepo
	c.Assert(repo.AddInterface(iface), IsNil)
	c.Assert(repo.AddPlug(s.plug), IsNil)
	c.Assert(repo.AddSlot(s.slot), IsNil)
	c.Assert(repo.Connect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name), IsNil)
	// Now producer.app should get `producer snippet` and consumer.app should
	// get `consumer snippet`.
	var snippets map[string][][]byte
	snippets, err := repo.SecuritySnippetsForSnap(s.plug.Snap, testSecurity)
	c.Assert(err, IsNil)
	c.Check(snippets, DeepEquals, map[string][][]byte{
		"meta/hooks/plug": [][]byte{
			[]byte(`producer snippet`),
		},
	})
	snippets, err = repo.SecuritySnippetsForSnap(s.slot.Snap, testSecurity)
	c.Assert(err, IsNil)
	c.Check(snippets, DeepEquals, map[string][][]byte{
		"app": [][]byte{
			[]byte(`consumer snippet`),
		},
	})
}

func (s *RepositorySuite) TestSecuritySnippetsForSnapFailure(c *C) {
	var testSecurity SecuritySystem = "security"
	iface := &TestInterface{
		InterfaceName: "interface",
		SlotSecuritySnippetCallback: func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			return nil, fmt.Errorf("cannot compute snippet for consumer")
		},
		PlugSecuritySnippetCallback: func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			return nil, fmt.Errorf("cannot compute snippet for provider")
		},
	}
	repo := s.emptyRepo
	c.Assert(repo.AddInterface(iface), IsNil)
	c.Assert(repo.AddPlug(s.plug), IsNil)
	c.Assert(repo.AddSlot(s.slot), IsNil)
	c.Assert(repo.Connect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name), IsNil)
	var snippets map[string][][]byte
	snippets, err := repo.SecuritySnippetsForSnap(s.plug.Snap, testSecurity)
	c.Assert(err, ErrorMatches, "cannot compute snippet for provider")
	c.Check(snippets, IsNil)
	snippets, err = repo.SecuritySnippetsForSnap(s.slot.Snap, testSecurity)
	c.Assert(err, ErrorMatches, "cannot compute snippet for consumer")
	c.Check(snippets, IsNil)
}
