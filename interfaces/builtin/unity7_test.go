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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type Unity7InterfaceSuite struct {
	iface        interfaces.Interface
	slotInfo     *snap.SlotInfo
	slot         *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
	plugInstInfo *snap.PlugInfo
	plugInst     *interfaces.ConnectedPlug
}

var _ = Suite(&Unity7InterfaceSuite{
	iface: builtin.MustInterface("unity7"),
})

const unity7mockPlugSnapInfoYaml = `name: other-snap
version: 1.0
apps:
 app2:
  command: foo
  plugs: [unity7]
`

func (s *Unity7InterfaceSuite) SetUpTest(c *C) {
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", SnapType: snap.TypeOS},
		Name:      "unity7",
		Interface: "unity7",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
	plugSnap := snaptest.MockInfo(c, unity7mockPlugSnapInfoYaml, nil)
	s.plugInfo = plugSnap.Plugs["unity7"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)

	plugSnapInst := snaptest.MockInfo(c, unity7mockPlugSnapInfoYaml, nil)
	plugSnapInst.InstanceKey = "instance"
	s.plugInstInfo = plugSnapInst.Plugs["unity7"]
	s.plugInst = interfaces.NewConnectedPlug(s.plugInstInfo, nil, nil)
}

func (s *Unity7InterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "unity7")
}

func (s *Unity7InterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *Unity7InterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *Unity7InterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	apparmorSpec := apparmor.NewSpecification(appSet)
	mylog.Check(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot))

	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other-snap.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other-snap.app2"), testutil.Contains, `/usr/share/pixmaps`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other-snap.app2"), testutil.Contains, `path=/com/canonical/indicator/messages/other_snap_*_desktop`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other-snap.app2"), testutil.Contains, `deny /var/lib/snapd/desktop/applications/mimeinfo.cache r,`)

	// getDesktopFileRules() rules
	c.Assert(apparmorSpec.SnippetForTag("snap.other-snap.app2"), testutil.Contains, `# This leaks the names of snaps with desktop files`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other-snap.app2"), testutil.Contains, `/var/lib/snapd/desktop/applications/ r,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other-snap.app2"), testutil.Contains, `/var/lib/snapd/desktop/applications/@{SNAP_INSTANCE_DESKTOP}_*.desktop r,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other-snap.app2"), testutil.Contains, `deny /var/lib/snapd/desktop/applications/@{SNAP_INSTANCE_DESKTOP}[^_.]*.desktop r,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other-snap.app2"), testutil.Contains, `deny /var/lib/snapd/desktop/applications/[^o]* r,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other-snap.app2"), testutil.Contains, `deny /var/lib/snapd/desktop/applications/other-sna[^p]* r,`)

	// connected plugs for instance name have a non-nil security snippet for apparmor
	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.plugInst.Snap(), nil))

	apparmorSpec = apparmor.NewSpecification(appSet)
	mylog.Check(apparmorSpec.AddConnectedPlug(s.iface, s.plugInst, s.slot))

	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other-snap_instance.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other-snap_instance.app2"), testutil.Contains, `/usr/share/pixmaps`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other-snap_instance.app2"), testutil.Contains, `path=/com/canonical/indicator/messages/other_snap_instance_*_desktop`)

	// connected plugs for instance name have a non-nil security snippet for apparmor
	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.plugInst.Snap(), nil))

	apparmorSpec = apparmor.NewSpecification(appSet)
	mylog.Check(apparmorSpec.AddConnectedPlug(s.iface, s.plugInst, s.slot))

	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other-snap_instance.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other-snap_instance.app2"), testutil.Contains, `/usr/share/pixmaps`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other-snap_instance.app2"), testutil.Contains, `path=/com/canonical/indicator/messages/other_snap_instance_*_desktop`)

	// connected plugs have a non-nil security snippet for seccomp
	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	seccompSpec := seccomp.NewSpecification(appSet)
	mylog.Check(seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot))

	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.other-snap.app2"})
	c.Check(seccompSpec.SnippetForTag("snap.other-snap.app2"), testutil.Contains, "bind\n")
}

func (s *Unity7InterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
