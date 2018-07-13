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

func (s *helpersSuite) TestIdentityMapper(c *C) {
	var m ifacestate.InterfaceMapper = &ifacestate.IdentityMapper{}

	// Nothing is altered.
	plugRef := interfaces.PlugRef{Snap: "example", Name: "network"}
	m.RemapPlugRefFromState(&plugRef)
	c.Assert(plugRef, Equals, interfaces.PlugRef{Snap: "example", Name: "network"})
	m.RemapPlugRefToState(&plugRef)
	c.Assert(plugRef, Equals, interfaces.PlugRef{Snap: "example", Name: "network"})
	m.RemapPlugRefFromRequest(&plugRef)
	c.Assert(plugRef, Equals, interfaces.PlugRef{Snap: "example", Name: "network"})
	m.RemapPlugRefToResponse(&plugRef)
	c.Assert(plugRef, Equals, interfaces.PlugRef{Snap: "example", Name: "network"})

	slotRef := interfaces.SlotRef{Snap: "core", Name: "network"}
	m.RemapSlotRefFromState(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "core", Name: "network"})
	m.RemapSlotRefToState(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "core", Name: "network"})
	m.RemapSlotRefFromRequest(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "core", Name: "network"})
	m.RemapSlotRefToResponse(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "core", Name: "network"})
}

func (s *helpersSuite) TestCoreCoreSystemMapper(c *C) {
	var m ifacestate.InterfaceMapper = &ifacestate.CoreCoreSystemMapper{}

	// Plugs are not altered in any way.
	plugRef := interfaces.PlugRef{Snap: "example", Name: "network"}
	m.RemapPlugRefFromState(&plugRef)
	c.Assert(plugRef, Equals, interfaces.PlugRef{Snap: "example", Name: "network"})
	m.RemapPlugRefToState(&plugRef)
	c.Assert(plugRef, Equals, interfaces.PlugRef{Snap: "example", Name: "network"})
	m.RemapPlugRefFromRequest(&plugRef)
	c.Assert(plugRef, Equals, interfaces.PlugRef{Snap: "example", Name: "network"})
	m.RemapPlugRefToResponse(&plugRef)
	c.Assert(plugRef, Equals, interfaces.PlugRef{Snap: "example", Name: "network"})

	// Slots are not altered when interacting with the state.
	slotRef := interfaces.SlotRef{Snap: "core", Name: "network"}
	m.RemapSlotRefFromState(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "core", Name: "network"})
	m.RemapSlotRefToState(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "core", Name: "network"})

	// When a slot referring to "core" is returned in API response it is
	// re-mapped to "system" and symmetrically back in API requests.
	slotRef = interfaces.SlotRef{Snap: "core", Name: "network"}
	m.RemapSlotRefToResponse(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "system", Name: "network"})
	m.RemapSlotRefFromRequest(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "core", Name: "network"})

	// Other slots are unchanged.
	slotRef = interfaces.SlotRef{Snap: "snap", Name: "slot"}
	m.RemapSlotRefFromState(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "snap", Name: "slot"})
	m.RemapSlotRefToState(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "snap", Name: "slot"})
	m.RemapSlotRefFromRequest(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "snap", Name: "slot"})
	m.RemapSlotRefToResponse(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "snap", Name: "slot"})
}

