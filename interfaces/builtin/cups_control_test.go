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

type cupsControlSuite struct {
	iface            interfaces.Interface
	coreSlotInfo     *snap.SlotInfo
	coreSlot         *interfaces.ConnectedSlot
	plugInfo         *snap.PlugInfo
	plug             *interfaces.ConnectedPlug
	providerSlotInfo *snap.SlotInfo
	providerSlot     *interfaces.ConnectedSlot
}

var _ = Suite(&cupsControlSuite{iface: builtin.MustInterface("cups-control")})

const cupsControlConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [cups-control]
`

const cupsControlCoreYaml = `name: core
version: 0
type: os
slots:
  cups-control:
`

const cupsControlProviderYaml = `name: provider
version: 0
apps:
 app:
  slots: [cups-control]
`

func (s *cupsControlSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, cupsControlConsumerYaml, nil, "cups-control")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, cupsControlCoreYaml, nil, "cups-control")
	s.providerSlot, s.providerSlotInfo = MockConnectedSlot(c, cupsControlProviderYaml, nil, "cups-control")
}

func (s *cupsControlSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "cups-control")
}

func (s *cupsControlSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
}

func (s *cupsControlSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *cupsControlSuite) TestAppArmorSpecCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// core to consumer on core is empty for ConnectedPlug
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// core to consumer on core is empty for PermanentSlot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// core to consumer on core is empty for ConnectedSlot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// consumer to provider on core for ConnectedPlug
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.providerSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Allow communicating with the cups server for printing and configuration.")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "#include <abstractions/cups-client>")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(name=org.freedesktop.DBus,label=\"snap.provider.app\"")
	c.Assert(spec.SnippetForTag("snap.provider.app"), Not(testutil.Contains), "# Allow daemon access to create the CUPS socket")

	// provider to consumer on core for PermanentSlot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.providerSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.provider.app"})
	c.Assert(spec.SnippetForTag("snap.provider.app"), testutil.Contains, "# Allow daemon access to create the CUPS socket")
	c.Assert(spec.SnippetForTag("snap.provider.app"), Not(testutil.Contains), "label=\"snap.consumer.app\"")

	// provider to consumer on core for ConnectedSlot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.providerSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.provider.app"})
	c.Assert(spec.SnippetForTag("snap.provider.app"), testutil.Contains, "peer=(name=org.freedesktop.DBus,label=\"snap.consumer.app\"")
}

func (s *cupsControlSuite) TestAppArmorSpecClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	// consumer to core on classic for ConnectedPlug
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Allow communicating with the cups server for printing and configuration.")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "#include <abstractions/cups-client>")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(name=org.freedesktop.DBus,label=\"{unconfined,/usr/sbin/cupsd,cupsd}\"")
	c.Assert(spec.SnippetForTag("snap.provider.app"), Not(testutil.Contains), "# Allow daemon access to create the CUPS socket")

	// core to consumer on classic is empty for PermanentSlot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// core to consumer on classic is empty for ConnectedSlot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// consumer to provider on classic for ConnectedPlug
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.providerSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Allow communicating with the cups server for printing and configuration.")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "#include <abstractions/cups-client>")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(name=org.freedesktop.DBus,label=\"snap.provider.app\"")
	c.Assert(spec.SnippetForTag("snap.provider.app"), Not(testutil.Contains), "# Allow daemon access to create the CUPS socket")

	// provider to consumer on classic for PermanentSlot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.providerSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.provider.app"})
	c.Assert(spec.SnippetForTag("snap.provider.app"), testutil.Contains, "# Allow daemon access to create the CUPS socket")
	c.Assert(spec.SnippetForTag("snap.provider.app"), Not(testutil.Contains), "label=\"snap.consumer.app\"")

	// provider to consumer on classic for ConnectedSlot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.providerSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.provider.app"})
	c.Assert(spec.SnippetForTag("snap.provider.app"), testutil.Contains, "peer=(name=org.freedesktop.DBus,label=\"snap.consumer.app\"")
}

func (s *cupsControlSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to the CUPS control socket`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "cups-control")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *cupsControlSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
