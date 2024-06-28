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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
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
const btcontrolMockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 bluetooth-control:
  interface: bluetooth-control
apps:
 app1:
`

var _ = Suite(&BluetoothControlInterfaceSuite{
	iface: builtin.MustInterface("bluetooth-control"),
})

func (s *BluetoothControlInterfaceSuite) SetUpTest(c *C) {
	s.slot, s.slotInfo = MockConnectedSlot(c, btcontrolMockSlotSnapInfoYaml, nil, "bluetooth-control")
	s.plug, s.plugInfo = MockConnectedPlug(c, btcontrolMockPlugSnapInfoYaml, nil, "bluetooth-control")
}

func (s *BluetoothControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "bluetooth-control")
}

func (s *BluetoothControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *BluetoothControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *BluetoothControlInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := apparmor.NewSpecification(s.plug.AppSet())
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "capability net_admin")
}

func (s *BluetoothControlInterfaceSuite) TestSecCompSpec(c *C) {
	spec := seccomp.NewSpecification(s.plug.AppSet())
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "\nbind\n")
}

func (s *BluetoothControlInterfaceSuite) TestUDevSpec(c *C) {
	spec := udev.NewSpecification(s.plug.AppSet())
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 3)
	c.Assert(spec.Snippets(), testutil.Contains, `# bluetooth-control
SUBSYSTEM=="bluetooth", TAG+="snap_other_app2"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# bluetooth-control
SUBSYSTEM=="BT_chrdev", TAG+="snap_other_app2"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_other_app2", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_other_app2 $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *BluetoothControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
