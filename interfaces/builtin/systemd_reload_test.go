// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

type systemdReloadInterfaceSuite struct {
	iface        interfaces.Interface
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

var _ = Suite(&systemdReloadInterfaceSuite{
	iface: builtin.MustInterface("systemd-reload"),
})

const systemdReloadConsumerYaml = `name: consumer
version: 0
apps:
 app:
  command: foo
  plugs: [systemd-reload]
`

const systemdReloadCoreYaml = `name: core
version: 0
type: os
slots:
  systemd-reload:
`

func (s *systemdReloadInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, systemdReloadConsumerYaml, nil, "systemd-reload")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, systemdReloadCoreYaml, nil, "systemd-reload")
}

func (s *systemdReloadInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "systemd-reload")
}

func (s *systemdReloadInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
}

func (s *systemdReloadInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *systemdReloadInterfaceSuite) TestConnectedPlugSnippet(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), testutil.Contains, `org.freedesktop.systemd1`)
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), testutil.Contains, `Reload`)
}

func (s *systemdReloadInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
