// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type inputInterfaceSuite struct {
	iface           interfaces.Interface
	coreSlotInfo    *snap.SlotInfo
	coreSlot        *interfaces.ConnectedSlot
	classicSlotInfo *snap.SlotInfo
	classicSlot     *interfaces.ConnectedSlot
	plugInfo        *snap.PlugInfo
	plug            *interfaces.ConnectedPlug
}

var _ = Suite(&inputInterfaceSuite{
	iface: builtin.MustInterface("input"),
})

const inputMockPlugSnapInfoYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [input]
`

// an input slot on an input snap (as installed on a core/all-snap system)
const inputCoreYaml = `name: input
version: 0
apps:
 app1:
  slots: [input]
`

// an input slot on the core snap (as automatically added on classic)
const inputClassicYaml = `name: core
version: 0
type: os
slots:
 input:
  interface: input
`

func (s *inputInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, inputMockPlugSnapInfoYaml, nil, "input")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, inputCoreYaml, nil, "input")
	s.classicSlot, s.classicSlotInfo = MockConnectedSlot(c, inputClassicYaml, nil, "input")
}

func (s *inputInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "input")
}

func (s *inputInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.classicSlotInfo), IsNil)
}

func (s *inputInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *inputInterfaceSuite) TestAppArmorSpec(c *C) {
	// on a core system with input slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	// connected plug to core slot
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "network netlink raw")
}

func (s *inputInterfaceSuite) TestAppArmorSpecOnClassic(c *C) {
	// on a classic system with input slot coming from the core snap.
	restore := release.MockOnClassic(true)
	defer restore()

	// connected plug to classic slot
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "network netlink raw,")

	// connected classic slot to plug
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent classic slot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.classicSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *inputInterfaceSuite) TestSecCompOnClassic(c *C) {
	// on a classic system with input slot coming from the core snap.
	restore := release.MockOnClassic(true)
	defer restore()

	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddPermanentSlot(s.iface, s.classicSlotInfo)
	c.Assert(err, IsNil)
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.classicSlot)
	c.Assert(err, IsNil)

	// app snap has additional seccomp rules
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(seccompSpec.SnippetForTag("snap.consumer.app"), testutil.Contains, "bind\n")
}

func (s *inputInterfaceSuite) TestSecCompOnCore(c *C) {
	// on a core system with input slot coming from a snap.
	restore := release.MockOnClassic(false)
	defer restore()

	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddPermanentSlot(s.iface, s.coreSlotInfo)
	c.Assert(err, IsNil)
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)

	c.Assert(seccompSpec.SnippetForTag("snap.consumer.app"), testutil.Contains, "bind\n")
}

func (s *inputInterfaceSuite) TestUDev(c *C) {
	for _, onClassic := range []bool{true, false} {
		restore := release.MockOnClassic(onClassic)
		defer restore()

		spec := &udev.Specification{}
		c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
		c.Assert(spec.Snippets(), HasLen, 6)
		c.Assert(spec.Snippets(), testutil.Contains, `# input
KERNEL=="event[0-9]*", TAG+="snap_consumer_app"`)
		c.Assert(spec.Snippets(), testutil.Contains, `# input
KERNEL=="mice", TAG+="snap_consumer_app"`)
		c.Assert(spec.Snippets(), testutil.Contains, `# input
KERNEL=="mouse[0-9]*", TAG+="snap_consumer_app"`)
		c.Assert(spec.Snippets(), testutil.Contains, `# input
KERNEL=="ts[0-9]*", TAG+="snap_consumer_app"`)
		c.Assert(spec.Snippets(), testutil.Contains, `# input
KERNEL=="tty[0-9]*", TAG+="snap_consumer_app"`)
		c.Assert(spec.Snippets(), testutil.Contains, `TAG=="snap_consumer_app", RUN+="/usr/lib/snapd/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`)
		c.Assert(spec.TriggeredSubsystems(), DeepEquals, []string{"input"})
	}
}

func (s *inputInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows input from keyboard/mouse devices`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "input")
}

func (s *inputInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.coreSlotInfo), Equals, true)
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.classicSlotInfo), Equals, true)
}

func (s *inputInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
