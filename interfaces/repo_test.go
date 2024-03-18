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
	"strings"

	. "gopkg.in/check.v1"

	. "github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type RepositorySuite struct {
	testutil.BaseTest
	iface Interface

	consumer *SnapAppSet
	producer *SnapAppSet

	consumerPlug     *snap.PlugInfo
	producerSelfPlug *snap.PlugInfo
	producerSlot     *snap.SlotInfo
	emptyRepo        *Repository
	// Repository pre-populated with s.iface
	testRepo *Repository

	// "Core"-like snaps with the same set of
	coreSnapAppSet *SnapAppSet
	coreSnap       *snap.Info

	ubuntuCoreSnapAppSet *SnapAppSet
	ubuntuCoreSnap       *snap.Info

	snapdSnapAppSet *SnapAppSet
	snapdSnap       *snap.Info
}

var _ = Suite(&RepositorySuite{
	iface: &ifacetest.TestInterface{
		InterfaceName: "interface",
	},
})

const consumerYaml = `
name: consumer
version: 0
apps:
    app:
hooks:
    configure:
plugs:
    plug:
        interface: interface
        label: label
        attr: value
`

const producerYaml = `
name: producer
version: 0
apps:
    app:
hooks:
    configure:
slots:
    slot:
        interface: interface
        label: label
        attr: value
plugs:
    self:
        interface: interface
        label: label
`

func (s *RepositorySuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.consumer = ifacetest.MockInfoAndAppSet(c, consumerYaml, nil, nil)
	s.consumerPlug = s.consumer.Info().Plugs["plug"]
	s.producer = ifacetest.MockInfoAndAppSet(c, producerYaml, nil, nil)
	s.producerSlot = s.producer.Info().Slots["slot"]
	s.producerSelfPlug = s.producer.Info().Plugs["self"]
	// NOTE: Each of the snaps below have one slot so that they can be picked
	// up by the repository. Some tests rename the "slot" slot as appropriate.
	s.ubuntuCoreSnapAppSet = ifacetest.MockInfoAndAppSet(c, `
name: ubuntu-core
version: 0
type: os
slots:
    slot:
        interface: interface
`, nil, nil)
	s.ubuntuCoreSnap = s.ubuntuCoreSnapAppSet.Info()
	// NOTE: The core snap has a slot so that it shows up in the
	// repository. The repository doesn't record snaps unless they
	// have at least one interface.
	s.coreSnapAppSet = ifacetest.MockInfoAndAppSet(c, `
name: core
version: 0
type: os
slots:
    slot:
        interface: interface
`, nil, nil)
	s.coreSnap = s.coreSnapAppSet.Info()
	s.snapdSnapAppSet = ifacetest.MockInfoAndAppSet(c, `
name: snapd
version: 0
type: app
slots:
    slot:
        interface: interface
`, nil, nil)
	s.snapdSnap = s.snapdSnapAppSet.Info()

	s.emptyRepo = NewRepository()
	s.testRepo = NewRepository()
	err := s.testRepo.AddInterface(s.iface)
	c.Assert(err, IsNil)
}

