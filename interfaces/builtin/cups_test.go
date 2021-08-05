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
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type cupsSuite struct {
	iface            interfaces.Interface
	plugInfo         *snap.PlugInfo
	plug             *interfaces.ConnectedPlug
	providerSlotInfo *snap.SlotInfo
	providerSlot     *interfaces.ConnectedSlot
}

var _ = Suite(&cupsSuite{iface: builtin.MustInterface("cups")})

const cupsConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [cups]
`

const cupsProviderYaml = `name: provider
version: 0
apps:
 app:
  slots: [cups]
`

func (s *cupsSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, cupsConsumerYaml, nil, "cups")
	s.providerSlot, s.providerSlotInfo = MockConnectedSlot(c, cupsProviderYaml, nil, "cups")
}

func (s *cupsSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "cups")
}

func (s *cupsSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.providerSlotInfo), IsNil)
}

func (s *cupsSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *cupsSuite) TestAppArmorSpec(c *C) {
	for _, onClassic := range []bool{true, false} {
		restore := release.MockOnClassic(onClassic)
		defer restore()

		// consumer to provider on core for ConnectedPlug
		spec := &apparmor.Specification{}
		c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.providerSlot), IsNil)
		c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
		c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Allow communicating with the cups server")
		c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "#include <abstractions/cups-client>")
		restore()
	}
}

func (s *cupsSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.Summary, Equals, `allows access to the CUPS socket for printing`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "cups")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-connection: true")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *cupsSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
