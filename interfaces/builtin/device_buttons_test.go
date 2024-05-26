// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type DeviceButtonsInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&DeviceButtonsInterfaceSuite{
	iface: builtin.MustInterface("device-buttons"),
})

const gpioKeysConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [device-buttons]
`

const gpioKeysCoreYaml = `name: core
version: 0
type: os
slots:
  device-buttons:
`

func (s *DeviceButtonsInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, gpioKeysConsumerYaml, nil, "device-buttons")
	s.slot, s.slotInfo = MockConnectedSlot(c, gpioKeysCoreYaml, nil, "device-buttons")
}

func (s *DeviceButtonsInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "device-buttons")
}

func (s *DeviceButtonsInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *DeviceButtonsInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *DeviceButtonsInterfaceSuite) TestAppArmorSpec(c *C) {
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/input/event[0-9]* rw,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/sys/devices/**/input[0-9]*/capabilities/* r,`)
}

func (s *DeviceButtonsInterfaceSuite) TestUDevSpec(c *C) {
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := udev.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 3)
	c.Assert(spec.Snippets(), testutil.Contains, `# device-buttons
KERNEL=="event[0-9]*", SUBSYSTEM=="input", ENV{ID_INPUT_KEY}=="1", ENV{ID_INPUT_KEYBOARD}!="1", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# device-buttons
KERNEL=="full", SUBSYSTEM=="mem", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
	c.Assert(spec.TriggeredSubsystems(), DeepEquals, []string{"input/key"})
}

func (s *DeviceButtonsInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to device buttons as input events`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "device-buttons")
}

func (s *DeviceButtonsInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *DeviceButtonsInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