func (s *RepositorySuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

type instanceNameAndYaml struct {
	Name string
	Yaml string
}

func addPlugsSlotsFromInstances(c *C, repo *Repository, iys []instanceNameAndYaml) []*SnapAppSet {
	result := make([]*SnapAppSet, 0, len(iys))
	for _, iy := range iys {
		set := ifacetest.MockInfoAndAppSet(c, iy.Yaml, nil, nil)
		if iy.Name != "" {
			instanceName := iy.Name
			c.Assert(snap.ValidateInstanceName(instanceName), IsNil)
			_, set.Info().InstanceKey = snap.SplitInstanceName(instanceName)
		}

		result = append(result, set)
		repo.AddAppSet(set)
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

// Tests for Repository.AllInterfaces()

func (s *RepositorySuite) TestAllInterfaces(c *C) {
	c.Assert(s.emptyRepo.AllInterfaces(), HasLen, 0)
	c.Assert(s.testRepo.AllInterfaces(), DeepEquals, []Interface{s.iface})

	// Add three interfaces in some non-sorted order.
	i1 := &ifacetest.TestInterface{InterfaceName: "i1"}
	i2 := &ifacetest.TestInterface{InterfaceName: "i2"}
	i3 := &ifacetest.TestInterface{InterfaceName: "i3"}
	c.Assert(s.emptyRepo.AddInterface(i3), IsNil)
	c.Assert(s.emptyRepo.AddInterface(i1), IsNil)
	c.Assert(s.emptyRepo.AddInterface(i2), IsNil)

	// The result is always sorted.
	c.Assert(s.emptyRepo.AllInterfaces(), DeepEquals, []Interface{i1, i2, i3})

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
	// The order of insertion is retained.
	c.Assert(s.emptyRepo.Backends(), DeepEquals, []SecurityBackend{b2, b1})
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

// Tests for Repository.AddAppSet()

func buildAppSetWithPlugsAndSlots(c *C, name string, plugs []*snap.PlugInfo, slots []*snap.SlotInfo) *SnapAppSet {
	sn := &snap.Info{
		SuggestedName: name,
		Version:       "1",
		SnapType:      "app",
		Slots:         make(map[string]*snap.SlotInfo),
		Plugs:         make(map[string]*snap.PlugInfo),
	}

	for _, plug := range plugs {
		plug.Snap = sn
		sn.Plugs[plug.Name] = plug
	}

	for _, slot := range slots {
		slot.Snap = sn
		sn.Slots[slot.Name] = slot
	}
	set, err := NewSnapAppSet(sn, nil)
	c.Assert(err, IsNil)
	return set
}

func (s *RepositorySuite) TestAddAppSetFailsWithInvalidSnapName(c *C) {
	set := buildAppSetWithPlugsAndSlots(c, "bad-snap-", nil, nil)
	err := s.testRepo.AddAppSet(set)
	c.Assert(err, ErrorMatches, `invalid snap name: "bad-snap-"`)
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddAppSetFailsWithInvalidPlugName(c *C) {
	set := buildAppSetWithPlugsAndSlots(c, "snap", []*snap.PlugInfo{
		{Name: "bad-name-", Interface: "interface"},
	}, nil)
	err := s.testRepo.AddAppSet(set)
	c.Assert(err, ErrorMatches, `invalid plug name: "bad-name-"`)
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddAppSetFailsWithInvalidSlotName(c *C) {
	set := buildAppSetWithPlugsAndSlots(c, "snap", nil, []*snap.SlotInfo{
		{Name: "bad-name-", Interface: "interface"},
	})
	err := s.emptyRepo.AddAppSet(set)
	c.Assert(err, ErrorMatches, `invalid slot name: "bad-name-"`)
	c.Assert(s.emptyRepo.AllSlots(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddAppSetStoresCorrectData(c *C) {
	err := s.testRepo.AddAppSet(s.producer)
	c.Assert(err, IsNil)
	slot := s.testRepo.Slot(s.producerSlot.Snap.InstanceName(), s.producerSlot.Name)
	// The added slot has the same data
	c.Assert(slot, DeepEquals, s.producerSlot)
}

func (s *RepositorySuite) TestAddAppSetParallelInstance(c *C) {
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 0)

	err := s.testRepo.AddAppSet(s.consumer)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 1)

	consumer := ifacetest.MockInfoAndAppSet(c, consumerYaml, nil, nil)
	consumer.Info().InstanceKey = "instance"

	err = s.testRepo.AddAppSet(consumer)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 2)

	c.Assert(s.testRepo.Plug(s.consumer.InstanceName(), s.consumerPlug.Name), DeepEquals, s.consumerPlug)
	c.Assert(s.testRepo.Plug(consumer.InstanceName(), "plug"), DeepEquals, consumer.Info().Plugs["plug"])
}

// Tests for Repository.AddSlot()

func (s *RepositorySuite) TestAddSlotClashingSlotFromAppSet(c *C) {
	slot := &snap.SlotInfo{
		Snap:      s.producer.Info(),
		Name:      "slot",
		Interface: "interface",
	}

	// Adding the app set succeeds
	err := s.testRepo.AddAppSet(s.producer)
	c.Assert(err, IsNil)
	// Adding the slot again fails with appropriate error
	err = s.testRepo.AddSlot(slot)
	c.Assert(err, ErrorMatches, `snap "producer" has slots conflicting on name "slot"`)
}

func (s *RepositorySuite) TestAddSlotClashingSlot(c *C) {
	yaml := `
name: producer
version: 0
apps:
  app:
hooks:
  configure:
    `
	producer := ifacetest.MockInfoAndAppSet(c, yaml, nil, nil)

	err := s.testRepo.AddAppSet(producer)
	c.Assert(err, IsNil)

	slot := &snap.SlotInfo{
		Snap:      producer.Info(),
		Name:      "slot",
		Interface: "interface",
	}

	// Adding the first slot succeeds
	err = s.testRepo.AddSlot(slot)
	c.Assert(err, IsNil)
	// Adding the slot again fails with appropriate error
	err = s.testRepo.AddSlot(slot)
	c.Assert(err, ErrorMatches, `snap "producer" has slots conflicting on name "slot"`)
}

func (s *RepositorySuite) TestAddSlotNoMatchingAppSet(c *C) {
	yaml := `
name: producer
version: 0
apps:
  app:
hooks:
  configure:
    `
	producer := ifacetest.MockInfoAndAppSet(c, yaml, nil, nil)

	slot := &snap.SlotInfo{
		Snap:      producer.Info(),
		Name:      "slot",
		Interface: "interface",
	}

	// should fail, since we haven't seen the slot's associated app set yet
	err := s.testRepo.AddSlot(slot)
	c.Assert(err, ErrorMatches, `cannot add slot, snap "producer" is not known`)
}

func (s *RepositorySuite) TestAddSlotClashingPlug(c *C) {
	err := s.testRepo.AddAppSet(s.consumer)
	c.Assert(err, IsNil)

	slot := &snap.SlotInfo{
		Snap:      s.consumer.Info(),
		Name:      "plug",
		Interface: "interface",
	}
	err = s.testRepo.AddSlot(slot)

	c.Assert(err, ErrorMatches, `snap "consumer" has plug and slot conflicting on name "plug"`)
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 1)
	c.Assert(s.testRepo.Plug(s.consumer.InstanceName(), "plug"), DeepEquals, s.consumerPlug)
}

func (s *RepositorySuite) TestAddSlotStoresCorrectData(c *C) {
	yaml := `
name: producer
version: 0
apps:
  app:
hooks:
  configure:
    `
	producer := ifacetest.MockInfoAndAppSet(c, yaml, nil, nil)
	c.Assert(s.testRepo.AddAppSet(producer), IsNil)

	slot := &snap.SlotInfo{
		Snap:      producer.Info(),
		Name:      "slot",
		Interface: "interface",
	}

	err := s.testRepo.AddSlot(slot)
	c.Assert(err, IsNil)
	found := s.testRepo.Slot(slot.Snap.InstanceName(), slot.Name)
	// The added slot has the same data
	c.Assert(found, DeepEquals, slot)
}

func (s *RepositorySuite) TestAddSlotParallelInstance(c *C) {
	yaml := `
name: producer
version: 0
apps:
  app:
hooks:
  configure:
    `
	producer := ifacetest.MockInfoAndAppSet(c, yaml, nil, nil)
	c.Assert(s.testRepo.AddAppSet(producer), IsNil)

	c.Assert(s.testRepo.AllSlots(""), HasLen, 0)

	slot := &snap.SlotInfo{
		Snap:      producer.Info(),
		Name:      "slot",
		Interface: "interface",
	}

	err := s.testRepo.AddSlot(slot)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllSlots(""), HasLen, 1)

	producerInstance := ifacetest.MockInfoAndAppSet(c, yaml, nil, nil)
	producerInstance.Info().InstanceKey = "instance"
	c.Assert(s.testRepo.AddAppSet(producerInstance), IsNil)

	slotInstance := &snap.SlotInfo{
		Snap:      producerInstance.Info(),
		Name:      "slot",
		Interface: "interface",
	}

	err = s.testRepo.AddSlot(slotInstance)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllSlots(""), HasLen, 2)

	c.Assert(s.testRepo.Slot(slot.Snap.InstanceName(), slot.Name), DeepEquals, slot)
	c.Assert(s.testRepo.Slot(slotInstance.Snap.InstanceName(), "slot"), DeepEquals, slotInstance)
}

// Tests for Repository.Plug()

func (s *RepositorySuite) TestPlug(c *C) {
	err := s.testRepo.AddAppSet(s.consumer)
	c.Assert(err, IsNil)
	c.Assert(s.emptyRepo.Plug(s.consumerPlug.Snap.InstanceName(), s.consumerPlug.Name), IsNil)
	c.Assert(s.testRepo.Plug(s.consumerPlug.Snap.InstanceName(), s.consumerPlug.Name), DeepEquals, s.consumerPlug)
}

func (s *RepositorySuite) TestPlugSearch(c *C) {
	addPlugsSlotsFromInstances(c, s.testRepo, []instanceNameAndYaml{
		{Name: "xx", Yaml: `
name: xx
version: 0
plugs:
    a: interface
    b: interface
    c: interface
`},
		{Name: "yy", Yaml: `
name: yy
version: 0
plugs:
    a: interface
    b: interface
    c: interface
`},
		{Name: "zz_instance", Yaml: `
name: zz
version: 0
plugs:
    a: interface
    b: interface
    c: interface
`},
	})
	// Plug() correctly finds plugs
	c.Assert(s.testRepo.Plug("xx", "a"), Not(IsNil))
	c.Assert(s.testRepo.Plug("xx", "b"), Not(IsNil))
	c.Assert(s.testRepo.Plug("xx", "c"), Not(IsNil))
	c.Assert(s.testRepo.Plug("yy", "a"), Not(IsNil))
	c.Assert(s.testRepo.Plug("yy", "b"), Not(IsNil))
	c.Assert(s.testRepo.Plug("yy", "c"), Not(IsNil))
	c.Assert(s.testRepo.Plug("zz_instance", "a"), Not(IsNil))
	c.Assert(s.testRepo.Plug("zz_instance", "b"), Not(IsNil))
	c.Assert(s.testRepo.Plug("zz_instance", "c"), Not(IsNil))
}

// Tests for Repository.RemovePlug()

func (s *RepositorySuite) TestRemovePlugSucceedsWhenPlugExistsAndDisconnected(c *C) {
	err := s.testRepo.AddAppSet(s.consumer)
	c.Assert(err, IsNil)
	err = s.testRepo.RemovePlug(s.consumerPlug.Snap.InstanceName(), s.consumerPlug.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllPlugs(""), HasLen, 0)
}

func (s *RepositorySuite) TestRemovePlugFailsWhenPlugDoesntExist(c *C) {
	err := s.emptyRepo.RemovePlug(s.consumerPlug.Snap.InstanceName(), s.consumerPlug.Name)
	c.Assert(err, ErrorMatches, `cannot remove plug "plug" from snap "consumer", no such plug`)
}

func (s *RepositorySuite) TestRemovePlugFailsWhenPlugIsConnected(c *C) {
	err := s.testRepo.AddAppSet(s.consumer)
	c.Assert(err, IsNil)
	err = s.testRepo.AddAppSet(s.producer)
	c.Assert(err, IsNil)
	connRef := NewConnRef(s.consumerPlug, s.producerSlot)
	_, err = s.testRepo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	// Removing a plug used by a slot returns an appropriate error
	err = s.testRepo.RemovePlug(s.consumerPlug.Snap.InstanceName(), s.consumerPlug.Name)
	c.Assert(err, ErrorMatches, `cannot remove plug "plug" from snap "consumer", it is still connected`)
	// The plug is still there
	slot := s.testRepo.Plug(s.consumerPlug.Snap.InstanceName(), s.consumerPlug.Name)
	c.Assert(slot, Not(IsNil))
}

// Tests for Repository.AllPlugs()

func (s *RepositorySuite) TestAllPlugsWithoutInterfaceName(c *C) {
	snaps := addPlugsSlotsFromInstances(c, s.testRepo, []instanceNameAndYaml{
		{Name: "snap-a", Yaml: `
name: snap-a
version: 0
plugs:
    name-a: interface
`},
		{Name: "snap-b", Yaml: `
name: snap-b
version: 0
plugs:
    name-a: interface
    name-b: interface
    name-c: interface
`},
		{Name: "snap-b_instance", Yaml: `
name: snap-b
version: 0
plugs:
    name-a: interface
    name-b: interface
    name-c: interface
`},
	})
	c.Assert(snaps, HasLen, 3)
	// The result is sorted by snap and name
	c.Assert(s.testRepo.AllPlugs(""), DeepEquals, []*snap.PlugInfo{
		snaps[0].Info().Plugs["name-a"],
		snaps[1].Info().Plugs["name-a"],
		snaps[1].Info().Plugs["name-b"],
		snaps[1].Info().Plugs["name-c"],
		snaps[2].Info().Plugs["name-a"],
		snaps[2].Info().Plugs["name-b"],
		snaps[2].Info().Plugs["name-c"],
	})
}

func (s *RepositorySuite) TestAllPlugsWithInterfaceName(c *C) {
	// Add another interface so that we can look for it
	err := s.testRepo.AddInterface(&ifacetest.TestInterface{InterfaceName: "other-interface"})
	c.Assert(err, IsNil)
	snaps := addPlugsSlotsFromInstances(c, s.testRepo, []instanceNameAndYaml{
		{Name: "snap-a", Yaml: `
name: snap-a
version: 0
plugs:
    name-a: interface
`},
		{Name: "snap-b", Yaml: `
name: snap-b
version: 0
plugs:
    name-a: interface
    name-b: other-interface
    name-c: interface
`},
		{Name: "snap-b_instance", Yaml: `
name: snap-b
version: 0
plugs:
    name-a: interface
    name-b: other-interface
    name-c: interface
`},
	})
	c.Assert(snaps, HasLen, 3)
	c.Assert(s.testRepo.AllPlugs("other-interface"), DeepEquals, []*snap.PlugInfo{
		snaps[1].Info().Plugs["name-b"],
		snaps[2].Info().Plugs["name-b"],
	})
}

// Tests for Repository.Plugs()

func (s *RepositorySuite) TestPlugs(c *C) {
	snaps := addPlugsSlotsFromInstances(c, s.testRepo, []instanceNameAndYaml{
		{Name: "snap-a", Yaml: `
name: snap-a
version: 0
plugs:
    name-a: interface
`},
		{Name: "snap-b", Yaml: `
name: snap-b
version: 0
plugs:
    name-a: interface
    name-b: interface
    name-c: interface
`},
		{Name: "snap-b_instance", Yaml: `
name: snap-b
version: 0
plugs:
    name-a: interface
    name-b: interface
    name-c: interface
`},
	})
	c.Assert(snaps, HasLen, 3)
	// The result is sorted by snap and name
	c.Assert(s.testRepo.Plugs("snap-b"), DeepEquals, []*snap.PlugInfo{
		snaps[1].Info().Plugs["name-a"],
		snaps[1].Info().Plugs["name-b"],
		snaps[1].Info().Plugs["name-c"],
	})
	c.Assert(s.testRepo.Plugs("snap-b_instance"), DeepEquals, []*snap.PlugInfo{
		snaps[2].Info().Plugs["name-a"],
		snaps[2].Info().Plugs["name-b"],
		snaps[2].Info().Plugs["name-c"],
	})
	// The result is empty if the snap is not known
	c.Assert(s.testRepo.Plugs("snap-x"), HasLen, 0)
	c.Assert(s.testRepo.Plugs("snap-b_other"), HasLen, 0)
}

// Tests for Repository.AllSlots()

func (s *RepositorySuite) TestAllSlots(c *C) {
	err := s.testRepo.AddInterface(&ifacetest.TestInterface{InterfaceName: "other-interface"})
	c.Assert(err, IsNil)
	snaps := addPlugsSlotsFromInstances(c, s.testRepo, []instanceNameAndYaml{
		{Name: "snap-a", Yaml: `
name: snap-a
version: 0
slots:
    name-a: interface
    name-b: interface
`},
		{Name: "snap-b", Yaml: `
name: snap-b
version: 0
slots:
    name-a: other-interface
`},
		{Name: "snap-b_instance", Yaml: `
name: snap-b
version: 0
slots:
    name-a: other-interface
`},
	})
	c.Assert(snaps, HasLen, 3)
	// AllSlots("") returns all slots, sorted by snap and slot name
	c.Assert(s.testRepo.AllSlots(""), DeepEquals, []*snap.SlotInfo{
		snaps[0].Info().Slots["name-a"],
		snaps[0].Info().Slots["name-b"],
		snaps[1].Info().Slots["name-a"],
		snaps[2].Info().Slots["name-a"],
	})
	// AllSlots("") returns all slots, sorted by snap and slot name
	c.Assert(s.testRepo.AllSlots("other-interface"), DeepEquals, []*snap.SlotInfo{
		snaps[1].Info().Slots["name-a"],
		snaps[2].Info().Slots["name-a"],
	})
}

// Tests for Repository.Slots()

func (s *RepositorySuite) TestSlots(c *C) {
	snaps := addPlugsSlotsFromInstances(c, s.testRepo, []instanceNameAndYaml{
		{Name: "snap-a", Yaml: `
name: snap-a
version: 0
slots:
    name-a: interface
    name-b: interface
`},
		{Name: "snap-b", Yaml: `
name: snap-b
version: 0
slots:
    name-a: interface
`},
		{Name: "snap-b_instance", Yaml: `
name: snap-b
version: 0
slots:
    name-a: interface
`},
	})
	// Slots("snap-a") returns slots present in that snap
	c.Assert(s.testRepo.Slots("snap-a"), DeepEquals, []*snap.SlotInfo{
		snaps[0].Info().Slots["name-a"],
		snaps[0].Info().Slots["name-b"],
	})
	// Slots("snap-b") returns slots present in that snap
	c.Assert(s.testRepo.Slots("snap-b"), DeepEquals, []*snap.SlotInfo{
		snaps[1].Info().Slots["name-a"],
	})
	// Slots("snap-b_instance") returns slots present in that snap
	c.Assert(s.testRepo.Slots("snap-b_instance"), DeepEquals, []*snap.SlotInfo{
		snaps[2].Info().Slots["name-a"],
	})
	// Slots("snap-c") returns no slots (because that snap doesn't exist)
	c.Assert(s.testRepo.Slots("snap-c"), HasLen, 0)
	// Slots("snap-b_other") returns no slots (the snap does not exist)
	c.Assert(s.testRepo.Slots("snap-b_other"), HasLen, 0)
	// Slots("") returns no slots
	c.Assert(s.testRepo.Slots(""), HasLen, 0)
}

// Tests for Repository.Slot()

func (s *RepositorySuite) TestSlotSucceedsWhenSlotExists(c *C) {
	err := s.testRepo.AddAppSet(s.producer)
	c.Assert(err, IsNil)
	slot := s.testRepo.Slot(s.producerSlot.Snap.InstanceName(), s.producerSlot.Name)
	c.Assert(slot, DeepEquals, s.producerSlot)
}

func (s *RepositorySuite) TestSlotFailsWhenSlotDoesntExist(c *C) {
	slot := s.testRepo.Slot(s.producerSlot.Snap.InstanceName(), s.producerSlot.Name)
	c.Assert(slot, IsNil)
}

// Tests for Repository.RemoveSlot()

func (s *RepositorySuite) TestRemoveSlotSuccedsWhenSlotExistsAndDisconnected(c *C) {
	err := s.testRepo.AddAppSet(s.producer)
	c.Assert(err, IsNil)
	// Removing a vacant slot simply works
	err = s.testRepo.RemoveSlot(s.producerSlot.Snap.InstanceName(), s.producerSlot.Name)
	c.Assert(err, IsNil)
	// The slot is gone now
	slot := s.testRepo.Slot(s.producerSlot.Snap.InstanceName(), s.producerSlot.Name)
	c.Assert(slot, IsNil)
}

func (s *RepositorySuite) TestRemoveSlotFailsWhenSlotDoesntExist(c *C) {
	// Removing a slot that doesn't exist returns an appropriate error
	err := s.testRepo.RemoveSlot(s.producerSlot.Snap.InstanceName(), s.producerSlot.Name)
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, `cannot remove slot "slot" from snap "producer", no such slot`)
}

func (s *RepositorySuite) TestRemoveSlotFailsWhenSlotIsConnected(c *C) {
	err := s.testRepo.AddAppSet(s.consumer)
	c.Assert(err, IsNil)
	err = s.testRepo.AddAppSet(s.producer)
	c.Assert(err, IsNil)
	connRef := NewConnRef(s.consumerPlug, s.producerSlot)
	_, err = s.testRepo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	// Removing a slot occupied by a plug returns an appropriate error
	err = s.testRepo.RemoveSlot(s.producerSlot.Snap.InstanceName(), s.producerSlot.Name)
	c.Assert(err, ErrorMatches, `cannot remove slot "slot" from snap "producer", it is still connected`)
	// The slot is still there
	slot := s.testRepo.Slot(s.producerSlot.Snap.InstanceName(), s.producerSlot.Name)
	c.Assert(slot, Not(IsNil))
}

// Tests for Repository.ResolveConnect()

func (s *RepositorySuite) TestResolveConnectExplicit(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.producer), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "producer", "slot")
	c.Check(err, IsNil)
	c.Check(conn, DeepEquals, &ConnRef{
		PlugRef: PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: SlotRef{Snap: "producer", Name: "slot"},
	})
}

