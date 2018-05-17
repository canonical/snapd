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
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type OpticalDriveInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot

	// Consuming Snap
	testPlugReadonly     *interfaces.ConnectedPlug
	testPlugReadonlyInfo *snap.PlugInfo
	testPlugWritable     *interfaces.ConnectedPlug
	testPlugWritableInfo *snap.PlugInfo
	testPlugDefault      *interfaces.ConnectedPlug
	testPlugDefaultInfo  *snap.PlugInfo
}

var _ = Suite(&OpticalDriveInterfaceSuite{
	iface: builtin.MustInterface("optical-drive"),
})

const opticalDriveConsumerYaml = `name: consumer
version: 0
plugs:
 plug-for-readonly:
  interface: optical-drive
  write: false
 plug-for-writable:
  interface: optical-drive
  write: true
apps:
 app:
  plugs: [optical-drive]
 app-readonly:
  plugs: [plug-for-readonly]
 app-writable:
  plugs: [plug-for-writable]
`

const opticalDriveCoreYaml = `name: core
version: 0
type: os
slots:
  optical-drive:
`

func (s *OpticalDriveInterfaceSuite) SetUpTest(c *C) {
	// s.plug, s.plugInfo = MockConnectedPlug(c, opticalDriveConsumerYaml, nil, "optical-drive")
	consumingSnapInfo := snaptest.MockInfo(c, opticalDriveConsumerYaml, nil)

	s.testPlugDefaultInfo = consumingSnapInfo.Plugs["optical-drive"]
	s.testPlugDefault = interfaces.NewConnectedPlug(s.testPlugDefaultInfo, nil)
	s.testPlugReadonlyInfo = consumingSnapInfo.Plugs["plug-for-readonly"]
	s.testPlugReadonly = interfaces.NewConnectedPlug(s.testPlugReadonlyInfo, nil)
	s.testPlugWritableInfo = consumingSnapInfo.Plugs["plug-for-writable"]
	s.testPlugWritable = interfaces.NewConnectedPlug(s.testPlugWritableInfo, nil)

	s.slot, s.slotInfo = MockConnectedSlot(c, opticalDriveCoreYaml, nil, "optical-drive")
}

func (s *OpticalDriveInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "optical-drive")
}

func (s *OpticalDriveInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "optical-drive",
		Interface: "optical-drive",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		"optical-drive slots are reserved for the core snap")
}

func (s *OpticalDriveInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.testPlugDefaultInfo), IsNil)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.testPlugReadonlyInfo), IsNil)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.testPlugWritableInfo), IsNil)
}

func (s *OpticalDriveInterfaceSuite) TestAppArmorSpec(c *C) {
	// spec := &apparmor.Specification{}
	// c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	// c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	// c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/sr[0-9]* r,`)

	checkConnectedPlugSnippet := func(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot, appName string, expectedSnippet string) {
		apparmorSpec := &apparmor.Specification{}
		err := apparmorSpec.AddConnectedPlug(s.iface, plug, slot)
		c.Assert(err, IsNil)

		c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{appName})
		snippet := apparmorSpec.SnippetForTag(appName)
		c.Assert(snippet, DeepEquals, expectedSnippet)
	}

	baseSnippet := "/run/udev/data/b11:[0-9]* r,\n" +
		"@{PROC}/sys/dev/cdrom/info r,"
	expectedSnippet1 := "/dev/scd[0-9]* r,\n" +
		"/dev/sr[0-9]* r,\n" +
		baseSnippet
	expectedSnippet2 := "/dev/scd[0-9]* rw,\n" +
		"/dev/sr[0-9]* rw,\n" +
		baseSnippet
	checkConnectedPlugSnippet(s.testPlugDefault, s.slot, "snap.consumer.app", expectedSnippet1)
	checkConnectedPlugSnippet(s.testPlugReadonly, s.slot, "snap.consumer.app-readonly", expectedSnippet1)
	checkConnectedPlugSnippet(s.testPlugWritable, s.slot, "snap.consumer.app-writable", expectedSnippet2)
}

func (s *OpticalDriveInterfaceSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugDefault, s.slot), IsNil)
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugReadonly, s.slot), IsNil)
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugWritable, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 9) // three rules multiplied by three apps
	c.Assert(spec.Snippets(), testutil.Contains, `# optical-drive
KERNEL=="sr[0-9]*", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `TAG=="snap_consumer_app", RUN+="/usr/lib/snapd/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`)
}

func (s *OpticalDriveInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows read and write access to optical drives`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "optical-drive")
}

func (s *OpticalDriveInterfaceSuite) TestAutoConnect(c *C) {
	// FIXME: fix AutoConnect methods to use ConnectedPlug/Slot
	c.Assert(s.iface.AutoConnect(&interfaces.Plug{PlugInfo: s.testPlugDefaultInfo}, &interfaces.Slot{SlotInfo: s.slotInfo}), Equals, true)
	c.Assert(s.iface.AutoConnect(&interfaces.Plug{PlugInfo: s.testPlugReadonlyInfo}, &interfaces.Slot{SlotInfo: s.slotInfo}), Equals, true)
	c.Assert(s.iface.AutoConnect(&interfaces.Plug{PlugInfo: s.testPlugWritableInfo}, &interfaces.Slot{SlotInfo: s.slotInfo}), Equals, false)
}

func (s *OpticalDriveInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
