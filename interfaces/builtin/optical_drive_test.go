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
	"github.com/snapcore/snapd/testutil"
)

type OpticalDriveInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&OpticalDriveInterfaceSuite{
	iface: builtin.MustInterface("optical-drive"),
})

const opticalDriveConsumerYaml = `name: consumer
apps:
 app:
  plugs: [optical-drive]
`

const opticalDriveCoreYaml = `name: core
type: os
slots:
  optical-drive:
`

func (s *OpticalDriveInterfaceSuite) SetUpTest(c *C) {
	s.plug = MockPlug(c, opticalDriveConsumerYaml, nil, "optical-drive")
	s.slot = MockSlot(c, opticalDriveCoreYaml, nil, "optical-drive")
}

func (s *OpticalDriveInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "optical-drive")
}

func (s *OpticalDriveInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "optical-drive",
		Interface: "optical-drive",
	}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"optical-drive slots are reserved for the core snap")
}

func (s *OpticalDriveInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *OpticalDriveInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/sr[0-9]* r,`)
}

func (s *OpticalDriveInterfaceSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.Snippets(), HasLen, 3)
	c.Assert(spec.Snippets(), testutil.Contains, `# optical-drive
KERNEL=="sr[0-9]*", TAG+="snap_consumer_app"`)
}

func (s *OpticalDriveInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows read access to optical drives`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "optical-drive")
}

func (s *OpticalDriveInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plug, s.slot), Equals, true)
}

func (s *OpticalDriveInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