// ResolveConnect uses the "snapd" snap when slot snap name is empty
func (s *RepositorySuite) TestResolveConnectImplicitSnapdSlot(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.snapdSnapAppSet), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "", "slot")
	c.Check(err, IsNil)
	c.Check(conn, DeepEquals, &ConnRef{
		PlugRef: PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: SlotRef{Snap: "snapd", Name: "slot"},
	})
}

// ResolveConnect uses the "core" snap when slot snap name is empty
func (s *RepositorySuite) TestResolveConnectImplicitCoreSlot(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.coreSnapAppSet), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "", "slot")
	c.Check(err, IsNil)
	c.Check(conn, DeepEquals, &ConnRef{
		PlugRef: PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: SlotRef{Snap: "core", Name: "slot"},
	})
}

// ResolveConnect uses the "ubuntu-core" snap when slot snap name is empty
func (s *RepositorySuite) TestResolveConnectImplicitUbuntuCoreSlot(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.ubuntuCoreSnapAppSet), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "", "slot")
	c.Check(err, IsNil)
	c.Check(conn, DeepEquals, &ConnRef{
		PlugRef: PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: SlotRef{Snap: "ubuntu-core", Name: "slot"},
	})
}

// ResolveConnect prefers the "snapd" snap if "snapd" and "core" are available
func (s *RepositorySuite) TestResolveConnectImplicitSlotPrefersSnapdOverCore(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.snapdSnapAppSet), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.coreSnapAppSet), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "", "slot")
	c.Check(err, IsNil)
	c.Check(conn.SlotRef.Snap, Equals, "snapd")
}

// ResolveConnect prefers the "core" snap if "core" and "ubuntu-core" are available
func (s *RepositorySuite) TestResolveConnectImplicitSlotPrefersCoreOverUbuntuCore(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.coreSnapAppSet), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.ubuntuCoreSnapAppSet), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "", "slot")
	c.Check(err, IsNil)
	c.Check(conn.SlotRef.Snap, Equals, "core")
}

// ResolveConnect detects lack of candidates
func (s *RepositorySuite) TestResolveConnectNoImplicitCandidates(c *C) {
	err := s.testRepo.AddInterface(&ifacetest.TestInterface{InterfaceName: "other-interface"})
	c.Assert(err, IsNil)
	// Tweak the "slot" slot so that it has an incompatible interface type.
	s.coreSnap.Slots["slot"].Interface = "other-interface"
	c.Assert(s.testRepo.AddAppSet(s.coreSnapAppSet), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "", "")
	c.Check(err, ErrorMatches, `snap "core" has no "interface" interface slots`)
	c.Check(conn, IsNil)
}

// ResolveConnect detects ambiguities when slot snap name is empty
func (s *RepositorySuite) TestResolveConnectAmbiguity(c *C) {
	coreSnapAppSet := ifacetest.MockInfoAndAppSet(c, `
name: core
version: 0
type: os
slots:
    slot-a:
        interface: interface
    slot-b:
        interface: interface
`, nil, nil)
	c.Assert(s.testRepo.AddAppSet(coreSnapAppSet), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.producer), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "", "")
	c.Check(err, ErrorMatches, `snap "core" has multiple "interface" interface slots: slot-a, slot-b`)
	c.Check(conn, IsNil)
}

// Pug snap name cannot be empty
func (s *RepositorySuite) TestResolveConnectEmptyPlugSnapName(c *C) {
	conn, err := s.testRepo.ResolveConnect("", "plug", "producer", "slot")
	c.Check(err, ErrorMatches, "cannot resolve connection, plug snap name is empty")
	c.Check(conn, IsNil)
}

// Plug name cannot be empty
func (s *RepositorySuite) TestResolveConnectEmptyPlugName(c *C) {
	conn, err := s.testRepo.ResolveConnect("consumer", "", "producer", "slot")
	c.Check(err, ErrorMatches, "cannot resolve connection, plug name is empty")
	c.Check(conn, IsNil)
}

// Plug must exist
func (s *RepositorySuite) TestResolveNoSuchPlug(c *C) {
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "consumer", "slot")
	c.Check(err, ErrorMatches, `snap "consumer" has no plug named "plug"`)
	e, _ := err.(*NoPlugOrSlotError)
	c.Check(e, NotNil)
	c.Check(conn, IsNil)
}

// Slot snap name cannot be empty if there's no core snap around
func (s *RepositorySuite) TestResolveConnectEmptySlotSnapName(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "", "slot")
	c.Check(err, ErrorMatches, "cannot resolve connection, slot snap name is empty")
	c.Check(conn, IsNil)
}

// Slot name cannot be empty if there's no core snap around
func (s *RepositorySuite) TestResolveConnectEmptySlotName(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "producer", "")
	c.Check(err, ErrorMatches, `snap "producer" has no "interface" interface slots`)
	c.Check(conn, IsNil)
}

// Slot must exists
func (s *RepositorySuite) TestResolveNoSuchSlot(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "producer", "slot")
	c.Check(err, ErrorMatches, `snap "producer" has no slot named "slot"`)
	e, _ := err.(*NoPlugOrSlotError)
	c.Check(e, NotNil)
	c.Check(conn, IsNil)
}

// Plug and slot must have matching types
func (s *RepositorySuite) TestResolveIncompatibleTypes(c *C) {
	c.Assert(s.testRepo.AddInterface(&ifacetest.TestInterface{InterfaceName: "other-interface"}), IsNil)
	set := buildAppSetWithPlugsAndSlots(c, "consumer", []*snap.PlugInfo{
		{Name: "plug", Interface: "other-interface"},
	}, nil)
	c.Assert(s.testRepo.AddAppSet(set), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.producer), IsNil)
	// Connecting a plug to an incompatible slot fails with an appropriate error
	conn, err := s.testRepo.ResolveConnect("consumer", "plug", "producer", "slot")
	c.Check(err, ErrorMatches,
		`cannot connect consumer:plug \("other-interface" interface\) to producer:slot \("interface" interface\)`)
	c.Check(conn, IsNil)
}

// Tests for Repository.Connect()

func (s *RepositorySuite) TestConnectFailsWhenPlugDoesNotExist(c *C) {
	err := s.testRepo.AddAppSet(s.producer)
	c.Assert(err, IsNil)
	// Connecting an unknown plug returns an appropriate error
	connRef := NewConnRef(s.consumerPlug, s.producerSlot)
	_, err = s.testRepo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, ErrorMatches, `cannot connect plug "plug" from snap "consumer": no such plug`)
	e, _ := err.(*NoPlugOrSlotError)
	c.Check(e, NotNil)
}

