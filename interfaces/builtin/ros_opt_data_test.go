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
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type rosOptDataInterfaceSuite struct {
	testutil.BaseTest

	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&rosOptDataInterfaceSuite{
	iface: builtin.MustInterface("ros-opt-data"),
})

const rosOptDataConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [ros-opt-data]
`

const rosOptDataProducerYaml = `name: producer
version: 0
apps:
  app:
   slots: [ros-opt-data]
`

func (s *rosOptDataInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, rosOptDataConsumerYaml, nil, "ros-opt-data")
	s.slot, s.slotInfo = MockConnectedSlot(c, rosOptDataProducerYaml, nil, "ros-opt-data")
}

func (s *rosOptDataInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "ros-opt-data")
}

func (s *rosOptDataInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *rosOptDataInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *rosOptDataInterfaceSuite) TestAppArmorSpec(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `capability dac_read_search,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `# Description: Allows read-only access`)
}

func (s *rosOptDataInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, testutil.Contains, `Allows read-only access`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "ros-opt-data")
}

func (s *rosOptDataInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *rosOptDataInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
