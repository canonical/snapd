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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type MirInterfaceSuite struct {
	iface       interfaces.Interface
	coreSlot    *interfaces.Slot
	classicSlot *interfaces.Slot
	plug        *interfaces.Plug
}

var _ = Suite(&MirInterfaceSuite{})

func (s *MirInterfaceSuite) SetUpTest(c *C) {
	// a pulseaudio slot on the core snap (as automatically added on classic)
	const mirMockClassicSlotSnapInfoYaml = `name: core
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
	s.iface = &builtin.MirInterface{}
	// mir snap with mir-server slot on an core/all-snap install.
	snapInfo := snaptest.MockInfo(c, mirMockSlotSnapInfoYaml, nil)
	s.coreSlot = &interfaces.Slot{SlotInfo: snapInfo.Slots["mir"]}
	// mir slot on a core snap in a classic install.
	snapInfo = snaptest.MockInfo(c, mirMockClassicSlotSnapInfoYaml, nil)
	s.classicSlot = &interfaces.Slot{SlotInfo: snapInfo.Slots["mir"]}
	// snap with the mir plug
	snapInfo = snaptest.MockInfo(c, mockPlugSnapInfoYaml, nil)
	s.plug = &interfaces.Plug{PlugInfo: snapInfo.Plugs["mir"]}
}

func (s *MirInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "mir")
}

func (s *MirInterfaceSuite) TestUsedSecuritySystems(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddPermanentSlot(s.iface, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.mir-server.mir"})
	c.Assert(apparmorSpec.SnippetForTag("snap.mir-server.mir"), testutil.Contains, "capability sys_tty_config")

	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddPermanentSlot(s.iface, s.classicSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 0)

	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.mir-server.mir"})
	c.Assert(apparmorSpec.SnippetForTag("snap.mir-server.mir"), testutil.Contains, "unix (receive, send) type=seqpacket addr=none peer=(label=\"snap.other")

	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, "/run/mir_socket rw,")
}

const mirMockSlotSnapInfoYaml = `name: mir-server
version: 1.0
slots:
 mir-server:
  interface: mir
apps:
 mir:
  command: foo
  slots:
   - mir-server
`

func (s *MirInterfaceSuite) TestSecComp(c *C) {
	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddPermanentSlot(s.iface, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.mir-server.mir"})
	c.Check(seccompSpec.SnippetForTag("snap.mir-server.mir"), testutil.Contains, "listen\n")
}

func (s *MirInterfaceSuite) TestSecCompOnClassic(c *C) {
	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddPermanentSlot(s.iface, s.classicSlot)
	c.Assert(err, IsNil)
	snippets := seccompSpec.Snippets()
	// no permanent seccomp snippet for the slot
	c.Assert(len(snippets), Equals, 0)
}
