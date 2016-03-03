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
	slot      *Slot
	plug      *Plug
	emptyRepo *Repository
	// Repository pre-populated with s.iface
	testRepo *Repository
}

var _ = Suite(&RepositorySuite{
	iface: &TestInterface{
		InterfaceName: "interface",
	},
	slot: &Slot{
		Snap:      "provider",
		Name:      "slot",
		Interface: "interface",
		Label:     "label",
		Attrs:     map[string]interface{}{"attr": "value"},
		Apps:      []string{"meta/hooks/slot"},
	},
	plug: &Plug{
		Snap:      "consumer",
		Name:      "plug",
		Interface: "interface",
		Label:     "label",
		Attrs:     map[string]interface{}{"attr": "value"},
		Apps:      []string{"app"},
	},
})

func (s *RepositorySuite) SetUpTest(c *C) {
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

// Tests for Repository.AddSlot()

func (s *RepositorySuite) TestAddSlot(c *C) {
	c.Assert(s.testRepo.AllSlots(""), HasLen, 0)
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllSlots(""), HasLen, 1)
	c.Assert(s.testRepo.Slot(s.slot.Snap, s.slot.Name), DeepEquals, s.slot)
}

func (s *RepositorySuite) TestAddSlotClash(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, ErrorMatches, `cannot add slot, snap "provider" already has slot "slot"`)
	c.Assert(s.testRepo.AllSlots(""), HasLen, 1)
	c.Assert(s.testRepo.Slot(s.slot.Snap, s.slot.Name), DeepEquals, s.slot)
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

