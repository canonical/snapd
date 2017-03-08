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

package builtin_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type BrowserSupportInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

const browserMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [browser-support]
`

var _ = Suite(&BrowserSupportInterfaceSuite{})

func (s *BrowserSupportInterfaceSuite) SetUpTest(c *C) {
	s.iface = &builtin.BrowserSupportInterface{}
	s.slot = &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
			Name:      "browser-support",
			Interface: "browser-support",
		},
	}
	plugSnap := snaptest.MockInfo(c, browserMockPlugSnapInfoYaml, nil)
	s.plug = &interfaces.Plug{PlugInfo: plugSnap.Plugs["browser-support"]}
}

func (s *BrowserSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "browser-support")
}

func (s *BrowserSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
}

func (s *BrowserSupportInterfaceSuite) TestSanitizePlugNoAttrib(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *BrowserSupportInterfaceSuite) TestSanitizePlugWithAttrib(c *C) {
	const mockSnapYaml = `name: browser-support-plug-snap
version: 1.0
plugs:
 browser-support:
  allow-sandbox: true
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)

	plug := &interfaces.Plug{PlugInfo: info.Plugs["browser-support"]}
	err := s.iface.SanitizePlug(plug)
	c.Assert(err, IsNil)
}

func (s *BrowserSupportInterfaceSuite) TestSanitizePlugWithBadAttrib(c *C) {
	const mockSnapYaml = `name: browser-support-plug-snap
version: 1.0
plugs:
 browser-support:
  allow-sandbox: bad
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)

	plug := &interfaces.Plug{PlugInfo: info.Plugs["browser-support"]}
	err := s.iface.SanitizePlug(plug)
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "browser-support plug requires bool with 'allow-sandbox'")
}

func (s *BrowserSupportInterfaceSuite) TestConnectedPlugSnippetWithoutAttrib(c *C) {
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `# Description: Can access various APIs needed by modern browers`)
	c.Assert(string(snippet), Not(testutil.Contains), `capability sys_admin,`)
	c.Assert(string(snippet), testutil.Contains, `deny ptrace (trace) peer=snap.@{SNAP_NAME}.**`)

	seccompSpec := &seccomp.Specification{}
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	snippets := seccompSpec.Snippets()
	c.Assert(len(snippets), Equals, 1)
	c.Assert(len(snippets["snap.other.app2"]), Equals, 1)
	secCompSnippet := snippets["snap.other.app2"][0]
	c.Assert(secCompSnippet, testutil.Contains, `# Description: Can access various APIs needed by modern browers`)
	c.Assert(secCompSnippet, Not(testutil.Contains), `chroot`)
}

func (s *BrowserSupportInterfaceSuite) TestConnectedPlugSnippetWithAttribFalse(c *C) {
	const mockSnapYaml = `name: browser-support-plug-snap
version: 1.0
plugs:
 browser-support:
  allow-sandbox: false
apps:
 app2:
  command: foo
  plugs: [browser-support]
`

	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := &interfaces.Plug{PlugInfo: info.Plugs["browser-support"]}

	snippet, err := s.iface.ConnectedPlugSnippet(plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `# Description: Can access various APIs needed by modern browers`)
	c.Assert(string(snippet), Not(testutil.Contains), `capability sys_admin,`)
	c.Assert(string(snippet), testutil.Contains, `deny ptrace (trace) peer=snap.@{SNAP_NAME}.**`)

	seccompSpec := &seccomp.Specification{}
	err = seccompSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	snippets := seccompSpec.Snippets()
	c.Assert(len(snippets), Equals, 1)
	c.Assert(len(snippets["snap.browser-support-plug-snap.app2"]), Equals, 1)
	secCompSnippet := snippets["snap.browser-support-plug-snap.app2"][0]
	c.Assert(secCompSnippet, testutil.Contains, `# Description: Can access various APIs needed by modern browers`)
	c.Assert(secCompSnippet, Not(testutil.Contains), `chroot`)
}

func (s *BrowserSupportInterfaceSuite) TestConnectedPlugSnippetWithAttribTrue(c *C) {
	const mockSnapYaml = `name: browser-support-plug-snap
version: 1.0
plugs:
 browser-support:
  allow-sandbox: true
apps:
 app2:
  command: foo
  plugs: [browser-support]
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := &interfaces.Plug{PlugInfo: info.Plugs["browser-support"]}

	snippet, err := s.iface.ConnectedPlugSnippet(plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `# Description: Can access various APIs needed by modern browers`)
	c.Assert(string(snippet), testutil.Contains, `ptrace (trace) peer=snap.@{SNAP_NAME}.**`)
	c.Assert(string(snippet), Not(testutil.Contains), `deny ptrace (trace) peer=snap.@{SNAP_NAME}.**`)

	seccompSpec := &seccomp.Specification{}
	err = seccompSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	snippets := seccompSpec.Snippets()
	c.Assert(len(snippets), Equals, 1)
	c.Assert(len(snippets["snap.browser-support-plug-snap.app2"]), Equals, 1)
	secCompSnippet := snippets["snap.browser-support-plug-snap.app2"][0]
	c.Assert(secCompSnippet, testutil.Contains, `# Description: Can access various APIs needed by modern browers`)
	c.Assert(secCompSnippet, testutil.Contains, `chroot`)
}

func (s *BrowserSupportInterfaceSuite) TestSanitizeIncorrectInterface(c *C) {
	c.Assert(func() { s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{Interface: "other"}}) },
		PanicMatches, `plug is not of interface "browser-support"`)
}

func (s *BrowserSupportInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	// connected plugs have a non-nil security snippet for apparmor
	seccompSpec := &seccomp.Specification{}
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.Snippets(), HasLen, 1)
}
