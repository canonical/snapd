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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type TpmInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&TpmInterfaceSuite{
	iface: builtin.MustInterface("tpm"),
})

const tpmConsumerYaml = `name: consumer
apps:
 app:
  plugs: [tpm]
`

const tpmCoreYaml = `name: core
type: os
slots:
  tpm:
`

func (s *TpmInterfaceSuite) SetUpTest(c *C) {
	s.plug = MockPlug(c, tpmConsumerYaml, nil, "tpm")
	s.slot = MockSlot(c, tpmCoreYaml, nil, "tpm")
}

func (s *TpmInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "tpm")
}

func (s *TpmInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		PlugSlotData: snap.PlugSlotData{
			Snap:      &snap.Info{SuggestedName: "some-snap"},
			Name:      "tpm",
			Interface: "tpm",
		}}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"tpm slots are reserved for the core snap")
}

func (s *TpmInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *TpmInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/tpm0")
}

func (s *TpmInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to the Trusted Platform Module device`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "tpm")
}

func (s *TpmInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plug, s.slot), Equals, true)
}

func (s *TpmInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