func (s *helpersSuite) TestCoreSnapdSystemMapper(c *C) {
	var m ifacestate.InterfaceMapper = &ifacestate.CoreSnapdSystemMapper{}

	// Plugs are not altered.
	plugRef := interfaces.PlugRef{Snap: "example", Name: "network"}
	m.RemapPlugRefFromState(&plugRef)
	c.Assert(plugRef, Equals, interfaces.PlugRef{Snap: "example", Name: "network"})
	m.RemapPlugRefToState(&plugRef)
	c.Assert(plugRef, Equals, interfaces.PlugRef{Snap: "example", Name: "network"})
	m.RemapPlugRefFromRequest(&plugRef)
	c.Assert(plugRef, Equals, interfaces.PlugRef{Snap: "example", Name: "network"})
	m.RemapPlugRefToResponse(&plugRef)
	c.Assert(plugRef, Equals, interfaces.PlugRef{Snap: "example", Name: "network"})

	// When a slot referring to "core" is loaded from the state it re-mapped to "snapd".
	// Symmetrically when said slot is saved to state it is re-mapped back.
	slotRef := interfaces.SlotRef{Snap: "core", Name: "network"}
	m.RemapSlotRefFromState(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "snapd", Name: "network"})
	m.RemapSlotRefToState(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "core", Name: "network"})

	// When a slot referring to "snapd" is returned in API response it is
	// re-mapped to "system". Not fully symmetrically API requests referring to
	// either "core" or "system" are re-mapped to "snapd".
	slotRef = interfaces.SlotRef{Snap: "snapd", Name: "network"}
	m.RemapSlotRefToResponse(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "system", Name: "network"})

	slotRef = interfaces.SlotRef{Snap: "system", Name: "network"}
	m.RemapSlotRefFromRequest(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "snapd", Name: "network"})
	slotRef = interfaces.SlotRef{Snap: "core", Name: "network"}
	m.RemapSlotRefFromRequest(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "snapd", Name: "network"})

	// Other slots are unchanged.
	slotRef = interfaces.SlotRef{Snap: "snap", Name: "slot"}
	m.RemapSlotRefFromState(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "snap", Name: "slot"})
	m.RemapSlotRefToState(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "snap", Name: "slot"})
	m.RemapSlotRefFromRequest(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "snap", Name: "slot"})
	m.RemapSlotRefToResponse(&slotRef)
	c.Assert(slotRef, Equals, interfaces.SlotRef{Snap: "snap", Name: "slot"})
}

// caseMapper implements InterfaceMapper to use upper case internally and lower case externally.
type caseMapper struct{}

// memory <=> state

func (m *caseMapper) RemapSnapFromState(snapName string) string {
	return strings.ToUpper(snapName)
}

func (m *caseMapper) RemapSnapToState(snapName string) string {
	return strings.ToLower(snapName)
}

func (m *caseMapper) RemapPlugRefFromState(plugRef *interfaces.PlugRef) {
	plugRef.Snap = strings.ToUpper(plugRef.Snap)
	plugRef.Name = strings.ToUpper(plugRef.Name)
}

func (m *caseMapper) RemapPlugRefToState(plugRef *interfaces.PlugRef) {
	plugRef.Snap = strings.ToLower(plugRef.Snap)
	plugRef.Name = strings.ToLower(plugRef.Name)
}

func (m *caseMapper) RemapSlotRefFromState(slotRef *interfaces.SlotRef) {
	slotRef.Snap = strings.ToUpper(slotRef.Snap)
	slotRef.Name = strings.ToUpper(slotRef.Name)
}

func (m *caseMapper) RemapSlotRefToState(slotRef *interfaces.SlotRef) {
	slotRef.Snap = strings.ToLower(slotRef.Snap)
	slotRef.Name = strings.ToLower(slotRef.Name)
}

// memory <=> request

func (m *caseMapper) RemapSnapFromRequest(snapName string) string {
	return strings.ToUpper(snapName)
}

func (m *caseMapper) RemapSnapToResponse(snapName string) string {
	return strings.ToLower(snapName)
}

func (m *caseMapper) RemapPlugRefFromRequest(plugRef *interfaces.PlugRef) {
	plugRef.Snap = strings.ToUpper(plugRef.Snap)
	plugRef.Name = strings.ToUpper(plugRef.Name)
}

func (m *caseMapper) RemapPlugRefToResponse(plugRef *interfaces.PlugRef) {
	plugRef.Snap = strings.ToLower(plugRef.Snap)
	plugRef.Name = strings.ToLower(plugRef.Name)
}

func (m *caseMapper) RemapSlotRefFromRequest(slotRef *interfaces.SlotRef) {
	slotRef.Snap = strings.ToUpper(slotRef.Snap)
	slotRef.Name = strings.ToUpper(slotRef.Name)
}

