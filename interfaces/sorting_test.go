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
)

type SortingSuite struct{}

var _ = Suite(&SortingSuite{})

func newConnRef(plugSnap, plug, slotSnap, slot string) *interfaces.ConnRef {
	return &interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: plugSnap, Name: plug}, SlotRef: interfaces.SlotRef{Snap: slotSnap, Name: slot}}
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

func (s *SortingSuite) TestByConnRef(c *C) {
	list := []*interfaces.ConnRef{
		newConnRef("name-1", "plug-3", "name-2", "slot-1"),
		newConnRef("name-1", "plug-1", "name-2", "slot-3"),
		newConnRef("name-1", "plug-2", "name-2", "slot-2"),
		newConnRef("name-1", "plug-1", "name-2", "slot-4"),
		newConnRef("name-1", "plug-1", "name-2", "slot-1"),
		newConnRef("name-1", "plug-1", "name-2_instance", "slot-1"),
		newConnRef("name-1_instance", "plug-1", "name-2", "slot-1"),
	}
	sort.Sort(interfaces.ByConnRef(list))

	c.Assert(list, DeepEquals, []*interfaces.ConnRef{
		newConnRef("name-1", "plug-1", "name-2", "slot-1"),
		newConnRef("name-1", "plug-1", "name-2", "slot-3"),
		newConnRef("name-1", "plug-1", "name-2", "slot-4"),
		newConnRef("name-1", "plug-1", "name-2_instance", "slot-1"),
		newConnRef("name-1", "plug-2", "name-2", "slot-2"),
		newConnRef("name-1", "plug-3", "name-2", "slot-1"),
		newConnRef("name-1_instance", "plug-1", "name-2", "slot-1"),
	})
}
