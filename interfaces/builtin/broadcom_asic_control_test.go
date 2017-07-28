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
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type BroadcomAsicControlSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&BroadcomAsicControlSuite{
	iface: builtin.MustInterface("broadcom-asic-control"),
})

const broadcomCtlMockPlugSnapInfo = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [broadcom-asic-control]
`

func (s *BroadcomAsicControlSuite) SetUpTest(c *C) {
	s.slot = &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
			Name:      "broadcom-asic-control",
			Interface: "broadcom-asic-control",
			Apps: map[string]*snap.AppInfo{
				"app1": {
					Snap: &snap.Info{
						SuggestedName: "core",
					},
					Name: "app1"}},
		},
	}

	plugSnap := snaptest.MockInfo(c, broadcomCtlMockPlugSnapInfo, nil)
	s.plug = &interfaces.Plug{PlugInfo: plugSnap.Plugs["broadcom-asic-control"]}
}

func (s *BroadcomAsicControlSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "broadcom-asic-control")
}

func (s *BroadcomAsicControlSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "broadcom-asic-control",
		Interface: "broadcom-asic-control",
	}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"broadcom-asic-control slots are reserved for the core snap")
}

func (s *BroadcomAsicControlSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *BroadcomAsicControlSuite) TestUsedSecuritySystems(c *C) {
	expectedAppArmorLine := "/sys/module/linux_kernel_bde/initstate r,"
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, expectedAppArmorLine)
	// .. and for udev
	expectedUDevLine := "KERNEL==\"linux-user-bde\", TAG+=\"snap_other_app2\"\n"
	udevSpec := &udev.Specification{}
	err = udevSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	snippets := udevSpec.Snippets()
	c.Assert(len(snippets), Equals, 1)
	c.Assert(snippets[0], testutil.Contains, expectedUDevLine)
	// .. and for kmod
	expectedModules := map[string]bool{
		"linux-user-bde":   true,
		"linux-kernel-bde": true,
		"linux-bcm-knet":   true,
	}
	kmodSpec := &kmod.Specification{}
	err = kmodSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	modules := kmodSpec.Modules()
	c.Assert(modules, DeepEquals, expectedModules)
}

func (s *BroadcomAsicControlSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
