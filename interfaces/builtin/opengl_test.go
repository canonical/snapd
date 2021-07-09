// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

type OpenglInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&OpenglInterfaceSuite{
	iface: builtin.MustInterface("opengl"),
})

const openglConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [opengl]
`

const openglCoreYaml = `name: core
version: 0
type: os
slots:
  opengl:
`

func (s *OpenglInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, openglConsumerYaml, nil, "opengl")
	s.slot, s.slotInfo = MockConnectedSlot(c, openglCoreYaml, nil, "opengl")
}

func (s *OpenglInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "opengl")
}

func (s *OpenglInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *OpenglInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *OpenglInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/nvidia* rw,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/dri/renderD[0-9]* rw,`)
}

func (s *OpenglInterfaceSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 10)
	c.Assert(spec.Snippets(), testutil.Contains, `# opengl
SUBSYSTEM=="drm", KERNEL=="card[0-9]*", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# opengl
KERNEL=="renderD[0-9]*", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# opengl
KERNEL=="nvhost-*", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# opengl
KERNEL=="nvmap", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# opengl
KERNEL=="tegra_dc_ctrl", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# opengl
KERNEL=="tegra_dc_[0-9]*", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# opengl
KERNEL=="pvr_sync", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_consumer_app", RUN+="%v/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *OpenglInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to OpenGL stack`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "opengl")
}

func (s *OpenglInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *OpenglInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
