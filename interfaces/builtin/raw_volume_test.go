// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"fmt"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type rawVolumeInterfaceSuite struct {
	testutil.BaseTest
	iface interfaces.Interface

	// OS snap
	testSlot1Info *snap.SlotInfo
	testSlot2Info *snap.SlotInfo
	testSlot3Info *snap.SlotInfo

	// Gadget snap
	testUDev1     *interfaces.ConnectedSlot
	testUDev1Info *snap.SlotInfo
	testUDev2     *interfaces.ConnectedSlot
	testUDev2Info *snap.SlotInfo
	testUDev3     *interfaces.ConnectedSlot
	testUDev3Info *snap.SlotInfo

	testUDevBadValue1     *interfaces.ConnectedSlot
	testUDevBadValue1Info *snap.SlotInfo

	// Consuming snap
	testPlugPart1     *interfaces.ConnectedPlug
	testPlugPart1Info *snap.PlugInfo
	testPlugPart2     *interfaces.ConnectedPlug
	testPlugPart2Info *snap.PlugInfo
	testPlugPart3     *interfaces.ConnectedPlug
	testPlugPart3Info *snap.PlugInfo
}

var _ = Suite(&rawVolumeInterfaceSuite{
	iface: builtin.MustInterface("raw-volume"),
})

func (s *rawVolumeInterfaceSuite) SetUpTest(c *C) {
	// Mock for OS snap
	osSnapInfo := snaptest.MockInfo(c, `
name: core
version: 0
type: os
slots:
  test-part-1:
    interface: raw-volume
    path: /dev/vda1
  test-part-2:
    interface: raw-volume
    path: /dev/mmcblk0p1
  test-part-3:
    interface: raw-volume
    path: /dev/i2o/hda1
`, nil)
	s.testSlot1Info = osSnapInfo.Slots["test-part-1"]
	s.testSlot2Info = osSnapInfo.Slots["test-part-2"]
	s.testSlot3Info = osSnapInfo.Slots["test-part-3"]

	// Mock for Gadget snap
	gadgetSnapInfo := snaptest.MockInfo(c, `
name: some-device
version: 0
type: gadget
slots:
  test-udev-1:
    interface: raw-volume
    path: /dev/vda1
  test-udev-2:
    interface: raw-volume
    path: /dev/mmcblk0p1
  test-udev-3:
    interface: raw-volume
    path: /dev/i2o/hda1
  test-udev-bad-value-1:
    interface: raw-volume
    path: /dev/vda0
`, nil)
	s.testUDev1Info = gadgetSnapInfo.Slots["test-udev-1"]
	s.testUDev1 = interfaces.NewConnectedSlot(s.testUDev1Info, nil, nil)
	s.testUDev2Info = gadgetSnapInfo.Slots["test-udev-2"]
	s.testUDev2 = interfaces.NewConnectedSlot(s.testUDev2Info, nil, nil)
	s.testUDev3Info = gadgetSnapInfo.Slots["test-udev-3"]
	s.testUDev3 = interfaces.NewConnectedSlot(s.testUDev3Info, nil, nil)
	s.testUDevBadValue1Info = gadgetSnapInfo.Slots["test-udev-bad-value-1"]
	s.testUDevBadValue1 = interfaces.NewConnectedSlot(s.testUDevBadValue1Info, nil, nil)

	// Mock for consumer snaps
	consumingSnapInfo := snaptest.MockInfo(c, `
name: client-snap
version: 0
plugs:
  plug-for-part-1:
    interface: raw-volume
  plug-for-part-2:
    interface: raw-volume
  plug-for-part-3:
    interface: raw-volume
apps:
  app-accessing-1-part:
    command: foo
    plugs:
    - plug-for-part-1
  app-accessing-2-part:
    command: foo
    plugs:
    - plug-for-part-2
  app-accessing-3-part:
    command: foo
    plugs:
    - plug-for-part-3
`, nil)
	s.testPlugPart1Info = consumingSnapInfo.Plugs["plug-for-part-1"]
	s.testPlugPart1 = interfaces.NewConnectedPlug(s.testPlugPart1Info, nil, nil)
	s.testPlugPart2Info = consumingSnapInfo.Plugs["plug-for-part-2"]
	s.testPlugPart2 = interfaces.NewConnectedPlug(s.testPlugPart2Info, nil, nil)
	s.testPlugPart3Info = consumingSnapInfo.Plugs["plug-for-part-3"]
	s.testPlugPart3 = interfaces.NewConnectedPlug(s.testPlugPart3Info, nil, nil)
}