func (m *caseMapper) RemapSlotRefToResponse(slotRef *interfaces.SlotRef) {
	slotRef.Snap = strings.ToLower(slotRef.Snap)
	slotRef.Name = strings.ToLower(slotRef.Name)
}

func (s *helpersSuite) TestRemapPlugRefFromState(c *C) {
	restore := ifacestate.MockInterfaceMapper(&caseMapper{})
	defer restore()

	origPlugRef := interfaces.PlugRef{Snap: "example", Name: "network"}
	chndPlugRef := ifacestate.RemapPlugRefFromState(origPlugRef)
	c.Assert(chndPlugRef, DeepEquals, interfaces.PlugRef{Snap: "EXAMPLE", Name: "NETWORK"})
}

func (s *helpersSuite) TestRemapPlugRefToState(c *C) {
	restore := ifacestate.MockInterfaceMapper(&caseMapper{})
	defer restore()

	origPlugRef := interfaces.PlugRef{Snap: "EXAMPLE", Name: "NETWORK"}
	chndPlugRef := ifacestate.RemapPlugRefToState(origPlugRef)
	c.Assert(chndPlugRef, DeepEquals, interfaces.PlugRef{Snap: "example", Name: "network"})
}

func (s *helpersSuite) TestRemapSlotRefFromState(c *C) {
	restore := ifacestate.MockInterfaceMapper(&caseMapper{})
	defer restore()

	origSlotRef := interfaces.SlotRef{Snap: "example", Name: "network"}
	chndSlotRef := ifacestate.RemapSlotRefFromState(origSlotRef)
	c.Assert(chndSlotRef, DeepEquals, interfaces.SlotRef{Snap: "EXAMPLE", Name: "NETWORK"})
}

func (s *helpersSuite) TestSlotRefToState(c *C) {
	restore := ifacestate.MockInterfaceMapper(&caseMapper{})
	defer restore()

	origSlotRef := interfaces.SlotRef{Snap: "EXAMPLE", Name: "NETWORK"}
	chndSlotRef := ifacestate.RemapSlotRefToState(origSlotRef)
	c.Assert(chndSlotRef, DeepEquals, interfaces.SlotRef{Snap: "example", Name: "network"})
}

func (s *helpersSuite) TestRemapPlugRefFromRequest(c *C) {
	restore := ifacestate.MockInterfaceMapper(&caseMapper{})
	defer restore()

	origPlugRef := interfaces.PlugRef{Snap: "example", Name: "network"}
	chndPlugRef := ifacestate.RemapPlugRefFromRequest(origPlugRef)
	c.Assert(chndPlugRef, DeepEquals, interfaces.PlugRef{Snap: "EXAMPLE", Name: "NETWORK"})
}

func (s *helpersSuite) TestRemapPlugRefToResponse(c *C) {
	restore := ifacestate.MockInterfaceMapper(&caseMapper{})
	defer restore()

	origPlugRef := interfaces.PlugRef{Snap: "EXAMPLE", Name: "NETWORK"}
	chndPlugRef := ifacestate.RemapPlugRefToResponse(origPlugRef)
	c.Assert(chndPlugRef, DeepEquals, interfaces.PlugRef{Snap: "example", Name: "network"})
}

func (s *helpersSuite) TestRemapSlotRefFromRequest(c *C) {
	restore := ifacestate.MockInterfaceMapper(&caseMapper{})
	defer restore()

	origSlotRef := interfaces.SlotRef{Snap: "example", Name: "network"}
	chndSlotRef := ifacestate.RemapSlotRefFromRequest(origSlotRef)
	c.Assert(chndSlotRef, DeepEquals, interfaces.SlotRef{Snap: "EXAMPLE", Name: "NETWORK"})
}

func (s *helpersSuite) TestSlotRefToResponse(c *C) {
	restore := ifacestate.MockInterfaceMapper(&caseMapper{})
	defer restore()

	origSlotRef := interfaces.SlotRef{Snap: "EXAMPLE", Name: "NETWORK"}
	chndSlotRef := ifacestate.RemapSlotRefToResponse(origSlotRef)
	c.Assert(chndSlotRef, DeepEquals, interfaces.SlotRef{Snap: "example", Name: "network"})
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
