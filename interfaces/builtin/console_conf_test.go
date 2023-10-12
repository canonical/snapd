// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

type ConsoleConfInterfaceSuite struct {
	iface        interfaces.Interface
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

var _ = Suite(&ConsoleConfInterfaceSuite{
	iface: builtin.MustInterface("console-conf"),
})

const consoleConfConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [console-conf]
`

const consoleConfCoreYaml = `name: core
version: 0
type: os
slots:
  console-conf:
`

func (s *ConsoleConfInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, consoleConfConsumerYaml, nil, "console-conf")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, consoleConfCoreYaml, nil, "console-conf")
}

func (s *ConsoleConfInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "console-conf")
}

func (s *ConsoleConfInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
}

func (s *ConsoleConfInterfaceSuite) TestAppArmorConnectedSlot(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *ConsoleConfInterfaceSuite) TestAppArmorPermanentSlot(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *ConsoleConfInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *ConsoleConfInterfaceSuite) TestAppArmorPermanentPlug(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *ConsoleConfInterfaceSuite) TestAppArmorConnectedPlug(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `capability dac_read_search,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `capability dac_override,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/{,var/}run/console_conf/ rw,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/{,var/}run/console_conf/** rw,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/var/log/console-conf/ rw,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/var/log/console-conf/* rw,`)
}

func (s *ConsoleConfInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows console-conf capability`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *ConsoleConfInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
