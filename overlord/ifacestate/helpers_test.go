// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package ifacestate_test

import (
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/state"
)

type helpersSuite struct {
	st *state.State
}

var _ = Suite(&helpersSuite{})

func (s *helpersSuite) SetUpTest(c *C) {
	s.st = state.New(nil)
}

func (s *helpersSuite) TearDownTest(c *C) {
}

func (s *helpersSuite) TestNilMapper(c *C) {
	var m ifacestate.InterfaceMapper = &ifacestate.NilMapper{}

	// Nothing is altered.
	c.Assert(m.RemapIncomingPlugRef(&interfaces.PlugRef{}), Equals, false)
	c.Assert(m.RemapOutgoingPlugRef(&interfaces.PlugRef{}), Equals, false)
	c.Assert(m.RemapIncomingSlotRef(&interfaces.SlotRef{}), Equals, false)
	c.Assert(m.RemapOutgoingSlotRef(&interfaces.SlotRef{}), Equals, false)
}

func (s *helpersSuite) TestCoreSnapdMapper(c *C) {
	var m ifacestate.InterfaceMapper = &ifacestate.CoreSnapdMapper{}

	// Plugs are not altered.
	plugRef := interfaces.PlugRef{Snap: "example", Name: "network"}
	c.Assert(m.RemapIncomingPlugRef(&plugRef), Equals, false)
	c.Assert(m.RemapOutgoingPlugRef(&plugRef), Equals, false)

	// The "snapd" snap is used on the inside while appearing as "core" on the outside.
	slotRef := interfaces.SlotRef{Snap: "core", Name: "network"}
	c.Assert(m.RemapIncomingSlotRef(&slotRef), Equals, true)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "snapd", Name: "network"})
	c.Assert(m.RemapOutgoingSlotRef(&slotRef), Equals, true)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "core", Name: "network"})

	// Other slots are unchanged.
	slotRef = interfaces.SlotRef{Snap: "snap", Name: "slot"}
	c.Assert(m.RemapIncomingSlotRef(&slotRef), Equals, false)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "snap", Name: "slot"})
	c.Assert(m.RemapOutgoingSlotRef(&slotRef), Equals, false)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "snap", Name: "slot"})
}

// caseMapper implements InterfaceMapper to use upper case internally and lower case externally.
type caseMapper struct{}

func (m *caseMapper) RemapIncomingPlugRef(plugRef *interfaces.PlugRef) (changed bool) {
	plugRef.Snap = strings.ToUpper(plugRef.Snap)
	plugRef.Name = strings.ToUpper(plugRef.Name)
	return true
}

func (m *caseMapper) RemapOutgoingPlugRef(plugRef *interfaces.PlugRef) (changed bool) {
	plugRef.Snap = strings.ToLower(plugRef.Snap)
	plugRef.Name = strings.ToLower(plugRef.Name)
	return true
}

func (m *caseMapper) RemapIncomingSlotRef(slotRef *interfaces.SlotRef) (changed bool) {
	slotRef.Snap = strings.ToUpper(slotRef.Snap)
	slotRef.Name = strings.ToUpper(slotRef.Name)
	return true
}

func (m *caseMapper) RemapOutgoingSlotRef(slotRef *interfaces.SlotRef) (changed bool) {
	slotRef.Snap = strings.ToLower(slotRef.Snap)
	slotRef.Name = strings.ToLower(slotRef.Name)
	return true
}

func (s *helpersSuite) TestRemapIncomingPlugRef(c *C) {
	restore := ifacestate.MockInterfaceMapper(&caseMapper{})
	defer restore()

	plugRef := interfaces.PlugRef{Snap: "example", Name: "network"}
	c.Assert(ifacestate.RemapIncomingPlugRef(&plugRef), Equals, true)
	c.Assert(plugRef, DeepEquals, interfaces.PlugRef{Snap: "EXAMPLE", Name: "NETWORK"})
}

func (s *helpersSuite) TestRemapOutgoingPlugRef(c *C) {
	restore := ifacestate.MockInterfaceMapper(&caseMapper{})
	defer restore()

	plugRef := interfaces.PlugRef{Snap: "EXAMPLE", Name: "NETWORK"}
	c.Assert(ifacestate.RemapOutgoingPlugRef(&plugRef), Equals, true)
	c.Assert(plugRef, DeepEquals, interfaces.PlugRef{Snap: "example", Name: "network"})
}

func (s *helpersSuite) TestRemapIncomingSlotRef(c *C) {
	restore := ifacestate.MockInterfaceMapper(&caseMapper{})
	defer restore()

	slotRef := interfaces.SlotRef{Snap: "example", Name: "network"}
	c.Assert(ifacestate.RemapIncomingSlotRef(&slotRef), Equals, true)
	c.Assert(slotRef, DeepEquals, interfaces.SlotRef{Snap: "EXAMPLE", Name: "NETWORK"})
}

func (s *helpersSuite) TestRemapOutgoingSlotRef(c *C) {
	restore := ifacestate.MockInterfaceMapper(&caseMapper{})
	defer restore()

	slotRef := interfaces.SlotRef{Snap: "EXAMPLE", Name: "NETWORK"}
	c.Assert(ifacestate.RemapOutgoingSlotRef(&slotRef), Equals, true)
	c.Assert(slotRef, DeepEquals, interfaces.SlotRef{Snap: "example", Name: "network"})
}

func (s *helpersSuite) TestRemapIncomingConnRef(c *C) {
	restore := ifacestate.MockInterfaceMapper(&caseMapper{})
	defer restore()

	cref := interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "example", Name: "network"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "network"},
	}
	c.Assert(ifacestate.RemapIncomingConnRef(&cref), Equals, true)
	c.Assert(cref, DeepEquals, interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "EXAMPLE", Name: "NETWORK"},
		SlotRef: interfaces.SlotRef{Snap: "CORE", Name: "NETWORK"},
	})
}

func (s *helpersSuite) TestRemapOutgoingConnRef(c *C) {
	restore := ifacestate.MockInterfaceMapper(&caseMapper{})
	defer restore()

	cref := interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "EXAMPLE", Name: "NETWORK"},
		SlotRef: interfaces.SlotRef{Snap: "CORE", Name: "NETWORK"},
	}
	c.Assert(ifacestate.RemapOutgoingConnRef(&cref), Equals, true)
	c.Assert(cref, DeepEquals, interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "example", Name: "network"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "network"},
	})
}

func (s *helpersSuite) TestGetConns(c *C) {
	s.st.Lock()
	defer s.st.Unlock()
	s.st.Set("conns", map[string]interface{}{
		"app:network core:network": map[string]interface{}{
			"auto":      true,
			"interface": "network",
		},
	})

	restore := ifacestate.MockInterfaceMapper(&caseMapper{})
	defer restore()

	conns, err := ifacestate.GetConns(s.st)
	c.Assert(err, IsNil)
	for id, connState := range conns {
		c.Assert(id, Equals, "APP:NETWORK CORE:NETWORK")
		c.Assert(connState.Auto, Equals, true)
		c.Assert(connState.Interface, Equals, "network")
	}
}

func (s *helpersSuite) TestSetConns(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	restore := ifacestate.MockInterfaceMapper(&caseMapper{})
	defer restore()

	// This has upper-case data internally, see export_test.go
	ifacestate.SetConns(s.st, ifacestate.UpperCaseConnState())
	var conns map[string]interface{}
	err := s.st.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Assert(conns, DeepEquals, map[string]interface{}{
		"app:network core:network": map[string]interface{}{
			"auto":      true,
			"interface": "network",
		}})
}
