// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type BluetoothControlInterfaceSuite struct {
	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

const btcontrolMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [bluetooth-control]
`

var _ = Suite(&BluetoothControlInterfaceSuite{
	iface: builtin.MustInterface("bluetooth-control"),
})

func (s *BluetoothControlInterfaceSuite) SetUpTest(c *C) {
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
		Name:      "bluetooth-control",
		Interface: "bluetooth-control",
		Apps: map[string]*snap.AppInfo{
			"app1": {
				Snap: &snap.Info{
					SuggestedName: "core",
				},
				Name: "app1"}},
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil)
	plugSnap := snaptest.MockInfo(c, btcontrolMockPlugSnapInfoYaml, nil)
	s.plugInfo = plugSnap.Plugs["bluetooth-control"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil)
}

func (s *BluetoothControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "bluetooth-control")
}

func (s *BluetoothControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "bluetooth-control",
		Interface: "bluetooth-control",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		"bluetooth-control slots are reserved for the core snap")
}

func (s *BluetoothControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *BluetoothControlInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "capability net_admin")
}

func (s *BluetoothControlInterfaceSuite) TestSecCompSpec(c *C) {
	spec := &seccomp.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "\nbind\n")
}

func (s *BluetoothControlInterfaceSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets(), testutil.Contains, `# bluetooth-control
SUBSYSTEM=="bluetooth", TAG+="snap_other_app2"`)
	c.Assert(spec.Snippets(), testutil.Contains, `TAG=="snap_other_app2", RUN+="/usr/lib/snapd/snap-device-helper $env{ACTION} snap_other_app2 $devpath $major:$minor"`)
}

func (s *BluetoothControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
