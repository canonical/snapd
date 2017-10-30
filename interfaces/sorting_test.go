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
	"sort"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/snap"
)

type SortingSuite struct{}

var _ = Suite(&SortingSuite{})

func (s *SortingSuite) TestSortBySlotRef(c *C) {
	list := []interfaces.SlotRef{
		{
			Snap: "snap-2",
			Name: "name-2",
		},
		{
			Snap: "snap-1",
			Name: "name-2",
		},
		{
			Snap: "snap-1",
			Name: "name-1",
		},
	}
	sort.Sort(interfaces.BySlotRef(list))
	c.Assert(list, DeepEquals, []interfaces.SlotRef{
		{
			Snap: "snap-1",
			Name: "name-1",
		},
		{
			Snap: "snap-1",
			Name: "name-2",
		},
		{
			Snap: "snap-2",
			Name: "name-2",
		},
	})
}

func (s *SortingSuite) TestSortByPlugRef(c *C) {
	list := []interfaces.PlugRef{
		{
			Snap: "snap-2",
			Name: "name-2",
		},
		{
			Snap: "snap-1",
			Name: "name-2",
		},
		{
			Snap: "snap-1",
			Name: "name-1",
		},
	}
	sort.Sort(interfaces.ByPlugRef(list))
	c.Assert(list, DeepEquals, []interfaces.PlugRef{
		{
			Snap: "snap-1",
			Name: "name-1",
		},
		{
			Snap: "snap-1",
			Name: "name-2",
		},
		{
			Snap: "snap-2",
			Name: "name-2",
		},
	})
}

func (s *SortingSuite) TestByBackendName(c *C) {
	list := []interfaces.SecurityBackend{
		&ifacetest.TestSecurityBackend{BackendName: "backend-2"},
		&ifacetest.TestSecurityBackend{BackendName: "backend-1"},
	}
	sort.Sort(interfaces.ByBackendName(list))
	c.Assert(list, DeepEquals, []interfaces.SecurityBackend{
		&ifacetest.TestSecurityBackend{BackendName: "backend-1"},
		&ifacetest.TestSecurityBackend{BackendName: "backend-2"},
	})
}

func (s *SortingSuite) TestByInterfaceName(c *C) {
	list := []interfaces.Interface{
		&ifacetest.TestInterface{InterfaceName: "iface-2"},
		&ifacetest.TestInterface{InterfaceName: "iface-1"},
	}
	sort.Sort(interfaces.ByInterfaceName(list))
	c.Assert(list, DeepEquals, []interfaces.Interface{
		&ifacetest.TestInterface{InterfaceName: "iface-1"},
		&ifacetest.TestInterface{InterfaceName: "iface-2"},
	})
}

func (s *SortingSuite) TestByPlugInfo(c *C) {
	list := []*snap.PlugInfo{
		{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "plug-2"},
		{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "plug-1"},
		{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-2"},
		{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-1"},
	}
	sort.Sort(interfaces.ByPlugInfo(list))
	c.Assert(list, DeepEquals, []*snap.PlugInfo{
		{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-1"},
		{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-2"},
		{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "plug-1"},
		{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "plug-2"},
	})
}

func (s *SortingSuite) TestBySlotInfo(c *C) {
	list := []*snap.SlotInfo{
		{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "plug-2"},
		{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "plug-1"},
		{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-2"},
		{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-1"},
	}
	sort.Sort(interfaces.BySlotInfo(list))
	c.Assert(list, DeepEquals, []*snap.SlotInfo{
		{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-1"},
		{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-2"},
		{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "plug-1"},
		{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "plug-2"},
	})
}

func (s *SortingSuite) TestByConnRef(c *C) {
	list := []*interfaces.ConnRef{
		interfaces.NewConnRef(
			&snap.PlugInfo{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-3"},
			&snap.SlotInfo{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "slot-1"}),
		interfaces.NewConnRef(
			&snap.PlugInfo{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-1"},
			&snap.SlotInfo{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "slot-3"}),
		interfaces.NewConnRef(
			&snap.PlugInfo{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-2"},
			&snap.SlotInfo{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "slot-2"}),
		interfaces.NewConnRef(
			&snap.PlugInfo{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-1"},
			&snap.SlotInfo{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "slot-4"}),
		interfaces.NewConnRef(
			&snap.PlugInfo{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-1"},
			&snap.SlotInfo{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "slot-1"}),
	}
	sort.Sort(interfaces.ByConnRef(list))

	c.Assert(list, DeepEquals, []*interfaces.ConnRef{
		interfaces.NewConnRef(
			&snap.PlugInfo{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-1"},
			&snap.SlotInfo{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "slot-1"}),
		interfaces.NewConnRef(
			&snap.PlugInfo{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-1"},
			&snap.SlotInfo{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "slot-3"}),
		interfaces.NewConnRef(
			&snap.PlugInfo{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-1"},
			&snap.SlotInfo{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "slot-4"}),
		interfaces.NewConnRef(
			&snap.PlugInfo{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-2"},
			&snap.SlotInfo{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "slot-2"}),
		interfaces.NewConnRef(
			&snap.PlugInfo{Snap: &snap.Info{SuggestedName: "name-1"}, Name: "plug-3"},
			&snap.SlotInfo{Snap: &snap.Info{SuggestedName: "name-2"}, Name: "slot-1"}),
	})
}
