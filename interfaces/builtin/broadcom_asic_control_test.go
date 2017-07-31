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

const app1Yaml = `name: core
type: os
version: 1.0
apps:
 app1:
   command: bar
   slots: [broadcom-asic-control]`

const app2Yaml = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [broadcom-asic-control]
`

func (s *BroadcomAsicControlSuite) SetUpTest(c *C) {
	slotSnap := snaptest.MockInfo(c, app1Yaml, nil)
	s.slot = &interfaces.Slot{SlotInfo: slotSnap.Slots["broadcom-asic-control"]}

	plugSnap := snaptest.MockInfo(c, app2Yaml, nil)
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

func (s *BroadcomAsicControlSuite) TestAppArmorSecuritySystem(c *C) {
	expectedAppArmorLine := "/sys/module/linux_kernel_bde/{,**} r,"
	// connected plugs have a non-nil security snippet for apparmor
	spec := &apparmor.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(spec.SnippetForTag("snap.other.app2"), testutil.Contains, expectedAppArmorLine)
}

func (s *BroadcomAsicControlSuite) TestUDevSecuritySystem(c *C) {
	// .. and for udev
	expectedUDevLine := "KERNEL==\"linux-user-bde\", TAG+=\"snap_other_app2\"\n"
	spec := &udev.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	snippets := spec.Snippets()
	c.Assert(len(snippets), Equals, 1)
	c.Assert(snippets[0], testutil.Contains, expectedUDevLine)
}

func (s *BroadcomAsicControlSuite) TestKModSecuritySystem(c *C) {
	// .. and for kmod
	expectedModules := map[string]bool{
		"linux-user-bde":   true,
		"linux-kernel-bde": true,
		"linux-bcm-knet":   true,
	}
	spec := &kmod.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	modules := spec.Modules()
	c.Assert(modules, DeepEquals, expectedModules)
}

func (s *BroadcomAsicControlSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
