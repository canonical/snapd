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

type MediacontrolInterfaceSuite struct {
	iface        interfaces.Interface
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

var _ = Suite(&MediacontrolInterfaceSuite{
	iface: builtin.MustInterface("media-control"),
})

const mediacontrolConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [media-control]
`

const mediacontrolCoreYaml = `name: core
version: 0
type: os
slots:
  media-control:
`

func (s *MediacontrolInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, mediacontrolConsumerYaml, nil, "media-control")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, mediacontrolCoreYaml, nil, "media-control")
}

func (s *MediacontrolInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "media-control")
}

func (s *MediacontrolInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
}

func (s *MediacontrolInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *MediacontrolInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/media[0-9]* rwk,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/v4l-subdev[0-9]* rw,`)
}

func (s *MediacontrolInterfaceSuite) TestUDevSpec(c *C) {
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 3)
	c.Assert(spec.Snippets(), testutil.Contains, `# media-control
SUBSYSTEM=="media", KERNEL=="media[0-9]*", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# media-control
SUBSYSTEM=="video4linux", KERNEL=="v4l-subdev[0-9]*", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains,
		fmt.Sprintf(`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper snap_consumer_app"`, dirs.DistroLibExecDir))
}

func (s *MediacontrolInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to media control devices`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *MediacontrolInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
