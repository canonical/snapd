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

package builtin_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type adbSupportSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&adbSupportSuite{
	iface: builtin.MustInterface("adb-support"),
})

const adbConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [adb-support]
`

const adbCoreYaml = `name: provider
version: 0
apps:
 app:
  slots: [adb-support]
`

func (s *adbSupportSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, adbConsumerYaml, nil, "adb-support")
	s.slot, s.slotInfo = MockConnectedSlot(c, adbCoreYaml, nil, "adb-support")
}

func (s *adbSupportSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "adb-support")
}

func (s *adbSupportSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)

	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "adb-support",
		Interface: "adb-support",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), IsNil)
}

func (s *adbSupportSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *adbSupportSuite) TestSecCompSpec(c *C) {
	spec := &seccomp.Specification{}
	c.Assert(spec.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	spec = &seccomp.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	spec = &seccomp.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	spec = &seccomp.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *adbSupportSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	spec = &apparmor.Specification{}
	c.Assert(spec.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 1)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/bus/usb/[0-9][0-9][0-9]/[0-9][0-9][0-9] rw,")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/run/udev/data/c189:* r,")

	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *adbSupportSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)

	spec = &udev.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)

	spec = &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 82)
	c.Assert(spec.Snippets(), testutil.Contains, `# adb-support
SUBSYSTEM=="usb", ATTR{idVendor}=="0502", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# adb-support
SUBSYSTEM=="usb", ATTR{idVendor}=="19d2", TAG+="snap_consumer_app"`)

	// One-plus devices are included.
	// https://bugs.launchpad.net/snapd/+bug/1821474
	c.Assert(spec.Snippets(), testutil.Contains, `# adb-support
SUBSYSTEM=="usb", ATTR{idVendor}=="2a70", TAG+="snap_consumer_app"`)

	spec = &udev.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	c.Assert(spec.Snippets()[0], testutil.Contains, `SUBSYSTEM=="usb", ATTR{idVendor}=="0502", MODE="0666"`)
}

func (s *adbSupportSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows operating as Android Debug Bridge service`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "adb-support")
}

func (s *adbSupportSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
