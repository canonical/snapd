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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type RawInputInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&RawInputInterfaceSuite{
	iface: builtin.MustInterface("raw-input"),
})

const rawInputConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [raw-input]
`

const rawInputCoreYaml = `name: core
version: 0
type: os
slots:
  raw-input:
`

func (s *RawInputInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, rawInputConsumerYaml, nil, "raw-input")
	s.slot, s.slotInfo = MockConnectedSlot(c, rawInputCoreYaml, nil, "raw-input")
}

func (s *RawInputInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "raw-input")
}

func (s *RawInputInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *RawInputInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *RawInputInterfaceSuite) TestSecCompSpec(c *C) {
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := seccomp.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "bind\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "socket AF_NETLINK - NETLINK_KOBJECT_UEVENT\n")
}

func (s *RawInputInterfaceSuite) TestAppArmorSpec(c *C) {
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/input/* rw,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/sys/devices/**/input[0-9]*/capabilities/* r,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/run/udev/data/c13:[0-9]* r,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/run/udev/data/+input:input[0-9]* r,`)
}

func (s *RawInputInterfaceSuite) TestUDevSpec(c *C) {
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := udev.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 5)
	c.Assert(spec.Snippets(), testutil.Contains, `# raw-input
KERNEL=="event[0-9]*", SUBSYSTEM=="input", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# raw-input
KERNEL=="mice", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# raw-input
KERNEL=="mouse[0-9]*", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# raw-input
KERNEL=="ts[0-9]*", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
	c.Assert(spec.TriggeredSubsystems(), DeepEquals, []string{"input"})
}

func (s *RawInputInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to raw input devices`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "raw-input")
}

func (s *RawInputInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *RawInputInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
