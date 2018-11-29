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
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
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

var snapdYaml = `name: snapd
version: 1.0
`

// Check what happens with system-key when security profile regeneration fails.
func (s *helpersSuite) TestSystemKeyAndFailingProfileRegeneration(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// Create a fake security backend with failing Setup method and mock all
	// security backends away so that we only use this special one. Note that
	// the backend is given a non-empty name as the interface manager skips
	// test backends with empty name for convenience.
	backend := &ifacetest.TestSecurityBackend{
		BackendName: "BROKEN",
		SetupCallback: func(snapInfo *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error {
			return errors.New("cannot setup security profile")
		},
	}
	restore := ifacestate.MockSecurityBackends([]interfaces.SecurityBackend{backend})
	defer restore()

	// Create a mock overlord, mainly to have state.
	ovld := overlord.Mock()
	st := ovld.State()

	// Put a fake snap in the state, we need to setup security for at least one
	// snap to give the fake security backend a chance to fail.
	yamlText := `
name: test-snapd-canary
version: 1
apps:
  test-snapd-canary:
    command: bin/canary
`
	si := &snap.SideInfo{Revision: snap.R(1), RealName: "test-snapd-canary"}
	snapInfo := snaptest.MockSnap(c, yamlText, si)
	st.Lock()
	snapst := &snapstate.SnapState{
		SnapType: string(snap.TypeApp),
		Sequence: []*snap.SideInfo{si},
		Active:   true,
		Current:  snap.R(1),
	}
	snapstate.Set(st, snapInfo.InstanceName(), snapst)
	st.Unlock()

	// Pretend that security profiles are out of date and mock the
	// function that writes the new system key with one always panics.
	restore = ifacestate.MockProfilesNeedRegeneration(func() bool { return true })
	defer restore()
	restore = ifacestate.MockWriteSystemKey(func() error { panic("system key should not be written") })
	defer restore()
	// Put a fake system key in place, we just want to see that file being removed.
	err := os.MkdirAll(filepath.Dir(dirs.SnapSystemKeyFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(dirs.SnapSystemKeyFile, []byte("system-key"), 0755)
	c.Assert(err, IsNil)

	// Put up a fake logger to capture logged messages.
	log, restore := logger.MockLogger()
	defer restore()

	// Construct the interface manager.
	_, err = ifacestate.Manager(st, nil, ovld.TaskRunner(), nil, nil)
	c.Assert(err, IsNil)

	// Check that system key is not on disk.
	c.Check(log.String(), testutil.Contains, `cannot regenerate BROKEN profile for snap "test-snapd-canary": cannot setup security profile`)
	c.Check(osutil.FileExists(dirs.SnapSystemKeyFile), Equals, false)
}
