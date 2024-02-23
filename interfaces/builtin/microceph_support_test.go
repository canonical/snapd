// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

type MicrocephSupportInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&MicrocephSupportInterfaceSuite{
	iface: builtin.MustInterface("microceph-support"),
})

const microcephSupportConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [microceph-support]
`

const microcephSupportCoreYaml = `name: core
version: 0
type: os
slots:
  microceph-support:
`

func (s *MicrocephSupportInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, microcephSupportConsumerYaml, nil, "microceph-support")
	s.slot, s.slotInfo = MockConnectedSlot(c, microcephSupportCoreYaml, nil, "microceph-support")
}

func (s *MicrocephSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "microceph-support")
}

func (s *MicrocephSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *MicrocephSupportInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *MicrocephSupportInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/sys/bus/rbd/add_single_major rwk,                         # add single major dev\n")
}

func (s *MicrocephSupportInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows operating as the MicroCeph service`)
	c.Assert(si.BaseDeclarationPlugs, testutil.Contains, "microceph-support")
}

func (s *MicrocephSupportInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, true)
}

func (s *MicrocephSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