func (s *rawVolumeInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "raw-volume")
}

func (s *rawVolumeInterfaceSuite) TestSanitizeCoreSnapSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testSlot1Info), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testSlot2Info), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testSlot3Info), IsNil)
}

func (s *rawVolumeInterfaceSuite) TestSanitizeGadgetSnapSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDev1Info), IsNil)
}

func (s *rawVolumeInterfaceSuite) TestSanitizeBadGadgetSnapSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDevBadValue1Info), ErrorMatches, `slot "some-device:test-udev-bad-value-1" path attribute must be a valid device node`)
}

func (s *rawVolumeInterfaceSuite) TestSanitizeSlotHappy(c *C) {
	const mockSnapYaml = `name: raw-volume-slot-snap
type: gadget
version: 1.0
slots:
  raw-volume:
    path: $t
`

	var testCases = []struct {
		input string
	}{
		{`/dev/hda1`},
		{`/dev/hda63`},
		{`/dev/hdb42`},
		{`/dev/hdt63`},
		{`/dev/sda1`},
		{`/dev/sda15`},
		{`/dev/sdb8`},
		{`/dev/sdc14`},
		{`/dev/sdde10`},
		{`/dev/sdiv15`},
		{`/dev/i2o/hda1`},
		{`/dev/i2o/hda15`},
		{`/dev/i2o/hdb8`},
		{`/dev/i2o/hdc10`},
		{`/dev/i2o/hdde10`},
		{`/dev/i2o/hddx15`},
		{`/dev/mmcblk0p1`},
		{`/dev/mmcblk0p63`},
		{`/dev/mmcblk12p42`},
		{`/dev/mmcblk999p63`},
		{`/dev/nvme0p1`},
		{`/dev/nvme0p63`},
		{`/dev/nvme12p42`},
		{`/dev/nvme99p63`},
		{`/dev/nvme0n1p1`},
		{`/dev/nvme0n1p63`},
		{`/dev/nvme12n34p42`},
		{`/dev/nvme99n63p63`},
		{`/dev/vda1`},
		{`/dev/vda63`},
		{`/dev/vdb42`},
		{`/dev/vdz63`},
	}

	for _, t := range testCases {
		yml := strings.Replace(mockSnapYaml, "$t", t.input, -1)
		info := snaptest.MockInfo(c, yml, nil)
		slot := info.Slots["raw-volume"]

		c.Check(interfaces.BeforePrepareSlot(s.iface, slot), IsNil, Commentf("unexpected error for %q", t.input))
	}
}

func (s *rawVolumeInterfaceSuite) TestSanitizeSlotUnhappy(c *C) {
	const mockSnapYaml = `name: raw-volume-slot-snap
type: gadget
version: 1.0
slots:
  raw-volume:
    path: $t
`

	var testCases = []struct {
		input string
	}{
		{`/dev/hda0`},
		{`/dev/hdt64`},
		{`/dev/hdu1`},
		{`/dev/sda0`},
		{`/dev/sdiv16`},
		{`/dev/sdiw1`},
		{`/dev/i2o/hda0`},
		{`/dev/i20/hddx16`},
		{`/dev/i2o/hddy1`},
		{`/dev/mmcblk0p0`},
		{`/dev/mmcblk999p64`},
		{`/dev/mmcblk1000p1`},
		{`/dev/nvme0p0`},
		{`/dev/nvme99p64`},
		{`/dev/nvme100p1`},
		{`/dev/nvme0n0p1`},
		{`/dev/nvme99n64p1`},
		{`/dev/nvme100n1p1`},
		{`/dev/vda0`},
		{`/dev/vdz64`},
		{`/dev/vdaa1`},
	}

	for _, t := range testCases {
		yml := strings.Replace(mockSnapYaml, "$t", t.input, -1)
		info := snaptest.MockInfo(c, yml, nil)
		slot := info.Slots["raw-volume"]

		c.Check(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches, `slot "raw-volume-slot-snap:raw-volume" path attribute must be a valid device node`, Commentf("unexpected error for %q", t.input))
	}
}

