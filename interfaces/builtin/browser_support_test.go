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
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type BrowserSupportInterfaceSuite struct {
	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

const browserMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [browser-support]
`

var _ = Suite(&BrowserSupportInterfaceSuite{
	iface: builtin.MustInterface("browser-support"),
})

func (s *BrowserSupportInterfaceSuite) SetUpTest(c *C) {
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", SnapType: snap.TypeOS},
		Name:      "browser-support",
		Interface: "browser-support",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
	plugSnap := snaptest.MockInfo(c, browserMockPlugSnapInfoYaml, nil)
	s.plugInfo = plugSnap.Plugs["browser-support"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *BrowserSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "browser-support")
}

func (s *BrowserSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *BrowserSupportInterfaceSuite) TestSanitizePlugNoAttrib(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *BrowserSupportInterfaceSuite) TestSanitizePlugWithAttrib(c *C) {
	const mockSnapYaml = `name: browser-support-plug-snap
version: 1.0
plugs:
 browser-support:
  allow-sandbox: true
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["browser-support"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), IsNil)
}

func (s *BrowserSupportInterfaceSuite) TestSanitizePlugWithBadAttrib(c *C) {
	const mockSnapYaml = `name: browser-support-plug-snap
version: 1.0
plugs:
 browser-support:
  allow-sandbox: bad
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["browser-support"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches,
		"browser-support plug requires bool with 'allow-sandbox'")
}

func (s *BrowserSupportInterfaceSuite) TestConnectedPlugSnippetWithoutAttrib(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	snippet := apparmorSpec.SnippetForTag("snap.other.app2")
	c.Assert(string(snippet), testutil.Contains, `# Description: Can access various APIs needed by modern browsers`)
	c.Assert(string(snippet), Not(testutil.Contains), `capability sys_admin,`)

	appSet, err = interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	seccompSpec := seccomp.NewSpecification(appSet)
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	secCompSnippet := seccompSpec.SnippetForTag("snap.other.app2")
	c.Assert(secCompSnippet, testutil.Contains, `# Description: Can access various APIs needed by modern browsers`)
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
	plug := interfaces.NewConnectedPlug(info.Plugs["browser-support"], nil, nil)

	appSet, err := interfaces.NewSnapAppSet(plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.browser-support-plug-snap.app2"})
	snippet := apparmorSpec.SnippetForTag("snap.browser-support-plug-snap.app2")
	c.Assert(snippet, testutil.Contains, `# Description: Can access various APIs needed by modern browsers`)
	c.Assert(snippet, Not(testutil.Contains), `capability sys_admin,`)
	c.Assert(snippet, Not(testutil.Contains), `userns,`)

	appSet, err = interfaces.NewSnapAppSet(plug.Snap(), nil)
	c.Assert(err, IsNil)
	seccompSpec := seccomp.NewSpecification(appSet)
	err = seccompSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.browser-support-plug-snap.app2"})
	secCompSnippet := seccompSpec.SnippetForTag("snap.browser-support-plug-snap.app2")
	c.Assert(secCompSnippet, testutil.Contains, `# Description: Can access various APIs needed by modern browsers`)
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
	plug := interfaces.NewConnectedPlug(info.Plugs["browser-support"], nil, nil)

	restore := apparmor_sandbox.MockFeatures(nil, nil, nil, nil)
	defer restore()
	appSet, err := interfaces.NewSnapAppSet(plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.browser-support-plug-snap.app2"})
	snippet := apparmorSpec.SnippetForTag("snap.browser-support-plug-snap.app2")
	c.Assert(snippet, testutil.Contains, `# Description: Can access various APIs needed by modern browsers`)
	c.Assert(snippet, testutil.Contains, `ptrace (trace) peer=snap.@{SNAP_INSTANCE_NAME}.**`)
	// we haven't mocked the userns apparmor feature
	c.Assert(snippet, Not(testutil.Contains), `userns,`)

	appSet, err = interfaces.NewSnapAppSet(plug.Snap(), nil)
	c.Assert(err, IsNil)
	seccompSpec := seccomp.NewSpecification(appSet)
	err = seccompSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.browser-support-plug-snap.app2"})
	secCompSnippet := seccompSpec.SnippetForTag("snap.browser-support-plug-snap.app2")
	c.Assert(secCompSnippet, testutil.Contains, `# Description: Can access various APIs needed by modern browsers`)
	c.Assert(secCompSnippet, testutil.Contains, `chroot`)
}

func (s *BrowserSupportInterfaceSuite) TestConnectedPlugSnippetWithUserNS(c *C) {
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
	plug := interfaces.NewConnectedPlug(info.Plugs["browser-support"], nil, nil)

	restore := apparmor_sandbox.MockFeatures(nil, nil, []string{"userns"}, nil)
	defer restore()
	appSet, err := interfaces.NewSnapAppSet(plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.browser-support-plug-snap.app2"})
	snippet := apparmorSpec.SnippetForTag("snap.browser-support-plug-snap.app2")
	c.Assert(snippet, testutil.Contains, `# Description: Can access various APIs needed by modern browsers`)
	c.Assert(snippet, testutil.Contains, `ptrace (trace) peer=snap.@{SNAP_INSTANCE_NAME}.**`)
	c.Assert(snippet, testutil.Contains, `userns,`)
}

func (s *BrowserSupportInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 1)

	// connected plugs have a non-nil security snippet for apparmor
	appSet, err = interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	seccompSpec := seccomp.NewSpecification(appSet)
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.Snippets(), HasLen, 1)
}

func (s *BrowserSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
