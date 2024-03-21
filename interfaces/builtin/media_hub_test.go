// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type MediaHubInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&MediaHubInterfaceSuite{
	iface: builtin.MustInterface("media-hub"),
})

func (s *MediaHubInterfaceSuite) SetUpTest(c *C) {
	var mockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [media-hub]
`
	const mockSlotSnapInfoYaml = `name: media-hub
version: 1.0
slots:
 media-hub:
  interface: media-hub
apps:
 app:
  command: foo
  slots: [media-hub]
`
	s.slot, s.slotInfo = MockConnectedSlot(c, mockSlotSnapInfoYaml, nil, "media-hub")
	s.plug, s.plugInfo = MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, "media-hub")
}

func (s *MediaHubInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "media-hub")
}

// The label glob when all apps are bound to the media-hub slot
func (s *MediaHubInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	appSet := appSetWithApps(c, "media-hub", "app1", "app2")
	si := appSet.Info()
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap:      si,
		Name:      "media-hub",
		Interface: "media-hub",
		Apps:      map[string]*snap.AppInfo{"app1": si.Apps["app1"], "app2": si.Apps["app2"]},
	}, appSet, nil, nil)

	release.OnClassic = false

	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains,
		`peer=(label="snap.media-hub.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the media-hub slot
func (s *MediaHubInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	appSet := appSetWithApps(c, "media-hub", "app1", "app2", "app3")
	si := appSet.Info()
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap:      si,
		Name:      "media-hub",
		Interface: "media-hub",
		Apps:      map[string]*snap.AppInfo{"app1": si.Apps["app1"], "app2": si.Apps["app2"]},
	}, appSet, nil, nil)

	release.OnClassic = false

	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains,
		`peer=(label="snap.media-hub{.app1,.app2}"),`)
}

// The label uses short form when exactly one app is bound to the media-hub slot
func (s *MediaHubInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	appSet := appSetWithApps(c, "media-hub", "app")
	si := appSet.Info()
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap:      si,
		Name:      "media-hub",
		Interface: "media-hub",
		Apps:      map[string]*snap.AppInfo{"app": si.Apps["app"]},
	}, appSet, nil, nil)

	release.OnClassic = false

	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains,
		`peer=(label="snap.media-hub.app"),`)
}

func (s *MediaHubInterfaceSuite) TestConnectedPlugSnippetAppArmor(c *C) {
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), Not(IsNil))
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains,
		`#include <abstractions/dbus-session-strict>`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains,
		`peer=(label="snap.media-hub.app"),`)
}

func (s *MediaHubInterfaceSuite) TestPermanentSlotSnippetAppArmor(c *C) {
	apparmorSpec := apparmor.NewSpecification(s.slot.AppSet())
	err := apparmorSpec.AddPermanentSlot(s.iface, s.slotInfo)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.media-hub.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.media-hub.app"), Not(IsNil))
	c.Assert(apparmorSpec.SnippetForTag("snap.media-hub.app"), testutil.Contains,
		`#include <abstractions/dbus-session-strict>`)
	c.Assert(apparmorSpec.SnippetForTag("snap.media-hub.app"), testutil.Contains,
		`peer=(label=unconfined),`)
}

func (s *MediaHubInterfaceSuite) TestConnectedSlotSnippetAppArmor(c *C) {
	apparmorSpec := apparmor.NewSpecification(s.slot.AppSet())
	err := apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.media-hub.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.media-hub.app"), Not(IsNil))
	c.Assert(apparmorSpec.SnippetForTag("snap.media-hub.app"), Not(testutil.Contains),
		`peer=(label=unconfined),`)
}

func (s *MediaHubInterfaceSuite) TestPermanentSlotSnippetSecComp(c *C) {
	spec := seccomp.NewSpecification(s.slot.AppSet())
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(spec.SnippetForTag("snap.media-hub.app"), testutil.Contains, "bind\n")
}

func (s *MediaHubInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