func (s *RepositorySuite) TestAddSlotFailsWithInvalidSlotName(c *C) {
	slot := &Slot{
		Snap:      "snap",
		Name:      "bad-name-",
		Interface: "interface",
	}
	err := s.testRepo.AddSlot(slot)
	c.Assert(err, ErrorMatches, `invalid interface name: "bad-name-"`)
	c.Assert(s.testRepo.AllSlots(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddSlotFailsWithUnknownInterface(c *C) {
	err := s.emptyRepo.AddSlot(s.slot)
	c.Assert(err, ErrorMatches, `cannot add slot, interface "interface" is not known`)
	c.Assert(s.emptyRepo.AllSlots(""), HasLen, 0)
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

// Tests for Repository.Slot()

func (s *RepositorySuite) TestSlot(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	c.Assert(s.emptyRepo.Slot(s.slot.Snap, s.slot.Name), IsNil)
	c.Assert(s.testRepo.Slot(s.slot.Snap, s.slot.Name), DeepEquals, s.slot)
}

func (s *RepositorySuite) TestSlotSearch(c *C) {
	err := s.testRepo.AddSlot(&Slot{
		Snap:      "x",
		Name:      "a",
		Interface: s.slot.Interface,
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{
		Snap:      "x",
		Name:      "b",
		Interface: s.slot.Interface,
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{
		Snap:      "x",
		Name:      "c",
		Interface: s.slot.Interface,
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{
		Snap:      "y",
		Name:      "a",
		Interface: s.slot.Interface,
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{
		Snap:      "y",
		Name:      "b",
		Interface: s.slot.Interface,
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{
		Snap:      "y",
		Name:      "c",
		Interface: s.slot.Interface,
	})
	c.Assert(err, IsNil)
	// Slot() correctly finds slots
	c.Assert(s.testRepo.Slot("x", "a"), Not(IsNil))
	c.Assert(s.testRepo.Slot("x", "b"), Not(IsNil))
	c.Assert(s.testRepo.Slot("x", "c"), Not(IsNil))
	c.Assert(s.testRepo.Slot("y", "a"), Not(IsNil))
	c.Assert(s.testRepo.Slot("y", "b"), Not(IsNil))
	c.Assert(s.testRepo.Slot("y", "c"), Not(IsNil))
}

// Tests for Repository.RemoveSlot()

func (s *RepositorySuite) TestRemoveSlotSucceedsWhenSlotExistsAndDisconnected(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.RemoveSlot(s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllSlots(""), HasLen, 0)
}

func (s *RepositorySuite) TestRemoveSlotFailsWhenSlotDoesntExist(c *C) {
	err := s.emptyRepo.RemoveSlot(s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot remove slot "slot" from snap "provider", no such slot`)
}

func (s *RepositorySuite) TestRemoveSlotFailsWhenSlotIsConnected(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
	// Removing a slot used by a plug returns an appropriate error
	err = s.testRepo.RemoveSlot(s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot remove slot "slot" from snap "provider", it is still connected`)
	// The slot is still there
	plug := s.testRepo.Slot(s.slot.Snap, s.slot.Name)
	c.Assert(plug, Not(IsNil))
}

// Tests for Repository.AllSlots()

func (s *RepositorySuite) TestAllSlotsWithoutInterfaceName(c *C) {
	// Note added in non-sorted order
	err := s.testRepo.AddSlot(&Slot{
		Snap:      "snap-b",
		Name:      "name-a",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{
		Snap:      "snap-b",
		Name:      "name-c",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{
		Snap:      "snap-b",
		Name:      "name-b",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{
		Snap:      "snap-a",
		Name:      "name-a",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	// The result is sorted by snap and name
	c.Assert(s.testRepo.AllSlots(""), DeepEquals, []*Slot{
		&Slot{
			Snap:      "snap-a",
			Name:      "name-a",
			Interface: "interface",
		},
		&Slot{
			Snap:      "snap-b",
			Name:      "name-a",
			Interface: "interface",
		},
		&Slot{
			Snap:      "snap-b",
			Name:      "name-b",
			Interface: "interface",
		},
		&Slot{
			Snap:      "snap-b",
			Name:      "name-c",
			Interface: "interface",
		},
	})
}

func (s *RepositorySuite) TestAllSlotsWithInterfaceName(c *C) {
	// Add another interface so that we can look for it
	err := s.testRepo.AddInterface(&TestInterface{InterfaceName: "other-interface"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{
		Snap:      "snap",
		Name:      "name-a",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{
		Snap:      "snap",
		Name:      "name-b",
		Interface: "other-interface",
	})
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllSlots("other-interface"), DeepEquals, []*Slot{
		&Slot{
			Snap:      "snap",
			Name:      "name-b",
			Interface: "other-interface",
		},
	})
}

// Tests for Repository.Slots()

func (s *RepositorySuite) TestSlots(c *C) {
	// Note added in non-sorted order
	err := s.testRepo.AddSlot(&Slot{
		Snap:      "snap-b",
		Name:      "name-a",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{
		Snap:      "snap-b",
		Name:      "name-c",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{
		Snap:      "snap-b",
		Name:      "name-b",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{
		Snap:      "snap-a",
		Name:      "name-a",
		Interface: "interface",
	})
	c.Assert(err, IsNil)
	// The result is sorted by snap and name
	c.Assert(s.testRepo.Slots("snap-b"), DeepEquals, []*Slot{
		&Slot{
			Snap:      "snap-b",
			Name:      "name-a",
			Interface: "interface",
		},
		&Slot{
			Snap:      "snap-b",
			Name:      "name-b",
			Interface: "interface",
		},
		&Slot{
			Snap:      "snap-b",
			Name:      "name-c",
			Interface: "interface",
		},
	})
	// The result is empty if the snap is not known
	c.Assert(s.testRepo.Slots("snap-x"), HasLen, 0)
}

// Tests for Repository.AllPlugs()

func (s *RepositorySuite) TestAllPlugs(c *C) {
	err := s.testRepo.AddInterface(&TestInterface{InterfaceName: "other-interface"})
	c.Assert(err, IsNil)
	// Add some plugs
	err = s.testRepo.AddPlug(&Plug{Snap: "snap-a", Name: "plug-b", Interface: "interface"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{Snap: "snap-b", Name: "plug-a", Interface: "other-interface"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{Snap: "snap-a", Name: "plug-a", Interface: "interface"})
	c.Assert(err, IsNil)
	// AllPlugs("") returns all plugs, sorted by snap and plug name
	c.Assert(s.testRepo.AllPlugs(""), DeepEquals, []*Plug{
		&Plug{Snap: "snap-a", Name: "plug-a", Interface: "interface"},
		&Plug{Snap: "snap-a", Name: "plug-b", Interface: "interface"},
		&Plug{Snap: "snap-b", Name: "plug-a", Interface: "other-interface"},
	})
	// AllPlugs("") returns all plugs, sorted by snap and plug name
	c.Assert(s.testRepo.AllPlugs("other-interface"), DeepEquals, []*Plug{
		&Plug{Snap: "snap-b", Name: "plug-a", Interface: "other-interface"},
	})
}

// Tests for Repository.Plugs()

func (s *RepositorySuite) TestPlugs(c *C) {
	// Add some plugs
	err := s.testRepo.AddPlug(&Plug{Snap: "snap-a", Name: "plug-b", Interface: "interface"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{Snap: "snap-b", Name: "plug-a", Interface: "interface"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(&Plug{Snap: "snap-a", Name: "plug-a", Interface: "interface"})
	c.Assert(err, IsNil)
	// Plugs("snap-a") returns plugs present in that snap
	c.Assert(s.testRepo.Plugs("snap-a"), DeepEquals, []*Plug{
		&Plug{Snap: "snap-a", Name: "plug-a", Interface: "interface"},
		&Plug{Snap: "snap-a", Name: "plug-b", Interface: "interface"},
	})
	// Plugs("snap-b") returns plugs present in that snap
	c.Assert(s.testRepo.Plugs("snap-b"), DeepEquals, []*Plug{
		&Plug{Snap: "snap-b", Name: "plug-a", Interface: "interface"},
	})
	// Plugs("snap-c") returns no plugs (because that snap doesn't exist)
	c.Assert(s.testRepo.Plugs("snap-c"), HasLen, 0)
	// Plugs("") returns no plugs
	c.Assert(s.testRepo.Plugs(""), HasLen, 0)
}

// Tests for Repository.Plug()

func (s *RepositorySuite) TestPlugSucceedsWhenPlugExists(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	plug := s.testRepo.Plug(s.plug.Snap, s.plug.Name)
	c.Assert(plug, DeepEquals, s.plug)
}

func (s *RepositorySuite) TestPlugFailsWhenPlugDoesntExist(c *C) {
	plug := s.testRepo.Plug(s.plug.Snap, s.plug.Name)
	c.Assert(plug, IsNil)
}

// Tests for Repository.AddPlug()

func (s *RepositorySuite) TestAddPlugFailsWhenInterfaceIsUnknown(c *C) {
	err := s.emptyRepo.AddPlug(s.plug)
	c.Assert(err, ErrorMatches, `cannot add plug, interface "interface" is not known`)
}

func (s *RepositorySuite) TestAddPlugFailsWhenPlugNameIsInvalid(c *C) {
	err := s.emptyRepo.AddPlug(&Plug{Snap: s.plug.Snap, Name: "bad-name-", Interface: s.plug.Interface})
	c.Assert(err, ErrorMatches, `invalid interface name: "bad-name-"`)
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

func (s *RepositorySuite) TestAddPlugFailsForDuplicates(c *C) {
	// Adding the first plug succeeds
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Adding the plug again fails with appropriate error
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, ErrorMatches, `cannot add plug, snap "consumer" already has plug "plug"`)
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

func (s *RepositorySuite) TestAddPlugStoresCorrectData(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	plug := s.testRepo.Plug(s.plug.Snap, s.plug.Name)
	// The added plug has the same data
	c.Assert(plug, DeepEquals, s.plug)
}

// Tests for Repository.RemovePlug()

func (s *RepositorySuite) TestRemovePlugSuccedsWhenPlugExistsAndDisconnected(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Removing a vacant plug simply works
	err = s.testRepo.RemovePlug(s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
	// The plug is gone now
	plug := s.testRepo.Plug(s.plug.Snap, s.plug.Name)
	c.Assert(plug, IsNil)
}

func (s *RepositorySuite) TestRemovePlugFailsWhenPlugDoesntExist(c *C) {
	// Removing a plug that doesn't exist returns an appropriate error
	err := s.testRepo.RemovePlug(s.plug.Snap, s.plug.Name)
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, `cannot remove slot plug "plug" from snap "consumer", no such plug`)
}

func (s *RepositorySuite) TestRemovePlugFailsWhenPlugIsConnected(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
	// Removing a plug occupied by a slot returns an appropriate error
	err = s.testRepo.RemovePlug(s.plug.Snap, s.plug.Name)
	c.Assert(err, ErrorMatches, `cannot remove plug "plug" from snap "consumer", it is still connected`)
	// The plug is still there
	plug := s.testRepo.Plug(s.plug.Snap, s.plug.Name)
	c.Assert(plug, Not(IsNil))
}

// Tests for Repository.Connect()

func (s *RepositorySuite) TestConnectFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Connecting an unknown slot returns an appropriate error
	err = s.testRepo.Connect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, ErrorMatches, `cannot connect slot "slot" from snap "provider", no such slot`)
}

func (s *RepositorySuite) TestConnectFailsWhenPlugDoesNotExist(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Connecting to an unknown plug returns an error
	err = s.testRepo.Connect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, ErrorMatches, `cannot connect slot to plug "plug" from snap "consumer", no such plug`)
}

func (s *RepositorySuite) TestConnectSucceedsWhenIdenticalConnectExists(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
	// Connecting exactly the same thing twice succeeds without an error but does nothing.
	err = s.testRepo.Connect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
	// Only one "connect" is actually present.
	c.Assert(s.testRepo.ConnectedPlugs(s.plug.Snap), DeepEquals, map[*Plug][]*Slot{
		s.plug: []*Slot{s.slot},
	})
}

func (s *RepositorySuite) TestConnectFailsWhenPlugAndSlotAreIncompatible(c *C) {
	otherInterface := &TestInterface{InterfaceName: "other-interface"}
	err := s.testRepo.AddInterface(otherInterface)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{Snap: s.slot.Snap, Name: s.slot.Name, Interface: "other-interface"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Connecting a slot to an incompatible plug fails with an appropriate error
	err = s.testRepo.Connect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, ErrorMatches, `cannot connect slot "provider:slot" \(interface "other-interface"\) to "consumer:plug" \(interface "interface"\)`)
}

func (s *RepositorySuite) TestConnectSucceeds(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Connecting a slot works okay
	err = s.testRepo.Connect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
}

// Tests for Repository.Disconnect()

func (s *RepositorySuite) TestDisconnectFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Disconnecting an unknown slot returns and appropriate error
	err = s.testRepo.Disconnect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect slot "slot" from snap "provider", no such slot`)
}

func (s *RepositorySuite) TestDisconnectFailsWhenPlugDoesNotExist(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Disconnecting from an unknown plug returns an appropriate error
	err = s.testRepo.Disconnect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect slot from plug "plug" from snap "consumer", no such plug`)
}

func (s *RepositorySuite) TestDisconnectFromPlugFailsWhenPlugDoesNotExist(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Disconnecting everything form an unknown plug returns an appropriate error
	err = s.testRepo.Disconnect("", "", s.plug.Snap, s.plug.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect slot from plug "plug" from snap "consumer", no such plug`)
}

func (s *RepositorySuite) TestDisconnectFromSnapFailsWhenPlugDoesNotExist(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Disconnecting all slots from a snap that is not known returns an appropriate error
	err = s.testRepo.Disconnect("", "", s.plug.Snap, "")
	c.Assert(err, ErrorMatches, `cannot disconnect slot from snap "consumer", no such snap`)
}

func (s *RepositorySuite) TestDisconnectFailsWhenNotConnected(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Disconnecting a slot that is not connected returns an appropriate error
	err = s.testRepo.Disconnect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect slot "slot" from snap "provider" from plug "plug" from snap "consumer", it is not connected`)
}

func (s *RepositorySuite) TestDisconnectFromSnapDoesNothingWhenNotConnected(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Disconnecting a all slots from a snap that uses nothing is not an error.
	err = s.testRepo.Disconnect("", "", s.plug.Snap, "")
	c.Assert(err, IsNil)
}

func (s *RepositorySuite) TestDisconnectFromPlugDoesNothingWhenNotConnected(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// Disconnecting a all slots from a plug that uses nothing is not an error.
	err = s.testRepo.Disconnect("", "", s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
}

func (s *RepositorySuite) TestDisconnectSucceeds(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
	// Disconnecting a connected slot works okay
	err = s.testRepo.Disconnect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.ConnectedPlugs(s.plug.Snap), HasLen, 0)
}

func (s *RepositorySuite) TestDisconnectFromSnap(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
	// Disconnecting everything from a snap works OK
	err = s.testRepo.Disconnect("", "", s.plug.Snap, "")
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.ConnectedPlugs(s.plug.Snap), HasLen, 0)
}

func (s *RepositorySuite) TestDisconnectFromPlug(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.testRepo.Connect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
	// Disconnecting everything from a slot plug works OK
	err = s.testRepo.Disconnect("", "", s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.ConnectedPlugs(s.plug.Snap), HasLen, 0)
}

// Test for Repository.ConnectedPlugs()

func (s *RepositorySuite) TestConnectedReturnsNothingForUnknownSnaps(c *C) {
	// Asking about unknown snaps just returns nothing
	c.Assert(s.testRepo.ConnectedPlugs("unknown"), HasLen, 0)
}

func (s *RepositorySuite) TestConnectedReturnsNothingForEmptyString(c *C) {
	// Asking about the empty string just returns nothing
	c.Assert(s.testRepo.ConnectedPlugs(""), HasLen, 0)
}

func (s *RepositorySuite) TestConnectedPlugsReturnsCorrectData(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// After connecting the result is as expected
	err = s.testRepo.Connect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.ConnectedPlugs(s.plug.Snap), DeepEquals, map[*Plug][]*Slot{
		s.plug: []*Slot{s.slot},
	})
	// After revoking the result is empty again
	err = s.testRepo.Disconnect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.ConnectedPlugs(s.plug.Snap), HasLen, 0)
}

// Tests for Repository.ConnectedSlots()

func (s *RepositorySuite) TestConnectedSlotsReturnsNothingForUnknownSnaps(c *C) {
	// Asking about unknown snaps just returns an empty map
	c.Assert(s.testRepo.ConnectedSlots("unknown"), HasLen, 0)
}

func (s *RepositorySuite) TestConnectedSlotsReturnsNothingForEmptyString(c *C) {
	// Asking about the empty string just returns an empty map
	c.Assert(s.testRepo.ConnectedSlots(""), HasLen, 0)
}

func (s *RepositorySuite) TestConnectedSlotsReturnsCorrectData(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// After connecting the result is as expected
	err = s.testRepo.Connect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
	connects := s.testRepo.ConnectedSlots(s.slot.Snap)
	c.Assert(connects, DeepEquals, map[*Slot][]*Plug{
		s.slot: []*Plug{s.plug},
	})
	// After revoking the result is empty again
	err = s.testRepo.Disconnect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.ConnectedSlots(s.slot.Snap), HasLen, 0)
}

// Tests for Repository.SlotConnections()

func (s *RepositorySuite) TestSlotConnectionsReturnsNothingForUnknownSlots(c *C) {
	// Asking about unknown snaps just returns an empty list
	c.Assert(s.testRepo.SlotConnections("unknown", "unknown"), HasLen, 0)
}

func (s *RepositorySuite) TestSlotConnectionsReturnsNothingForEmptyString(c *C) {
	// Asking about the empty string just returns an empty list
	c.Assert(s.testRepo.SlotConnections("", ""), HasLen, 0)
}

func (s *RepositorySuite) TestSlotConnectionsReturnsCorrectData(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	// After connecting the result is as expected
	err = s.testRepo.Connect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
	users := s.testRepo.SlotConnections(s.slot.Snap, s.slot.Name)
	c.Assert(users, DeepEquals, []*Plug{s.plug})
	// After revoking the result is empty again
	err = s.testRepo.Disconnect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.SlotConnections(s.slot.Snap, s.slot.Name), HasLen, 0)
}

// Tests for Repository.SecuritySnippetsForSnap()

func (s *RepositorySuite) TestPlugSnippetsForSnapSuccess(c *C) {
	const testSecurity SecuritySystem = "security"
	iface := &TestInterface{
		InterfaceName: "interface",
		SlotSecuritySnippetCallback: func(slot *Slot, plug *Plug, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == testSecurity {
				return []byte(`producer snippet`), nil
			}
			return nil, ErrUnknownSecurity
		},
		PlugSecuritySnippetCallback: func(slot *Slot, plug *Plug, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == testSecurity {
				return []byte(`consumer snippet`), nil
			}
			return nil, ErrUnknownSecurity
		},
	}
	repo := s.emptyRepo
	c.Assert(repo.AddInterface(iface), IsNil)
	c.Assert(repo.AddSlot(s.slot), IsNil)
	c.Assert(repo.AddPlug(s.plug), IsNil)
	c.Assert(repo.Connect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name), IsNil)
	// Now producer.app should get `producer snippet` and consumer.app should
	// get `consumer snippet`.
	var snippets map[string][][]byte
	snippets, err := repo.SecuritySnippetsForSnap(s.slot.Snap, testSecurity)
	c.Assert(err, IsNil)
	c.Check(snippets, DeepEquals, map[string][][]byte{
		"meta/hooks/slot": [][]byte{
			[]byte(`producer snippet`),
		},
	})
	snippets, err = repo.SecuritySnippetsForSnap(s.plug.Snap, testSecurity)
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
		PlugSecuritySnippetCallback: func(slot *Slot, plug *Plug, securitySystem SecuritySystem) ([]byte, error) {
			return nil, fmt.Errorf("cannot compute snippet for consumer")
		},
		SlotSecuritySnippetCallback: func(slot *Slot, plug *Plug, securitySystem SecuritySystem) ([]byte, error) {
			return nil, fmt.Errorf("cannot compute snippet for provider")
		},
	}
	repo := s.emptyRepo
	c.Assert(repo.AddInterface(iface), IsNil)
	c.Assert(repo.AddSlot(s.slot), IsNil)
	c.Assert(repo.AddPlug(s.plug), IsNil)
	c.Assert(repo.Connect(s.slot.Snap, s.slot.Name, s.plug.Snap, s.plug.Name), IsNil)
	var snippets map[string][][]byte
	snippets, err := repo.SecuritySnippetsForSnap(s.slot.Snap, testSecurity)
	c.Assert(err, ErrorMatches, "cannot compute snippet for provider")
	c.Check(snippets, IsNil)
	snippets, err = repo.SecuritySnippetsForSnap(s.plug.Snap, testSecurity)
	c.Assert(err, ErrorMatches, "cannot compute snippet for consumer")
	c.Check(snippets, IsNil)
}
