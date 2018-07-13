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
	var m ifacestate.SnapMapper = &ifacestate.IdentityMapper{}

	// Nothing is altered.
	c.Assert(m.RemapSnapFromState("example"), Equals, "example")
	c.Assert(m.RemapSnapToState("example"), Equals, "example")
	c.Assert(m.RemapSnapFromRequest("example"), Equals, "example")
	c.Assert(m.RemapSnapToResponse("example"), Equals, "example")
}

func (s *helpersSuite) TestCoreCoreSystemMapper(c *C) {
	var m ifacestate.SnapMapper = &ifacestate.CoreCoreSystemMapper{}

	// Snaps are not renamed when interacting with the state.
	c.Assert(m.RemapSnapFromState("core"), Equals, "core")
	c.Assert(m.RemapSnapToState("core"), Equals, "core")

	// The "core" snap is renamed to the "system" in API response
	// and back in the API requests.
	c.Assert(m.RemapSnapFromRequest("system"), Equals, "core")
	c.Assert(m.RemapSnapToResponse("core"), Equals, "system")
}

func (s *helpersSuite) TestCoreSnapdSystemMapper(c *C) {
	var m ifacestate.SnapMapper = &ifacestate.CoreSnapdSystemMapper{}

	// The "snapd" snap is renamed to the "core" in when saving the state
	// and back when loading the state.
	c.Assert(m.RemapSnapFromState("core"), Equals, "snapd")
	c.Assert(m.RemapSnapToState("snapd"), Equals, "core")

	// The "snapd" snap is renamed to the "system" in API response and back in
	// the API requests.
	c.Assert(m.RemapSnapFromRequest("system"), Equals, "snapd")
	c.Assert(m.RemapSnapToResponse("snapd"), Equals, "system")

	// The "core" snap is also renamed to "snapd" in API requests, for
	// compatibility.
	c.Assert(m.RemapSnapFromRequest("core"), Equals, "snapd")
}

// caseMapper implements SnapMapper to use upper case internally and lower case externally.
type caseMapper struct{}

func (m *caseMapper) RemapSnapFromState(snapName string) string {
	return strings.ToUpper(snapName)
}

func (m *caseMapper) RemapSnapToState(snapName string) string {
	return strings.ToLower(snapName)
}

func (m *caseMapper) RemapSnapFromRequest(snapName string) string {
	return strings.ToUpper(snapName)
}

func (m *caseMapper) RemapSnapToResponse(snapName string) string {
	return strings.ToLower(snapName)
}

func (s *helpersSuite) TestMappingFunctions(c *C) {
	restore := ifacestate.MockSnapMapper(&caseMapper{})
	defer restore()

	c.Assert(ifacestate.RemapSnapFromState("example"), Equals, "EXAMPLE")
	c.Assert(ifacestate.RemapSnapToState("EXAMPLE"), Equals, "example")
	c.Assert(ifacestate.RemapSnapFromRequest("example"), Equals, "EXAMPLE")
	c.Assert(ifacestate.RemapSnapToResponse("EXAMPLE"), Equals, "example")
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

	restore := ifacestate.MockSnapMapper(&caseMapper{})
	defer restore()

	conns, err := ifacestate.GetConns(s.st)
	c.Assert(err, IsNil)
	for id, connState := range conns {
		c.Assert(id, Equals, "APP:network CORE:network")
		c.Assert(connState.Auto, Equals, true)
		c.Assert(connState.Interface, Equals, "network")
	}
}

func (s *helpersSuite) TestSetConns(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	restore := ifacestate.MockSnapMapper(&caseMapper{})
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
