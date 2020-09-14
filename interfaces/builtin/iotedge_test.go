// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"github.com/snapcore/snapd/testutil"
)

type IotedgeInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const iotedgeMockPlugSnapInfoYaml = `name: iotedge
version: 1.0
apps:
 app:
  command: foo
  plugs: [iotedge]
`

const iotedgeCoreYaml = `name: core
version: 0
type: os
slots:
  iotedge:
`

var _ = Suite(&IotedgeInterfaceSuite{
	iface: builtin.MustInterface("iotedge"),
})

func (s *IotedgeInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, iotedgeMockPlugSnapInfoYaml, nil, "iotedge")
	s.slot, s.slotInfo = MockConnectedSlot(c, iotedgeCoreYaml, nil, "iotedge")
}

func (s *IotedgeInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "iotedge")
}

func (s *IotedgeInterfaceSuite) TestConnectedPlugSnippet(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.iotedge.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.iotedge.app"), testutil.Contains, `run/iotedge/mgmt.sock`)
	c.Assert(apparmorSpec.SnippetForTag("snap.iotedge.app"), testutil.Contains, `run/iotedge/workload.sock`)
	c.Assert(apparmorSpec.SnippetForTag("snap.iotedge.app"), testutil.Contains, `@{PROC}/[0-9]*/environ`)
	c.Assert(apparmorSpec.SnippetForTag("snap.iotedge.app"), testutil.Contains, "ptrace")
	c.Assert(apparmorSpec.SnippetForTag("snap.iotedge.app"), testutil.Contains, "capability sys_ptrace")
	c.Assert(apparmorSpec.SnippetForTag("snap.iotedge.app"), testutil.Contains, "capability dac_read_search")
}

func (s *IotedgeInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *IotedgeInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *IotedgeInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *IotedgeInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(nil, nil), Equals, true)
}
