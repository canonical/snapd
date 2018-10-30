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
	"sort"
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

func (s *helpersSuite) TestIdentityMapper(c *C) {
	var m ifacestate.SnapMapper = &ifacestate.IdentityMapper{}

	// Nothing is altered.
	c.Assert(m.RemapSnapFromState("example"), Equals, "example")
	c.Assert(m.RemapSnapToState("example"), Equals, "example")
	c.Assert(m.RemapSnapFromRequest("example"), Equals, "example")
}

func (s *helpersSuite) TestCoreCoreSystemMapper(c *C) {
	var m ifacestate.SnapMapper = &ifacestate.CoreCoreSystemMapper{}

	// Snaps are not renamed when interacting with the state.
	c.Assert(m.RemapSnapFromState("core"), Equals, "core")
	c.Assert(m.RemapSnapToState("core"), Equals, "core")

	// The "core" snap is renamed to the "system" in API response
	// and back in the API requests.
	c.Assert(m.RemapSnapFromRequest("system"), Equals, "core")

	// Other snap names are unchanged.
	c.Assert(m.RemapSnapFromState("potato"), Equals, "potato")
	c.Assert(m.RemapSnapToState("potato"), Equals, "potato")
	c.Assert(m.RemapSnapFromRequest("potato"), Equals, "potato")
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

	// The "core" snap is also renamed to "snapd" in API requests, for
	// compatibility.
	c.Assert(m.RemapSnapFromRequest("core"), Equals, "snapd")

	// Other snap names are unchanged.
	c.Assert(m.RemapSnapFromState("potato"), Equals, "potato")
	c.Assert(m.RemapSnapToState("potato"), Equals, "potato")
	c.Assert(m.RemapSnapFromRequest("potato"), Equals, "potato")
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

func (s *helpersSuite) TestMappingFunctions(c *C) {
	restore := ifacestate.MockSnapMapper(&caseMapper{})
	defer restore()

	c.Assert(ifacestate.RemapSnapFromState("example"), Equals, "EXAMPLE")
	c.Assert(ifacestate.RemapSnapToState("EXAMPLE"), Equals, "example")
	c.Assert(ifacestate.RemapSnapFromRequest("example"), Equals, "EXAMPLE")
}

func (s *helpersSuite) TestGetConns(c *C) {
	s.st.Lock()
	defer s.st.Unlock()
	s.st.Set("conns", map[string]interface{}{
		"app:network core:network": map[string]interface{}{
			"auto":      true,
			"interface": "network",
			"slot-static": map[string]interface{}{
				"number": int(78),
			},
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
		c.Assert(connState.StaticSlotAttrs["number"], Equals, int64(78))
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

func (s *helpersSuite) TestHotplugTaskHelpers(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	t := s.st.NewTask("foo", "")
	_, _, err := ifacestate.GetHotplugAttrs(t)
	c.Assert(err, ErrorMatches, `internal error: cannot get interface name from hotplug task: no state entry for key`)

	t.Set("interface", "x")
	_, _, err = ifacestate.GetHotplugAttrs(t)
	c.Assert(err, ErrorMatches, `internal error: cannot get hotplug key from hotplug task: no state entry for key`)

	ifacestate.SetHotplugAttrs(t, "iface", "key")

	var key, iface string
	c.Assert(t.Get("hotplug-key", &key), IsNil)
	c.Assert(key, Equals, "key")

	c.Assert(t.Get("interface", &iface), IsNil)
	c.Assert(iface, Equals, "iface")

	iface, key, err = ifacestate.GetHotplugAttrs(t)
	c.Assert(err, IsNil)
	c.Assert(key, Equals, "key")
	c.Assert(iface, Equals, "iface")
}

func (s *helpersSuite) TestHotplugSlotInfo(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	slots, err := ifacestate.GetHotplugSlots(s.st)
	c.Assert(err, IsNil)
	c.Assert(slots, HasLen, 0)

	defs := map[string]*ifacestate.HotplugSlotInfo{}
	defs["foo"] = &ifacestate.HotplugSlotInfo{
		Name:        "foo",
		Interface:   "iface",
		StaticAttrs: map[string]interface{}{"attr": "value"},
		HotplugKey:  "key",
	}
	ifacestate.SetHotplugSlots(s.st, defs)

	var data map[string]interface{}
	c.Assert(s.st.Get("hotplug-slots", &data), IsNil)
	c.Assert(data, DeepEquals, map[string]interface{}{
		"foo": map[string]interface{}{
			"name":         "foo",
			"interface":    "iface",
			"static-attrs": map[string]interface{}{"attr": "value"},
			"hotplug-key":  "key",
		}})

	slots, err = ifacestate.GetHotplugSlots(s.st)
	c.Assert(err, IsNil)
	c.Assert(slots, DeepEquals, defs)
}

func (s *helpersSuite) TestFindConnsForHotplugKey(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	st.Set("conns", map[string]interface{}{
		"snap1:plug1 core:slot1": map[string]interface{}{
			"interface":   "iface1",
			"hotplug-key": "key1",
		},
		"snap1:plug2 core:slot2": map[string]interface{}{
			"interface":   "iface2",
			"hotplug-key": "key1",
		},
		"snap1:plug3 core:slot3": map[string]interface{}{
			"interface":   "iface2",
			"hotplug-key": "key2",
		},
		"snap2:plug1 core:slot1": map[string]interface{}{
			"interface":   "iface2",
			"hotplug-key": "key2",
		},
	})

	conns, err := ifacestate.GetConns(st)
	c.Assert(err, IsNil)

	hotplugConns := ifacestate.FindConnsForHotplugKey(conns, "iface1", "key1")
	c.Assert(hotplugConns, DeepEquals, []string{"snap1:plug1 core:slot1"})

	hotplugConns = ifacestate.FindConnsForHotplugKey(conns, "iface2", "key2")
	sort.Strings(hotplugConns)
	c.Assert(hotplugConns, DeepEquals, []string{"snap1:plug3 core:slot3", "snap2:plug1 core:slot1"})

	hotplugConns = ifacestate.FindConnsForHotplugKey(conns, "unknown", "key1")
	c.Assert(hotplugConns, HasLen, 0)
}