func (s *RepositorySuite) TestConnectFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddAppSet(s.consumer)
	c.Assert(err, IsNil)
	// Connecting to an unknown slot returns an error
	connRef := NewConnRef(s.consumerPlug, s.producerSlot)
	_, err = s.testRepo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, ErrorMatches, `cannot connect slot "slot" from snap "producer": no such slot`)
	e, _ := err.(*NoPlugOrSlotError)
	c.Check(e, NotNil)
}

func (s *RepositorySuite) TestConnectSucceedsWhenIdenticalConnectExists(c *C) {
	err := s.testRepo.AddAppSet(s.consumer)
	c.Assert(err, IsNil)
	err = s.testRepo.AddAppSet(s.producer)
	c.Assert(err, IsNil)
	connRef := NewConnRef(s.consumerPlug, s.producerSlot)
	conn, err := s.testRepo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(conn, NotNil)
	c.Assert(conn.Plug, NotNil)
	c.Assert(conn.Slot, NotNil)
	c.Assert(conn.Plug.Name(), Equals, "plug")
	c.Assert(conn.Slot.Name(), Equals, "slot")
	// Connecting exactly the same thing twice succeeds without an error but does nothing.
	_, err = s.testRepo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	// Only one connection is actually present.
	c.Assert(s.testRepo.Interfaces(), DeepEquals, &Interfaces{
		Plugs:       []*snap.PlugInfo{s.consumerPlug, s.producerSelfPlug},
		Slots:       []*snap.SlotInfo{s.producerSlot},
		Connections: []*ConnRef{NewConnRef(s.consumerPlug, s.producerSlot)},
	})
}

func (s *RepositorySuite) TestConnectFailsWhenSlotAndPlugAreIncompatible(c *C) {
	otherInterface := &ifacetest.TestInterface{InterfaceName: "other-interface"}
	err := s.testRepo.AddInterface(otherInterface)
	c.Assert(err, IsNil)

	set := buildAppSetWithPlugsAndSlots(c, "consumer", []*snap.PlugInfo{
		{Name: "plug", Interface: "other-interface"},
	}, nil)
	err = s.testRepo.AddAppSet(set)
	c.Assert(err, IsNil)

	err = s.testRepo.AddAppSet(s.producer)
	c.Assert(err, IsNil)

	// Connecting a plug to an incompatible slot fails with an appropriate error
	connRef := NewConnRef(s.consumerPlug, s.producerSlot)
	_, err = s.testRepo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, ErrorMatches, `cannot connect plug "consumer:plug" \(interface "other-interface"\) to "producer:slot" \(interface "interface"\)`)
}

func (s *RepositorySuite) TestConnectSucceeds(c *C) {
	err := s.testRepo.AddAppSet(s.consumer)
	c.Assert(err, IsNil)
	err = s.testRepo.AddAppSet(s.producer)
	c.Assert(err, IsNil)
	// Connecting a plug works okay
	connRef := NewConnRef(s.consumerPlug, s.producerSlot)
	_, err = s.testRepo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
}

// Tests for Repository.Disconnect() and DisconnectAll()

// Disconnect fails if any argument is empty
func (s *RepositorySuite) TestDisconnectFailsOnEmptyArgs(c *C) {
	err1 := s.testRepo.Disconnect(s.consumerPlug.Snap.InstanceName(), s.consumerPlug.Name, s.producerSlot.Snap.InstanceName(), "")
	err2 := s.testRepo.Disconnect(s.consumerPlug.Snap.InstanceName(), s.consumerPlug.Name, "", s.producerSlot.Name)
	err3 := s.testRepo.Disconnect(s.consumerPlug.Snap.InstanceName(), "", s.producerSlot.Snap.InstanceName(), s.producerSlot.Name)
	err4 := s.testRepo.Disconnect("", s.consumerPlug.Name, s.producerSlot.Snap.InstanceName(), s.producerSlot.Name)
	c.Assert(err1, ErrorMatches, `cannot disconnect, slot name is empty`)
	c.Assert(err2, ErrorMatches, `cannot disconnect, slot snap name is empty`)
	c.Assert(err3, ErrorMatches, `cannot disconnect, plug name is empty`)
	c.Assert(err4, ErrorMatches, `cannot disconnect, plug snap name is empty`)
}

// Disconnect fails if plug doesn't exist
func (s *RepositorySuite) TestDisconnectFailsWithoutPlug(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.producer), IsNil)
	err := s.testRepo.Disconnect(s.consumerPlug.Snap.InstanceName(), s.consumerPlug.Name, s.producerSlot.Snap.InstanceName(), s.producerSlot.Name)
	c.Assert(err, ErrorMatches, `snap "consumer" has no plug named "plug"`)
	e, _ := err.(*NoPlugOrSlotError)
	c.Check(e, NotNil)
}

// Disconnect fails if slot doesn't exist
func (s *RepositorySuite) TestDisconnectFailsWithutSlot(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	err := s.testRepo.Disconnect(s.consumerPlug.Snap.InstanceName(), s.consumerPlug.Name, s.producerSlot.Snap.InstanceName(), s.producerSlot.Name)
	c.Assert(err, ErrorMatches, `snap "producer" has no slot named "slot"`)
	e, _ := err.(*NoPlugOrSlotError)
	c.Check(e, NotNil)
}

// Disconnect fails if there's no connection to disconnect
func (s *RepositorySuite) TestDisconnectFailsWhenNotConnected(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.producer), IsNil)
	err := s.testRepo.Disconnect(s.consumerPlug.Snap.InstanceName(), s.consumerPlug.Name, s.producerSlot.Snap.InstanceName(), s.producerSlot.Name)
	c.Assert(err, ErrorMatches, `cannot disconnect consumer:plug from producer:slot, it is not connected`)
	e, _ := err.(*NotConnectedError)
	c.Check(e, NotNil)
}

// Disconnect works when plug and slot exist and are connected
func (s *RepositorySuite) TestDisconnectSucceeds(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.producer), IsNil)
	_, err := s.testRepo.Connect(NewConnRef(s.consumerPlug, s.producerSlot), nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	_, err = s.testRepo.Connect(NewConnRef(s.consumerPlug, s.producerSlot), nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	err = s.testRepo.Disconnect(s.consumerPlug.Snap.InstanceName(), s.consumerPlug.Name, s.producerSlot.Snap.InstanceName(), s.producerSlot.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.Interfaces(), DeepEquals, &Interfaces{
		Plugs: []*snap.PlugInfo{s.consumerPlug, s.producerSelfPlug},
		Slots: []*snap.SlotInfo{s.producerSlot},
	})
}

// Tests for Repository.Connected

// Connected fails if snap name is empty and there's no core snap around
func (s *RepositorySuite) TestConnectedFailsWithEmptySnapName(c *C) {
	_, err := s.testRepo.Connected("", s.consumerPlug.Name)
	c.Check(err, ErrorMatches, "internal error: cannot obtain core snap name while computing connections")
}

// Connected fails if plug or slot name is empty
func (s *RepositorySuite) TestConnectedFailsWithEmptyPlugSlotName(c *C) {
	_, err := s.testRepo.Connected(s.consumerPlug.Snap.InstanceName(), "")
	c.Check(err, ErrorMatches, "plug or slot name is empty")
}

// Connected fails if plug or slot doesn't exist
func (s *RepositorySuite) TestConnectedFailsWithoutPlugOrSlot(c *C) {
	_, err1 := s.testRepo.Connected(s.consumerPlug.Snap.InstanceName(), s.consumerPlug.Name)
	_, err2 := s.testRepo.Connected(s.producerSlot.Snap.InstanceName(), s.producerSlot.Name)
	c.Check(err1, ErrorMatches, `snap "consumer" has no plug or slot named "plug"`)
	e, _ := err1.(*NoPlugOrSlotError)
	c.Check(e, NotNil)
	c.Check(err2, ErrorMatches, `snap "producer" has no plug or slot named "slot"`)
	e, _ = err1.(*NoPlugOrSlotError)
	c.Check(e, NotNil)
}

// Connected finds connections when asked from plug or from slot side
func (s *RepositorySuite) TestConnectedFindsConnections(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.producer), IsNil)
	_, err := s.testRepo.Connect(NewConnRef(s.consumerPlug, s.producerSlot), nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	conns, err := s.testRepo.Connected(s.consumerPlug.Snap.InstanceName(), s.consumerPlug.Name)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, []*ConnRef{NewConnRef(s.consumerPlug, s.producerSlot)})

	conns, err = s.testRepo.Connected(s.producerSlot.Snap.InstanceName(), s.producerSlot.Name)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, []*ConnRef{NewConnRef(s.consumerPlug, s.producerSlot)})
}

// Connected uses the core snap if snap name is empty
func (s *RepositorySuite) TestConnectedFindsCoreSnap(c *C) {
	core := &snap.Info{
		SuggestedName: "core",
		SnapType:      snap.TypeOS,
		Slots:         make(map[string]*snap.SlotInfo),
		Version:       "1",
	}
	core.Slots["slot"] = &snap.SlotInfo{
		Snap:      core,
		Name:      "slot",
		Interface: "interface",
	}

	coreAppSet, err := NewSnapAppSet(core, nil)
	c.Assert(err, IsNil)

	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	c.Assert(s.testRepo.AddAppSet(coreAppSet), IsNil)

	slot := core.Slots["slot"]

	_, err = s.testRepo.Connect(NewConnRef(s.consumerPlug, slot), nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	conns, err := s.testRepo.Connected("", slot.Name)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, []*ConnRef{NewConnRef(s.consumerPlug, slot)})
}

// Connected finds connections when asked from plug or from slot side
func (s *RepositorySuite) TestConnections(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.producer), IsNil)
	_, err := s.testRepo.Connect(NewConnRef(s.consumerPlug, s.producerSlot), nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	conns, err := s.testRepo.Connections(s.consumerPlug.Snap.InstanceName())
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, []*ConnRef{NewConnRef(s.consumerPlug, s.producerSlot)})

	conns, err = s.testRepo.Connections(s.producerSlot.Snap.InstanceName())
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, []*ConnRef{NewConnRef(s.consumerPlug, s.producerSlot)})

	conns, err = s.testRepo.Connections("abc")
	c.Assert(err, IsNil)
	c.Assert(conns, HasLen, 0)
}

func (s *RepositorySuite) TestConnectionsWithSelfConnected(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.producer), IsNil)
	_, err := s.testRepo.Connect(NewConnRef(s.producerSelfPlug, s.producerSlot), nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	conns, err := s.testRepo.Connections(s.producerSelfPlug.Snap.InstanceName())
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, []*ConnRef{NewConnRef(s.producerSelfPlug, s.producerSlot)})

	conns, err = s.testRepo.Connections(s.producerSlot.Snap.InstanceName())
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, []*ConnRef{NewConnRef(s.producerSelfPlug, s.producerSlot)})
}

