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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type SoftwareWatchdogSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&SoftwareWatchdogSuite{
	iface: builtin.MustInterface("software-watchdog"),
})

const softwareWatchdogMockPlugSnapInfo = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [software-watchdog]
`

func (s *SoftwareWatchdogSuite) SetUpTest(c *C) {
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
		Name:      "software-watchdog",
		Interface: "software-watchdog",
		Apps: map[string]*snap.AppInfo{
			"app1": {
				Snap: &snap.Info{
					SuggestedName: "core",
				},
				Name: "app1"}},
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil)

	plugSnap := snaptest.MockInfo(c, softwareWatchdogMockPlugSnapInfo, nil)
	s.plugInfo = plugSnap.Plugs["software-watchdog"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil)
}

func (s *SoftwareWatchdogSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "software-watchdog")
}

func (s *SoftwareWatchdogSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	si := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "software-watchdog",
		Interface: "software-watchdog",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, si), ErrorMatches,
		"software-watchdog slots are reserved for the core snap")
}

func (s *SoftwareWatchdogSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *SoftwareWatchdogSuite) TestAppArmorNotifySocketDefault(c *C) {

	restore := builtin.MockOsGetenv(func(what string) string {
		c.Assert(what, Equals, "NOTIFY_SOCKET")
		return ""
	})
	defer restore()

	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, "\n/run/systemd/notify w,")
}

func (s *SoftwareWatchdogSuite) TestAppArmorNotifySocketEnv(c *C) {

	restore := builtin.MockOsGetenv(func(what string) string {
		c.Assert(what, Equals, "NOTIFY_SOCKET")
		return "/foo/bar"
	})
	defer restore()

	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, "\n/foo/bar w,")
}

func (s *SoftwareWatchdogSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