func (s *rawVolumeInterfaceSuite) TestSanitizeSlotUnclean(c *C) {
	const mockSnapYaml = `name: raw-volume-slot-snap
type: gadget
version: 1.0
slots:
  raw-volume:
    path: $t
`

	var testCases = []struct {
		input string
	}{
		{`/dev/hda1/.`},
		{`/dev/i2o/`},
		{`/dev/./././mmcblk0p1////`},
	}

	for _, t := range testCases {
		yml := strings.Replace(mockSnapYaml, "$t", t.input, -1)
		info := snaptest.MockInfo(c, yml, nil)
		slot := info.Slots["raw-volume"]
		c.Check(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches, `cannot use slot "raw-volume-slot-snap:raw-volume" path ".*": try ".*"`, Commentf("unexpected error for %q", t.input))
	}
}

func (s *rawVolumeInterfaceSuite) TestUDevSpec(c *C) {
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.testPlugPart1.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugPart1, s.testUDev1), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets()[0], Equals, `# raw-volume
KERNEL=="vda1", TAG+="snap_client-snap_app-accessing-1-part"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-1-part", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper snap_client-snap_app-accessing-1-part"`, dirs.DistroLibExecDir))

	spec = udev.NewSpecification(interfaces.NewSnapAppSet(s.testPlugPart2.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugPart2, s.testUDev2), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets()[0], Equals, `# raw-volume
KERNEL=="mmcblk0p1", TAG+="snap_client-snap_app-accessing-2-part"`)

	spec = udev.NewSpecification(interfaces.NewSnapAppSet(s.testPlugPart3.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugPart3, s.testUDev3), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets()[0], Equals, `# raw-volume
KERNEL=="i2o/hda1", TAG+="snap_client-snap_app-accessing-3-part"`)
}

func (s *rawVolumeInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testPlugPart1.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugPart1, s.testUDev1), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.client-snap.app-accessing-1-part"})
	c.Assert(spec.SnippetForTag("snap.client-snap.app-accessing-1-part"), testutil.Contains, `/dev/vda1 rw,`)
	c.Assert(spec.SnippetForTag("snap.client-snap.app-accessing-1-part"), testutil.Contains, `capability sys_admin,`)

	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testPlugPart2.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugPart2, s.testUDev2), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.client-snap.app-accessing-2-part"})
	c.Assert(spec.SnippetForTag("snap.client-snap.app-accessing-2-part"), testutil.Contains, `/dev/mmcblk0p1 rw,`)
	c.Assert(spec.SnippetForTag("snap.client-snap.app-accessing-2-part"), testutil.Contains, `capability sys_admin,`)

	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.testPlugPart3.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugPart3, s.testUDev3), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.client-snap.app-accessing-3-part"})
	c.Assert(spec.SnippetForTag("snap.client-snap.app-accessing-3-part"), testutil.Contains, `/dev/i2o/hda1 rw,`)
	c.Assert(spec.SnippetForTag("snap.client-snap.app-accessing-3-part"), testutil.Contains, `capability sys_admin,`)
}

func (s *rawVolumeInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.Summary, Equals, `allows read/write access to specific disk partition`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "raw-volume")
}

func (s *rawVolumeInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, true)
}

func (s *rawVolumeInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
