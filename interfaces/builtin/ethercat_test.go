// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type EtherCATInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&EtherCATInterfaceSuite{
	iface: builtin.MustInterface("ethercat"),
})

const ethercatConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [ethercat]
 other:
`

const ethercatCoreYaml = `name: core
version: 0
type: os
slots:
 ethercat:
`

func (s *EtherCATInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, ethercatConsumerYaml, nil, "ethercat")
	s.slot, s.slotInfo = MockConnectedSlot(c, ethercatCoreYaml, nil, "ethercat")
}

func (s *EtherCATInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "ethercat")
}

func (s *EtherCATInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *EtherCATInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *EtherCATInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.Summary, Equals, "allows access to the IgH EtherCAT master")
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.BaseDeclarationPlugs, Equals, "")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "ethercat:")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *EtherCATInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := apparmor.NewSpecification(s.plug.AppSet())
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Equals, `
# Description: Allows access to the IgH EtherCAT master.
/dev/EtherCAT[0-9]* rw,
`)
	c.Assert(spec.SnippetForTag("snap.consumer.other"), Equals, "")
}

func (s *EtherCATInterfaceSuite) TestUDevSpec(c *C) {
	spec := udev.NewSpecification(s.plug.AppSet())
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.ControlsDeviceCgroup(), Equals, false)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets(), testutil.Contains, `# ethercat
SUBSYSTEM=="EtherCAT", KERNEL=="EtherCAT[0-9]*", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *EtherCATInterfaceSuite) TestSecCompSpec(c *C) {
	spec := seccomp.NewSpecification(s.plug.AppSet())
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *EtherCATInterfaceSuite) TestPlugAlias(c *C) {
	const consumerYaml = `name: consumer
version: 0
plugs:
 ethercat-master:
  interface: ethercat
apps:
 app:
  plugs: [ethercat-master]
`
	plug, plugInfo := MockConnectedPlug(c, consumerYaml, nil, "ethercat-master")
	c.Assert(interfaces.BeforePreparePlug(s.iface, plugInfo), IsNil)

	apparmorSpec := apparmor.NewSpecification(plug.AppSet())
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, plug, s.slot), IsNil)
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/EtherCAT[0-9]* rw,")

	udevSpec := udev.NewSpecification(plug.AppSet())
	c.Assert(udevSpec.AddConnectedPlug(s.iface, plug, s.slot), IsNil)
	c.Assert(udevSpec.Snippets(), testutil.Contains, `# ethercat
SUBSYSTEM=="EtherCAT", KERNEL=="EtherCAT[0-9]*", TAG+="snap_consumer_app"`)
}

func (s *EtherCATInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *EtherCATInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