// Tests for Repository.DisconnectAll()

func (s *RepositorySuite) TestDisconnectAll(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.producer), IsNil)
	_, err := s.testRepo.Connect(NewConnRef(s.consumerPlug, s.producerSlot), nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	conns := []*ConnRef{NewConnRef(s.consumerPlug, s.producerSlot)}
	s.testRepo.DisconnectAll(conns)
	c.Assert(s.testRepo.Interfaces(), DeepEquals, &Interfaces{
		Plugs: []*snap.PlugInfo{s.consumerPlug, s.producerSelfPlug},
		Slots: []*snap.SlotInfo{s.producerSlot},
	})
}

// Tests for Repository.Interfaces()

func (s *RepositorySuite) TestInterfacesSmokeTest(c *C) {
	err := s.testRepo.AddAppSet(s.consumer)
	c.Assert(err, IsNil)
	err = s.testRepo.AddAppSet(s.producer)
	c.Assert(err, IsNil)
	// After connecting the result is as expected
	connRef := NewConnRef(s.consumerPlug, s.producerSlot)
	_, err = s.testRepo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	ifaces := s.testRepo.Interfaces()
	c.Assert(ifaces, DeepEquals, &Interfaces{
		Plugs:       []*snap.PlugInfo{s.consumerPlug, s.producerSelfPlug},
		Slots:       []*snap.SlotInfo{s.producerSlot},
		Connections: []*ConnRef{NewConnRef(s.consumerPlug, s.producerSlot)},
	})
	// After disconnecting the connections become empty
	err = s.testRepo.Disconnect(s.consumerPlug.Snap.InstanceName(), s.consumerPlug.Name, s.producerSlot.Snap.InstanceName(), s.producerSlot.Name)
	c.Assert(err, IsNil)
	ifaces = s.testRepo.Interfaces()
	c.Assert(ifaces, DeepEquals, &Interfaces{
		Plugs: []*snap.PlugInfo{s.consumerPlug, s.producerSelfPlug},
		Slots: []*snap.SlotInfo{s.producerSlot},
	})
}

// Tests for Repository.SnapSpecification

const testSecurity SecuritySystem = "test"

var testInterface = &ifacetest.TestInterface{
	InterfaceName: "interface",
	TestPermanentPlugCallback: func(spec *ifacetest.Specification, plug *snap.PlugInfo) error {
		spec.AddSnippet("static plug snippet")
		return nil
	},
	TestConnectedPlugCallback: func(spec *ifacetest.Specification, plug *ConnectedPlug, slot *ConnectedSlot) error {
		spec.AddSnippet("connection-specific plug snippet")
		return nil
	},
	TestPermanentSlotCallback: func(spec *ifacetest.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("static slot snippet")
		return nil
	},
	TestConnectedSlotCallback: func(spec *ifacetest.Specification, plug *ConnectedPlug, slot *ConnectedSlot) error {
		spec.AddSnippet("connection-specific slot snippet")
		return nil
	},
}

func (s *RepositorySuite) TestSnapSpecification(c *C) {
	repo := s.emptyRepo
	backend := &ifacetest.TestSecurityBackend{BackendName: testSecurity}
	c.Assert(repo.AddBackend(backend), IsNil)
	c.Assert(repo.AddInterface(testInterface), IsNil)
	c.Assert(repo.AddAppSet(s.consumer), IsNil)
	c.Assert(repo.AddAppSet(s.producer), IsNil)

	// Snaps should get static security now
	spec, err := repo.SnapSpecification(testSecurity, s.consumer)
	c.Assert(err, IsNil)
	c.Check(spec.(*ifacetest.Specification).Snippets, DeepEquals, []string{"static plug snippet"})

	spec, err = repo.SnapSpecification(testSecurity, s.producer)
	c.Assert(err, IsNil)
	c.Check(spec.(*ifacetest.Specification).Snippets, DeepEquals, []string{"static slot snippet", "static plug snippet"})

	// Establish connection between plug and slot
	connRef := NewConnRef(s.consumerPlug, s.producerSlot)
	_, err = repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	// Snaps should get static and connection-specific security now
	spec, err = repo.SnapSpecification(testSecurity, s.consumer)
	c.Assert(err, IsNil)
	c.Check(spec.(*ifacetest.Specification).Snippets, DeepEquals, []string{
		"static plug snippet",
		"connection-specific plug snippet",
	})

	spec, err = repo.SnapSpecification(testSecurity, s.producer)
	c.Assert(err, IsNil)
	c.Check(spec.(*ifacetest.Specification).Snippets, DeepEquals, []string{
		"static slot snippet",
		"connection-specific slot snippet",
		"static plug snippet",
	})
}

func (s *RepositorySuite) TestSnapSpecificationFailureWithConnectionSnippets(c *C) {
	var testSecurity SecuritySystem = "security"
	backend := &ifacetest.TestSecurityBackend{BackendName: testSecurity}
	iface := &ifacetest.TestInterface{
		InterfaceName: "interface",
		TestConnectedSlotCallback: func(spec *ifacetest.Specification, plug *ConnectedPlug, slot *ConnectedSlot) error {
			return fmt.Errorf("cannot compute snippet for provider")
		},
		TestConnectedPlugCallback: func(spec *ifacetest.Specification, plug *ConnectedPlug, slot *ConnectedSlot) error {
			return fmt.Errorf("cannot compute snippet for consumer")
		},
	}
	repo := s.emptyRepo

	c.Assert(repo.AddBackend(backend), IsNil)
	c.Assert(repo.AddInterface(iface), IsNil)
	c.Assert(repo.AddAppSet(s.consumer), IsNil)
	c.Assert(repo.AddAppSet(s.producer), IsNil)
	connRef := NewConnRef(s.consumerPlug, s.producerSlot)
	_, err := repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	plugAppSet, err := NewSnapAppSet(s.consumerPlug.Snap, nil)
	c.Assert(err, IsNil)

	spec, err := repo.SnapSpecification(testSecurity, plugAppSet)
	c.Assert(err, ErrorMatches, "cannot compute snippet for consumer")
	c.Assert(spec, IsNil)

	slotAppSet, err := NewSnapAppSet(s.producerSlot.Snap, nil)
	c.Assert(err, IsNil)

	spec, err = repo.SnapSpecification(testSecurity, slotAppSet)
	c.Assert(err, ErrorMatches, "cannot compute snippet for provider")
	c.Assert(spec, IsNil)
}

func (s *RepositorySuite) TestSnapSpecificationFailureWithPermanentSnippets(c *C) {
	var testSecurity SecuritySystem = "security"
	iface := &ifacetest.TestInterface{
		InterfaceName: "interface",
		TestPermanentSlotCallback: func(spec *ifacetest.Specification, slot *snap.SlotInfo) error {
			return fmt.Errorf("cannot compute snippet for provider")
		},
		TestPermanentPlugCallback: func(spec *ifacetest.Specification, plug *snap.PlugInfo) error {
			return fmt.Errorf("cannot compute snippet for consumer")
		},
	}
	backend := &ifacetest.TestSecurityBackend{BackendName: testSecurity}
	repo := s.emptyRepo
	c.Assert(repo.AddBackend(backend), IsNil)
	c.Assert(repo.AddInterface(iface), IsNil)
	c.Assert(repo.AddAppSet(s.consumer), IsNil)
	c.Assert(repo.AddAppSet(s.producer), IsNil)
	connRef := NewConnRef(s.consumerPlug, s.producerSlot)
	_, err := repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	appSet, err := NewSnapAppSet(s.consumerPlug.Snap, nil)
	c.Assert(err, IsNil)

	spec, err := repo.SnapSpecification(testSecurity, appSet)
	c.Assert(err, ErrorMatches, "cannot compute snippet for consumer")
	c.Assert(spec, IsNil)

	appSet, err = NewSnapAppSet(s.producerSlot.Snap, nil)
	c.Assert(err, IsNil)

	spec, err = repo.SnapSpecification(testSecurity, appSet)
	c.Assert(err, ErrorMatches, "cannot compute snippet for provider")
	c.Assert(spec, IsNil)
}

type testSideArity struct {
	sideSnapName string
}

func (a *testSideArity) SlotsPerPlugAny() bool {
	return strings.HasSuffix(a.sideSnapName, "2")
}

func (s *RepositorySuite) TestAutoConnectCandidatePlugsAndSlots(c *C) {
	// Add two interfaces, one with automatic connections, one with manual
	repo := s.emptyRepo
	err := repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "auto"})
	c.Assert(err, IsNil)
	err = repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "manual"})
	c.Assert(err, IsNil)

	policyCheck := func(plug *ConnectedPlug, slot *ConnectedSlot) (bool, SideArity, error) {
		return slot.Interface() == "auto", &testSideArity{plug.Snap().InstanceName()}, nil
	}

	// Add a pair of snaps with plugs/slots using those two interfaces
	consumer := ifacetest.MockInfoAndAppSet(c, `
name: consumer
version: 0
plugs:
    auto:
    manual:
`, nil, nil)
	producer := ifacetest.MockInfoAndAppSet(c, `
name: producer
version: 0
type: os
slots:
    auto:
    manual:
`, nil, nil)
	err = repo.AddAppSet(producer)
	c.Assert(err, IsNil)
	err = repo.AddAppSet(consumer)
	c.Assert(err, IsNil)

	candidateSlots, arities := repo.AutoConnectCandidateSlots("consumer", "auto", policyCheck)
	c.Assert(candidateSlots, HasLen, 1)
	c.Check(candidateSlots[0].Snap.InstanceName(), Equals, "producer")
	c.Check(candidateSlots[0].Interface, Equals, "auto")
	c.Check(candidateSlots[0].Name, Equals, "auto")
	c.Assert(arities, HasLen, 1)
	c.Check(arities[0].SlotsPerPlugAny(), Equals, false)

	candidatePlugs := repo.AutoConnectCandidatePlugs("producer", "auto", policyCheck)
	c.Assert(candidatePlugs, HasLen, 1)
	c.Check(candidatePlugs[0].Snap.InstanceName(), Equals, "consumer")
	c.Check(candidatePlugs[0].Interface, Equals, "auto")
	c.Check(candidatePlugs[0].Name, Equals, "auto")
}

