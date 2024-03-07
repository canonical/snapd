// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type AllegroVcuInterfaceSuite struct {
	iface        interfaces.Interface
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

var _ = Suite(&AllegroVcuInterfaceSuite{
	iface: builtin.MustInterface("allegro-vcu"),
})

const allegroVcuConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [allegro-vcu]
`

const allegroVcuCoreYaml = `name: core
version: 0
type: os
slots:
  allegro-vcu:
`

func (s *AllegroVcuInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, allegroVcuConsumerYaml, nil, "allegro-vcu")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, allegroVcuCoreYaml, nil, "allegro-vcu")
}

func (s *AllegroVcuInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "allegro-vcu")
}

func (s *AllegroVcuInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
}

func (s *AllegroVcuInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *AllegroVcuInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/allegroDecodeIP rw,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/allegroIP rw,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/dmaproxy rw,`)
}

func (s *AllegroVcuInterfaceSuite) TestUDevSpec(c *C) {
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 4)
	c.Assert(spec.Snippets(), testutil.Contains, `# allegro-vcu
SUBSYSTEM=="allegro_decode_class", KERNEL=="allegroDecodeIP", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# allegro-vcu
SUBSYSTEM=="allegro_encode_class", KERNEL=="allegroIP", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# allegro-vcu
SUBSYSTEM=="char", KERNEL=="dmaproxy", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(
		`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *AllegroVcuInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to Allegro Video Code Unit`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *AllegroVcuInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
