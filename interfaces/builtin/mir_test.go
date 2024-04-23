// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type MirInterfaceSuite struct {
	iface           interfaces.Interface
	coreSlotInfo    *snap.SlotInfo
	coreSlot        *interfaces.ConnectedSlot
	classicSlotInfo *snap.SlotInfo
	classicSlot     *interfaces.ConnectedSlot
	plugInfo        *snap.PlugInfo
	plug            *interfaces.ConnectedPlug
}

var _ = Suite(&MirInterfaceSuite{
	iface: builtin.MustInterface("mir"),
})

func (s *MirInterfaceSuite) SetUpTest(c *C) {
	// a pulseaudio slot on the core snap (as automatically added on classic)
	const mirMockClassicSlotSnapInfoYaml = `name: core
version: 0
type: os
slots:
 mir:
  interface: mir
`
	const mirMockSlotSnapInfoYaml = `name: mir-server
version: 1.0
slots:
 mir:
  interface: mir
apps:
 mir:
  command: foo
  slots: [mir]
`
	const mockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [mir]
`
	// mir snap with mir-server slot on an core/all-snap install.
	snapInfo := snaptest.MockInfo(c, mirMockSlotSnapInfoYaml, nil)
	s.coreSlotInfo = snapInfo.Slots["mir"]
	s.coreSlot = interfaces.NewConnectedSlot(s.coreSlotInfo, nil, nil)
	// mir slot on a core snap in a classic install.
	snapInfo = snaptest.MockInfo(c, mirMockClassicSlotSnapInfoYaml, nil)
	s.classicSlotInfo = snapInfo.Slots["mir"]
	s.classicSlot = interfaces.NewConnectedSlot(s.classicSlotInfo, nil, nil)
	// snap with the mir plug
	snapInfo = snaptest.MockInfo(c, mockPlugSnapInfoYaml, nil)
	s.plugInfo = snapInfo.Plugs["mir"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *MirInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "mir")
}

func (s *MirInterfaceSuite) TestUsedSecuritySystems(c *C) {
	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.coreSlotInfo.Snap))
	err := apparmorSpec.AddPermanentSlot(s.iface, s.coreSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.mir-server.mir"})
	c.Assert(apparmorSpec.SnippetForTag("snap.mir-server.mir"), testutil.Contains, "capability sys_tty_config")

	apparmorSpec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.classicSlotInfo.Snap))
	err = apparmorSpec.AddPermanentSlot(s.iface, s.classicSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 0)

	apparmorSpec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.coreSlot.Snap()))
	err = apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.mir-server.mir"})
	c.Assert(apparmorSpec.SnippetForTag("snap.mir-server.mir"), testutil.Contains, "unix (receive, send) type=seqpacket addr=none peer=(label=\"snap.other")

	apparmorSpec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, "/run/mir_socket rw,")
}

func (s *MirInterfaceSuite) TestSecComp(c *C) {
	seccompSpec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.coreSlotInfo.Snap))
	err := seccompSpec.AddPermanentSlot(s.iface, s.coreSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.mir-server.mir"})
	c.Check(seccompSpec.SnippetForTag("snap.mir-server.mir"), testutil.Contains, "listen\n")
}

func (s *MirInterfaceSuite) TestSecCompOnClassic(c *C) {
	seccompSpec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.classicSlotInfo.Snap))
	err := seccompSpec.AddPermanentSlot(s.iface, s.classicSlotInfo)
	c.Assert(err, IsNil)
	snippets := seccompSpec.Snippets()
	// no permanent seccomp snippet for the slot
	c.Assert(len(snippets), Equals, 0)
}

func (s *MirInterfaceSuite) TestUDevSpec(c *C) {
	udevSpec := udev.NewSpecification(interfaces.NewSnapAppSet(s.coreSlotInfo.Snap))
	c.Assert(udevSpec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(udevSpec.Snippets(), HasLen, 6)
	c.Assert(udevSpec.Snippets(), testutil.Contains, `# mir
KERNEL=="tty[0-9]*", TAG+="snap_mir-server_mir"`)
	c.Assert(udevSpec.Snippets(), testutil.Contains, `# mir
KERNEL=="mice", TAG+="snap_mir-server_mir"`)
	c.Assert(udevSpec.Snippets(), testutil.Contains, `# mir
KERNEL=="mouse[0-9]*", TAG+="snap_mir-server_mir"`)
	c.Assert(udevSpec.Snippets(), testutil.Contains, `# mir
KERNEL=="event[0-9]*", TAG+="snap_mir-server_mir"`)
	c.Assert(udevSpec.Snippets(), testutil.Contains, `# mir
KERNEL=="ts[0-9]*", TAG+="snap_mir-server_mir"`)
	c.Assert(udevSpec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_mir-server_mir", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_mir-server_mir $devpath $major:$minor"`, dirs.DistroLibExecDir))
	c.Assert(udevSpec.TriggeredSubsystems(), DeepEquals, []string{"input"})
}

func (s *MirInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