func (s *RepositorySuite) TestAutoConnectCandidatePlugsAndSlotsSymmetry(c *C) {
	repo := s.emptyRepo
	// Add a "auto" interface
	err := repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "auto"})
	c.Assert(err, IsNil)

	policyCheck := func(plug *ConnectedPlug, slot *ConnectedSlot) (bool, SideArity, error) {
		return slot.Interface() == "auto", &testSideArity{plug.Snap().InstanceName()}, nil
	}

	// Add a producer snap for "auto"
	producer := ifacetest.MockInfoAndAppSet(c, `
name: producer
version: 0
type: os
slots:
    auto:
`, nil, nil)
	err = repo.AddAppSet(producer)
	c.Assert(err, IsNil)

	// Add two consumers snaps for "auto"
	consumer1 := ifacetest.MockInfoAndAppSet(c, `
name: consumer1
version: 0
plugs:
    auto:
`, nil, nil)

	err = repo.AddAppSet(consumer1)
	c.Assert(err, IsNil)

	// Add two consumers snaps for "auto"
	consumer2 := ifacetest.MockInfoAndAppSet(c, `
name: consumer2
version: 0
plugs:
    auto:
`, nil, nil)

	err = repo.AddAppSet(consumer2)
	c.Assert(err, IsNil)

	// Both can auto-connect
	candidateSlots, arities := repo.AutoConnectCandidateSlots("consumer1", "auto", policyCheck)
	c.Assert(candidateSlots, HasLen, 1)
	c.Check(candidateSlots[0].Snap.InstanceName(), Equals, "producer")
	c.Check(candidateSlots[0].Interface, Equals, "auto")
	c.Check(candidateSlots[0].Name, Equals, "auto")
	c.Assert(arities, HasLen, 1)
	c.Check(arities[0].SlotsPerPlugAny(), Equals, false)

	candidateSlots, arities = repo.AutoConnectCandidateSlots("consumer2", "auto", policyCheck)
	c.Assert(candidateSlots, HasLen, 1)
	c.Check(candidateSlots[0].Snap.InstanceName(), Equals, "producer")
	c.Check(candidateSlots[0].Interface, Equals, "auto")
	c.Check(candidateSlots[0].Name, Equals, "auto")
	c.Assert(arities, HasLen, 1)
	c.Check(arities[0].SlotsPerPlugAny(), Equals, true)

	// Plugs candidates seen from the producer (for example if
	// it's installed after) should be the same
	candidatePlugs := repo.AutoConnectCandidatePlugs("producer", "auto", policyCheck)
	c.Assert(candidatePlugs, HasLen, 2)
}

func (s *RepositorySuite) TestAutoConnectCandidateSlotsSideArity(c *C) {
	repo := s.emptyRepo
	// Add a "auto" interface
	err := repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "auto"})
	c.Assert(err, IsNil)

	policyCheck := func(plug *ConnectedPlug, slot *ConnectedSlot) (bool, SideArity, error) {
		return slot.Interface() == "auto", &testSideArity{slot.Snap().InstanceName()}, nil
	}

	// Add two producer snaps for "auto"
	producer1 := ifacetest.MockInfoAndAppSet(c, `
name: producer1
version: 0
slots:
    auto:
`, nil, nil)
	err = repo.AddAppSet(producer1)
	c.Assert(err, IsNil)

	producer2 := ifacetest.MockInfoAndAppSet(c, `
name: producer2
version: 0
slots:
    auto:
`, nil, nil)
	err = repo.AddAppSet(producer2)
	c.Assert(err, IsNil)

	// Add a consumer snap for "auto"
	consumer := ifacetest.MockInfoAndAppSet(c, `
name: consumer
version: 0
plugs:
    auto:
`, nil, nil)
	err = repo.AddAppSet(consumer)
	c.Assert(err, IsNil)

	// Both slots could auto-connect
	seenProducers := make(map[string]bool)
	candidateSlots, arities := repo.AutoConnectCandidateSlots("consumer", "auto", policyCheck)
	c.Assert(candidateSlots, HasLen, 2)
	c.Assert(arities, HasLen, 2)
	for i, candSlot := range candidateSlots {
		c.Check(candSlot.Interface, Equals, "auto")
		c.Check(candSlot.Name, Equals, "auto")
		producerName := candSlot.Snap.InstanceName()
		// SideArities match
		switch producerName {
		case "producer1":
			c.Check(arities[i].SlotsPerPlugAny(), Equals, false)
		case "producer2":
			c.Check(arities[i].SlotsPerPlugAny(), Equals, true)
		}
		seenProducers[producerName] = true
	}
	c.Check(seenProducers, DeepEquals, map[string]bool{
		"producer1": true,
		"producer2": true,
	})
}

// Tests for AddSnap and RemoveSnap

type AddRemoveSuite struct {
	testutil.BaseTest
	repo *Repository
}

var _ = Suite(&AddRemoveSuite{})

func (s *AddRemoveSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.repo = NewRepository()
	err := s.repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "iface"})
	c.Assert(err, IsNil)
	err = s.repo.AddInterface(&ifacetest.TestInterface{
		InterfaceName:             "invalid",
		BeforePreparePlugCallback: func(plug *snap.PlugInfo) error { return fmt.Errorf("plug is invalid") },
		BeforePrepareSlotCallback: func(slot *snap.SlotInfo) error { return fmt.Errorf("slot is invalid") },
	})
	c.Assert(err, IsNil)
}

func (s *AddRemoveSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

const testConsumerYaml = `
name: consumer
version: 0
apps:
    app:
        plugs: [iface]
`
const testProducerYaml = `
name: producer
version: 0
apps:
    app:
        slots: [iface]
`

func (s *AddRemoveSuite) addSnap(c *C, yaml string) (*snap.Info, error) {
	appSet := ifacetest.MockInfoAndAppSet(c, yaml, nil, nil)
	return appSet.Info(), s.repo.AddAppSet(appSet)
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

func (s *AddRemoveSuite) TestAddSnapSkipsUnknownInterfaces(c *C) {
	info, err := s.addSnap(c, `
name: bogus
version: 0
plugs:
  bogus-plug:
slots:
  bogus-slot:
`)
	c.Assert(err, IsNil)
	// the snap knowns about the bogus plug and slot
	c.Assert(info.Plugs["bogus-plug"], NotNil)
	c.Assert(info.Slots["bogus-slot"], NotNil)
	// but the repository ignores them
	c.Assert(s.repo.Plug("bogus", "bogus-plug"), IsNil)
	c.Assert(s.repo.Slot("bogus", "bogus-slot"), IsNil)
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
	connRef := &ConnRef{PlugRef: PlugRef{Snap: "consumer", Name: "iface"}, SlotRef: SlotRef{Snap: "producer", Name: "iface"}}
	_, err = s.repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	err = s.repo.RemoveSnap("consumer")
	c.Assert(err, ErrorMatches, "cannot remove connected plug consumer.iface")
}

func (s *AddRemoveSuite) TestRemoveSnapErrorsOnStillConnectedSlot(c *C) {
	_, err := s.addSnap(c, testConsumerYaml)
	c.Assert(err, IsNil)
	_, err = s.addSnap(c, testProducerYaml)
	c.Assert(err, IsNil)
	connRef := &ConnRef{PlugRef: PlugRef{Snap: "consumer", Name: "iface"}, SlotRef: SlotRef{Snap: "producer", Name: "iface"}}
	_, err = s.repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	err = s.repo.RemoveSnap("producer")
	c.Assert(err, ErrorMatches, "cannot remove connected slot producer.iface")
}

type DisconnectSnapSuite struct {
	testutil.BaseTest
	repo               *Repository
	s1, s2, s2Instance *SnapAppSet
}

var _ = Suite(&DisconnectSnapSuite{})

func (s *DisconnectSnapSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.repo = NewRepository()

	err := s.repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "iface-a"})
	c.Assert(err, IsNil)
	err = s.repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "iface-b"})
	c.Assert(err, IsNil)

	s.s1 = ifacetest.MockInfoAndAppSet(c, `
name: s1
version: 0
plugs:
    iface-a:
slots:
    iface-b:
`, nil, nil)
	err = s.repo.AddAppSet(s.s1)
	c.Assert(err, IsNil)

	s.s2 = ifacetest.MockInfoAndAppSet(c, `
name: s2
version: 0
plugs:
    iface-b:
slots:
    iface-a:
`, nil, nil)
	c.Assert(err, IsNil)
	err = s.repo.AddAppSet(s.s2)
	c.Assert(err, IsNil)
	s.s2Instance = ifacetest.MockInfoAndAppSet(c, `
name: s2
version: 0
plugs:
    iface-b:
slots:
    iface-a:
`, nil, nil)
	s.s2Instance.Info().InstanceKey = "instance"
	c.Assert(err, IsNil)
	err = s.repo.AddAppSet(s.s2Instance)
	c.Assert(err, IsNil)
}

func (s *DisconnectSnapSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *DisconnectSnapSuite) TestNotConnected(c *C) {
	affected, err := s.repo.DisconnectSnap("s1")
	c.Assert(err, IsNil)
	c.Check(affected, HasLen, 0)
}

func (s *DisconnectSnapSuite) TestOutgoingConnection(c *C) {
	connRef := &ConnRef{PlugRef: PlugRef{Snap: "s1", Name: "iface-a"}, SlotRef: SlotRef{Snap: "s2", Name: "iface-a"}}
	_, err := s.repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	// Disconnect s1 with which has an outgoing connection to s2
	affected, err := s.repo.DisconnectSnap("s1")
	c.Assert(err, IsNil)
	c.Check(affected, testutil.Contains, "s1")
	c.Check(affected, testutil.Contains, "s2")
}

func (s *DisconnectSnapSuite) TestIncomingConnection(c *C) {
	connRef := &ConnRef{PlugRef: PlugRef{Snap: "s2", Name: "iface-b"}, SlotRef: SlotRef{Snap: "s1", Name: "iface-b"}}
	_, err := s.repo.Connect(connRef, nil, nil, nil, nil, nil)
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
		connRef1 := &ConnRef{PlugRef: PlugRef{Snap: "s1", Name: "iface-a"}, SlotRef: SlotRef{Snap: "s2", Name: "iface-a"}}
		_, err := s.repo.Connect(connRef1, nil, nil, nil, nil, nil)
		c.Assert(err, IsNil)
		connRef2 := &ConnRef{PlugRef: PlugRef{Snap: "s2", Name: "iface-b"}, SlotRef: SlotRef{Snap: "s1", Name: "iface-b"}}
		_, err = s.repo.Connect(connRef2, nil, nil, nil, nil, nil)
		c.Assert(err, IsNil)
		affected, err := s.repo.DisconnectSnap(snapName)
		c.Assert(err, IsNil)
		c.Check(affected, testutil.Contains, "s1")
		c.Check(affected, testutil.Contains, "s2")
	}
}

