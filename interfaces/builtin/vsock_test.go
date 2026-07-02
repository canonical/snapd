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

type vsockInterfaceSuite struct {
	testutil.BaseTest

	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&vsockInterfaceSuite{
	iface: builtin.MustInterface("vsock"),
})

const vsockConsumerYaml = `name: consumer
version: 0
apps:
  app:
    plugs: [vsock]
`

const vsockCoreYaml = `name: core
version: 0
type: os
slots:
  vsock:
`

func (s *vsockInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.plug, s.plugInfo = MockConnectedPlug(c, vsockConsumerYaml, nil, "vsock")
	s.slot, s.slotInfo = MockConnectedSlot(c, vsockCoreYaml, nil, "vsock")
}

func (s *vsockInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "vsock")
}

func (s *vsockInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *vsockInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *vsockInterfaceSuite) TestAppArmorSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "network vsock,")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/vsock rw,")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/vmci rw,")
}

func (s *vsockInterfaceSuite) TestUDevSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := udev.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), testutil.Contains, "# vsock\nKERNEL==\"vsock\", TAG+=\"snap_consumer_app\"")
	c.Assert(spec.Snippets(), testutil.Contains, "# vsock\nKERNEL==\"vmci\", TAG+=\"snap_consumer_app\"")
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%s/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *vsockInterfaceSuite) TestSecCompSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := seccomp.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "bind\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "listen\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "accept\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "accept4\n")
}

func (s *vsockInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to vsock sockets for VM/container host communication`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "vsock")
	c.Assert(si.BaseDeclarationPlugs, testutil.Contains, "allow-installation: false")
	c.Assert(si.BaseDeclarationPlugs, testutil.Contains, "deny-auto-connection: true")
}

func (s *vsockInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *vsockInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
