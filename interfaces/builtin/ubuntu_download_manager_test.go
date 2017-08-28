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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type UbuntuDownloadManagerInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&UbuntuDownloadManagerInterfaceSuite{
	iface: builtin.MustInterface("ubuntu-download-manager"),
})

func (s *UbuntuDownloadManagerInterfaceSuite) SetUpTest(c *C) {
	var mockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [ubuntu-download-manager]
`
	snapInfo := snaptest.MockInfo(c, mockPlugSnapInfoYaml, nil)
	s.plug = &interfaces.Plug{PlugInfo: snapInfo.Plugs["ubuntu-download-manager"]}
	s.slot = &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			PlugSlotData: snap.PlugSlotData{
				Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
				Name:      "ubuntu-download-manager",
				Interface: "ubuntu-download-manager",
			},
		},
	}
}

func (s *UbuntuDownloadManagerInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "ubuntu-download-manager")
}

func (s *UbuntuDownloadManagerInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *UbuntuDownloadManagerInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		PlugSlotData: snap.PlugSlotData{
			Snap:      &snap.Info{SuggestedName: "some-snap"},
			Name:      "ubuntu-download-manager",
			Interface: "ubuntu-download-manager",
		}}}
	c.Assert(slot.Sanitize(s.iface), IsNil)
}

func (s *UbuntuDownloadManagerInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "path=/com/canonical/applications/download/**")
}

func (s *UbuntuDownloadManagerInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