func (s *DisconnectSnapSuite) TestParallelInstances(c *C) {
	_, err := s.repo.Connect(&ConnRef{PlugRef: PlugRef{Snap: "s1", Name: "iface-a"}, SlotRef: SlotRef{Snap: "s2_instance", Name: "iface-a"}}, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	affected, err := s.repo.DisconnectSnap("s1")
	c.Assert(err, IsNil)
	c.Check(affected, testutil.Contains, "s1")
	c.Check(affected, testutil.Contains, "s2_instance")

	_, err = s.repo.Connect(&ConnRef{PlugRef: PlugRef{Snap: "s2_instance", Name: "iface-b"}, SlotRef: SlotRef{Snap: "s1", Name: "iface-b"}}, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	affected, err = s.repo.DisconnectSnap("s1")
	c.Assert(err, IsNil)
	c.Check(affected, testutil.Contains, "s1")
	c.Check(affected, testutil.Contains, "s2_instance")
}

func contentPolicyCheck(plug *ConnectedPlug, slot *ConnectedSlot) (bool, SideArity, error) {
	return plug.Snap().Publisher.ID == slot.Snap().Publisher.ID, nil, nil
}

func contentAutoConnect(plug *snap.PlugInfo, slot *snap.SlotInfo) bool {
	return plug.Attrs["content"] == slot.Attrs["content"]
}

// internal helper that creates a new repository with two snaps, one
// is a content plug and one a content slot
func makeContentConnectionTestSnaps(c *C, plugContentToken, slotContentToken string) (*Repository, *snap.Info, *snap.Info) {
	repo := NewRepository()
	err := repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "content", AutoConnectCallback: contentAutoConnect})
	c.Assert(err, IsNil)

	plugSnap := ifacetest.MockInfoAndAppSet(c, fmt.Sprintf(`
name: content-plug-snap
version: 0
plugs:
  imported-content:
    interface: content
    content: %s
`, plugContentToken), nil, nil)
	slotSnap := ifacetest.MockInfoAndAppSet(c, fmt.Sprintf(`
name: content-slot-snap
version: 0
slots:
  exported-content:
    interface: content
    content: %s
`, slotContentToken), nil, nil)

	err = repo.AddAppSet(plugSnap)
	c.Assert(err, IsNil)
	err = repo.AddAppSet(slotSnap)
	c.Assert(err, IsNil)

	return repo, plugSnap.Info(), slotSnap.Info()
}

func (s *RepositorySuite) TestAutoConnectContentInterfaceSimple(c *C) {
	repo, _, _ := makeContentConnectionTestSnaps(c, "mylib", "mylib")
	candidateSlots, _ := repo.AutoConnectCandidateSlots("content-plug-snap", "imported-content", contentPolicyCheck)
	c.Assert(candidateSlots, HasLen, 1)
	c.Check(candidateSlots[0].Name, Equals, "exported-content")
	candidatePlugs := repo.AutoConnectCandidatePlugs("content-slot-snap", "exported-content", contentPolicyCheck)
	c.Assert(candidatePlugs, HasLen, 1)
	c.Check(candidatePlugs[0].Name, Equals, "imported-content")
}

func (s *RepositorySuite) TestAutoConnectContentInterfaceOSWorksCorrectly(c *C) {
	repo, _, slotSnap := makeContentConnectionTestSnaps(c, "mylib", "otherlib")
	slotSnap.SnapType = snap.TypeOS

	candidateSlots, _ := repo.AutoConnectCandidateSlots("content-plug-snap", "imported-content", contentPolicyCheck)
	c.Check(candidateSlots, HasLen, 0)
	candidatePlugs := repo.AutoConnectCandidatePlugs("content-slot-snap", "exported-content", contentPolicyCheck)
	c.Assert(candidatePlugs, HasLen, 0)
}

func (s *RepositorySuite) TestAutoConnectContentInterfaceNoMatchingContent(c *C) {
	repo, _, _ := makeContentConnectionTestSnaps(c, "mylib", "otherlib")
	candidateSlots, _ := repo.AutoConnectCandidateSlots("content-plug-snap", "imported-content", contentPolicyCheck)
	c.Check(candidateSlots, HasLen, 0)
	candidatePlugs := repo.AutoConnectCandidatePlugs("content-slot-snap", "exported-content", contentPolicyCheck)
	c.Assert(candidatePlugs, HasLen, 0)
}

func (s *RepositorySuite) TestAutoConnectContentInterfaceNoMatchingDeveloper(c *C) {
	repo, plugSnap, slotSnap := makeContentConnectionTestSnaps(c, "mylib", "mylib")
	// real code will use the assertions, this is just for emulation
	plugSnap.Publisher.ID = "fooid"
	slotSnap.Publisher.ID = "barid"

	candidateSlots, _ := repo.AutoConnectCandidateSlots("content-plug-snap", "imported-content", contentPolicyCheck)
	c.Check(candidateSlots, HasLen, 0)
	candidatePlugs := repo.AutoConnectCandidatePlugs("content-slot-snap", "exported-content", contentPolicyCheck)
	c.Assert(candidatePlugs, HasLen, 0)
}

func (s *RepositorySuite) TestInfo(c *C) {
	r := s.emptyRepo

	// Add some test interfaces.
	i1 := &ifacetest.TestInterface{InterfaceName: "i1", InterfaceStaticInfo: StaticInfo{Summary: "i1 summary", DocURL: "http://example.com/i1"}}
	i2 := &ifacetest.TestInterface{InterfaceName: "i2", InterfaceStaticInfo: StaticInfo{Summary: "i2 summary", DocURL: "http://example.com/i2"}}
	i3 := &ifacetest.TestInterface{InterfaceName: "i3", InterfaceStaticInfo: StaticInfo{Summary: "i3 summary", DocURL: "http://example.com/i3"}}
	c.Assert(r.AddInterface(i1), IsNil)
	c.Assert(r.AddInterface(i2), IsNil)
	c.Assert(r.AddInterface(i3), IsNil)

	// Add some test snaps.
	s1 := ifacetest.MockInfoAndAppSet(c, `
name: s1
version: 0
apps:
  s1:
    plugs: [i1, i2]
`, nil, nil)
	c.Assert(r.AddAppSet(s1), IsNil)

	s2 := ifacetest.MockInfoAndAppSet(c, `
name: s2
version: 0
apps:
  s2:
    slots: [i1, i3]
`, nil, nil)
	c.Assert(r.AddAppSet(s2), IsNil)

	s3 := ifacetest.MockInfoAndAppSet(c, `
name: s3
version: 0
type: os
slots:
  i2:
`, nil, nil)
	c.Assert(r.AddAppSet(s3), IsNil)
	s3Instance := ifacetest.MockInfoAndAppSet(c, `
name: s3
version: 0
type: os
slots:
  i2:
`, nil, nil)
	s3Instance.Info().InstanceKey = "instance"
	c.Assert(r.AddAppSet(s3Instance), IsNil)
	s4 := ifacetest.MockInfoAndAppSet(c, `
name: s4
version: 0
apps:
  s1:
    plugs: [i2]
`, nil, nil)
	c.Assert(r.AddAppSet(s4), IsNil)

	// Connect a few things for the tests below.
	_, err := r.Connect(&ConnRef{PlugRef: PlugRef{Snap: "s1", Name: "i1"}, SlotRef: SlotRef{Snap: "s2", Name: "i1"}}, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	_, err = r.Connect(&ConnRef{PlugRef: PlugRef{Snap: "s1", Name: "i1"}, SlotRef: SlotRef{Snap: "s2", Name: "i1"}}, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	_, err = r.Connect(&ConnRef{PlugRef: PlugRef{Snap: "s1", Name: "i2"}, SlotRef: SlotRef{Snap: "s3", Name: "i2"}}, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	_, err = r.Connect(&ConnRef{PlugRef: PlugRef{Snap: "s4", Name: "i2"}, SlotRef: SlotRef{Snap: "s3_instance", Name: "i2"}}, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	// Without any names or options we get the summary of all the interfaces.
	infos := r.Info(nil)
	c.Assert(infos, DeepEquals, []*Info{
		{Name: "i1", Summary: "i1 summary"},
		{Name: "i2", Summary: "i2 summary"},
		{Name: "i3", Summary: "i3 summary"},
	})

	// We can choose specific interfaces, unknown names are just skipped.
	infos = r.Info(&InfoOptions{Names: []string{"i2", "i4"}})
	c.Assert(infos, DeepEquals, []*Info{
		{Name: "i2", Summary: "i2 summary"},
	})

	// We can ask for documentation.
	infos = r.Info(&InfoOptions{Names: []string{"i2"}, Doc: true})
	c.Assert(infos, DeepEquals, []*Info{
		{Name: "i2", Summary: "i2 summary", DocURL: "http://example.com/i2"},
	})

	// We can ask for a list of plugs.
	infos = r.Info(&InfoOptions{Names: []string{"i2"}, Plugs: true})
	c.Assert(infos, DeepEquals, []*Info{
		{Name: "i2", Summary: "i2 summary", Plugs: []*snap.PlugInfo{s1.Info().Plugs["i2"], s4.Info().Plugs["i2"]}},
	})

	// We can ask for a list of slots too.
	infos = r.Info(&InfoOptions{Names: []string{"i2"}, Slots: true})
	c.Assert(infos, DeepEquals, []*Info{
		{Name: "i2", Summary: "i2 summary", Slots: []*snap.SlotInfo{s3.Info().Slots["i2"], s3Instance.Info().Slots["i2"]}},
	})

	// We can also ask for only those interfaces that have connected plugs or slots.
	infos = r.Info(&InfoOptions{Connected: true})
	c.Assert(infos, DeepEquals, []*Info{
		{Name: "i1", Summary: "i1 summary"},
		{Name: "i2", Summary: "i2 summary"},
	})
}

const ifacehooksSnap1 = `
name: s1
version: 0
plugs:
  consumer:
    interface: iface2
    attr0: val0
`

const ifacehooksSnap2 = `
name: s2
version: 0
slots:
  producer:
    interface: iface2
    attr0: val0
`

func (s *RepositorySuite) TestBeforeConnectValidation(c *C) {
	err := s.emptyRepo.AddInterface(&ifacetest.TestInterface{
		InterfaceName: "iface2",
		BeforeConnectSlotCallback: func(slot *ConnectedSlot) error {
			var val string
			if err := slot.Attr("attr1", &val); err != nil {
				return err
			}
			return slot.SetAttr("attr1", fmt.Sprintf("%s-validated", val))
		},
		BeforeConnectPlugCallback: func(plug *ConnectedPlug) error {
			var val string
			if err := plug.Attr("attr1", &val); err != nil {
				return err
			}
			return plug.SetAttr("attr1", fmt.Sprintf("%s-validated", val))
		},
	})
	c.Assert(err, IsNil)

	s1 := ifacetest.MockInfoAndAppSet(c, ifacehooksSnap1, nil, nil)
	c.Assert(s.emptyRepo.AddAppSet(s1), IsNil)
	s2 := ifacetest.MockInfoAndAppSet(c, ifacehooksSnap2, nil, nil)
	c.Assert(s.emptyRepo.AddAppSet(s2), IsNil)

	plugDynAttrs := map[string]interface{}{"attr1": "val1"}
	slotDynAttrs := map[string]interface{}{"attr1": "val1"}

	policyCheck := func(plug *ConnectedPlug, slot *ConnectedSlot) (bool, error) { return true, nil }
	conn, err := s.emptyRepo.Connect(&ConnRef{PlugRef: PlugRef{Snap: "s1", Name: "consumer"}, SlotRef: SlotRef{Snap: "s2", Name: "producer"}}, nil, plugDynAttrs, nil, slotDynAttrs, policyCheck)
	c.Assert(err, IsNil)
	c.Assert(conn, NotNil)

	c.Assert(conn.Plug, NotNil)
	c.Assert(conn.Slot, NotNil)

	c.Assert(conn.Plug.StaticAttrs(), DeepEquals, map[string]interface{}{"attr0": "val0"})
	c.Assert(conn.Plug.DynamicAttrs(), DeepEquals, map[string]interface{}{"attr1": "val1-validated"})
	c.Assert(conn.Slot.StaticAttrs(), DeepEquals, map[string]interface{}{"attr0": "val0"})
	c.Assert(conn.Slot.DynamicAttrs(), DeepEquals, map[string]interface{}{"attr1": "val1-validated"})
}

func (s *RepositorySuite) TestBeforeConnectValidationFailure(c *C) {
	err := s.emptyRepo.AddInterface(&ifacetest.TestInterface{
		InterfaceName: "iface2",
		BeforeConnectSlotCallback: func(slot *ConnectedSlot) error {
			return fmt.Errorf("invalid slot")
		},
		BeforeConnectPlugCallback: func(plug *ConnectedPlug) error {
			return fmt.Errorf("invalid plug")
		},
	})
	c.Assert(err, IsNil)

	s1 := ifacetest.MockInfoAndAppSet(c, ifacehooksSnap1, nil, nil)
	c.Assert(s.emptyRepo.AddAppSet(s1), IsNil)
	s2 := ifacetest.MockInfoAndAppSet(c, ifacehooksSnap2, nil, nil)
	c.Assert(s.emptyRepo.AddAppSet(s2), IsNil)

	plugDynAttrs := map[string]interface{}{"attr1": "val1"}
	slotDynAttrs := map[string]interface{}{"attr1": "val1"}

	policyCheck := func(plug *ConnectedPlug, slot *ConnectedSlot) (bool, error) { return true, nil }

	conn, err := s.emptyRepo.Connect(&ConnRef{PlugRef: PlugRef{Snap: "s1", Name: "consumer"}, SlotRef: SlotRef{Snap: "s2", Name: "producer"}}, nil, plugDynAttrs, nil, slotDynAttrs, policyCheck)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `cannot connect plug "consumer" of snap "s1": invalid plug`)
	c.Assert(conn, IsNil)
}

func (s *RepositorySuite) TestBeforeConnectValidationPolicyCheckFailure(c *C) {
	err := s.emptyRepo.AddInterface(&ifacetest.TestInterface{
		InterfaceName:             "iface2",
		BeforeConnectSlotCallback: func(slot *ConnectedSlot) error { return nil },
		BeforeConnectPlugCallback: func(plug *ConnectedPlug) error { return nil },
	})
	c.Assert(err, IsNil)

	s1 := ifacetest.MockInfoAndAppSet(c, ifacehooksSnap1, nil, nil)
	c.Assert(s.emptyRepo.AddAppSet(s1), IsNil)
	s2 := ifacetest.MockInfoAndAppSet(c, ifacehooksSnap2, nil, nil)
	c.Assert(s.emptyRepo.AddAppSet(s2), IsNil)

	plugDynAttrs := map[string]interface{}{"attr1": "val1"}
	slotDynAttrs := map[string]interface{}{"attr1": "val1"}

	policyCheck := func(plug *ConnectedPlug, slot *ConnectedSlot) (bool, error) {
		return false, fmt.Errorf("policy check failed")
	}

	conn, err := s.emptyRepo.Connect(&ConnRef{PlugRef: PlugRef{Snap: "s1", Name: "consumer"}, SlotRef: SlotRef{Snap: "s2", Name: "producer"}}, nil, plugDynAttrs, nil, slotDynAttrs, policyCheck)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `policy check failed`)
	c.Assert(conn, IsNil)
}

func (s *RepositorySuite) TestConnection(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.producer), IsNil)

	connRef := NewConnRef(s.consumerPlug, s.producerSlot)

	_, err := s.testRepo.Connection(connRef)
	c.Assert(err, ErrorMatches, `no connection from consumer:plug to producer:slot`)

	_, err = s.testRepo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	conn, err := s.testRepo.Connection(connRef)
	c.Assert(err, IsNil)
	c.Assert(conn.Plug.Name(), Equals, "plug")
	c.Assert(conn.Slot.Name(), Equals, "slot")

	_, err = s.testRepo.Connection(&ConnRef{PlugRef: PlugRef{Snap: "a", Name: "b"}, SlotRef: SlotRef{Snap: "producer", Name: "slot"}})
	c.Assert(err, ErrorMatches, `snap "a" has no plug named "b"`)
	e, _ := err.(*NoPlugOrSlotError)
	c.Check(e, NotNil)

	_, err = s.testRepo.Connection(&ConnRef{PlugRef: PlugRef{Snap: "consumer", Name: "plug"}, SlotRef: SlotRef{Snap: "a", Name: "b"}})
	c.Assert(err, ErrorMatches, `snap "a" has no slot named "b"`)
	e, _ = err.(*NoPlugOrSlotError)
	c.Check(e, NotNil)
}

func (s *RepositorySuite) TestConnectWithStaticAttrs(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	c.Assert(s.testRepo.AddAppSet(s.producer), IsNil)

	connRef := NewConnRef(s.consumerPlug, s.producerSlot)

	plugAttrs := map[string]interface{}{"foo": "bar"}
	slotAttrs := map[string]interface{}{"boo": "baz"}
	_, err := s.testRepo.Connect(connRef, plugAttrs, nil, slotAttrs, nil, nil)
	c.Assert(err, IsNil)

	conn, err := s.testRepo.Connection(connRef)
	c.Assert(err, IsNil)
	c.Assert(conn.Plug.Name(), Equals, "plug")
	c.Assert(conn.Slot.Name(), Equals, "slot")
	c.Assert(conn.Plug.StaticAttrs(), DeepEquals, plugAttrs)
	c.Assert(conn.Slot.StaticAttrs(), DeepEquals, slotAttrs)
}

func (s *RepositorySuite) TestAllHotplugInterfaces(c *C) {
	repo := NewRepository()
	c.Assert(repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "iface1"}), IsNil)
	c.Assert(repo.AddInterface(&ifacetest.TestHotplugInterface{TestInterface: ifacetest.TestInterface{InterfaceName: "iface2"}}), IsNil)
	c.Assert(repo.AddInterface(&ifacetest.TestHotplugInterface{TestInterface: ifacetest.TestInterface{InterfaceName: "iface3"}}), IsNil)

	hi := repo.AllHotplugInterfaces()
	c.Assert(hi, HasLen, 2)
	c.Assert(hi["iface2"], DeepEquals, &ifacetest.TestHotplugInterface{TestInterface: ifacetest.TestInterface{InterfaceName: "iface2"}})
	c.Assert(hi["iface3"], DeepEquals, &ifacetest.TestHotplugInterface{TestInterface: ifacetest.TestInterface{InterfaceName: "iface3"}})
}

func (s *RepositorySuite) TestHotplugMethods(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)

	coreSlot := &snap.SlotInfo{
		Snap:       s.coreSnap,
		Name:       "test-slot",
		Interface:  "interface",
		HotplugKey: "1234",
	}
	s.coreSnap.Slots["test-slot"] = coreSlot

	c.Assert(s.testRepo.AddAppSet(s.coreSnapAppSet), IsNil)

	slotInfo, err := s.testRepo.SlotForHotplugKey("interface", "1234")
	c.Assert(err, IsNil)
	c.Check(slotInfo, DeepEquals, coreSlot)

	// no slot for device key 9999
	slotInfo, err = s.testRepo.SlotForHotplugKey("interface", "9999")
	c.Assert(err, IsNil)
	c.Check(slotInfo, IsNil)

	_, err = s.testRepo.Connect(NewConnRef(s.consumerPlug, coreSlot), nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	conns, err := s.testRepo.ConnectionsForHotplugKey("interface", "1234")
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, []*ConnRef{NewConnRef(s.consumerPlug, coreSlot)})

	// no connections for device 9999
	conns, err = s.testRepo.ConnectionsForHotplugKey("interface", "9999")
	c.Assert(err, IsNil)
	c.Check(conns, HasLen, 0)
}

func (s *RepositorySuite) TestUpdateHotplugSlotAttrs(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	coreSlot := &snap.SlotInfo{
		Snap:       s.coreSnap,
		Name:       "test-slot",
		Interface:  "interface",
		HotplugKey: "1234",
		Attrs:      map[string]interface{}{"a": "b"},
	}
	s.coreSnap.Slots["test-slot"] = coreSlot
	c.Assert(s.testRepo.AddAppSet(s.coreSnapAppSet), IsNil)

	slot, err := s.testRepo.UpdateHotplugSlotAttrs("interface", "unknownkey", nil)
	c.Assert(err, ErrorMatches, `cannot find hotplug slot for interface interface and hotplug key "unknownkey"`)
	c.Assert(slot, IsNil)

	newAttrs := map[string]interface{}{"c": "d"}
	slot, err = s.testRepo.UpdateHotplugSlotAttrs("interface", "1234", newAttrs)
	// attributes are copied, so this change shouldn't be visible
	newAttrs["c"] = "tainted"
	c.Assert(err, IsNil)
	c.Assert(slot, NotNil)
	c.Assert(slot.Attrs, DeepEquals, map[string]interface{}{"c": "d"})
	c.Assert(coreSlot.Attrs, DeepEquals, map[string]interface{}{"c": "d"})
}

func (s *RepositorySuite) TestUpdateHotplugSlotAttrsConnectedError(c *C) {
	c.Assert(s.testRepo.AddAppSet(s.consumer), IsNil)
	coreSlot := &snap.SlotInfo{
		Snap:       s.coreSnap,
		Name:       "test-slot",
		Interface:  "interface",
		HotplugKey: "1234",
	}
	s.coreSnap.Slots["test-slot"] = coreSlot
	c.Assert(s.testRepo.AddAppSet(s.coreSnapAppSet), IsNil)

	_, err := s.testRepo.Connect(NewConnRef(s.consumerPlug, coreSlot), nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	slot, err := s.testRepo.UpdateHotplugSlotAttrs("interface", "1234", map[string]interface{}{"c": "d"})
	c.Assert(err, ErrorMatches, `internal error: cannot update slot test-slot while connected`)
	c.Assert(slot, IsNil)
}
