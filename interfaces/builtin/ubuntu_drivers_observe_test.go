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
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type UbuntuDriversObserveInterfaceSuite struct {
	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

var _ = Suite(&UbuntuDriversObserveInterfaceSuite{
	iface: builtin.MustInterface("ubuntu-drivers-observe"),
})

func (s *UbuntuDriversObserveInterfaceSuite) SetUpTest(c *C) {
	const coreYaml = `name: core
version: 0
type: os
slots:
  ubuntu-drivers-observe:
    interface: ubuntu-drivers-observe
`
	s.slot, s.slotInfo = MockConnectedSlot(c, coreYaml, nil, "ubuntu-drivers-observe")

	const consumerYaml = `name: consumer
version: 0
apps:
  app:
    plugs: [ubuntu-drivers-observe]
`
	s.plug, s.plugInfo = MockConnectedPlug(c, consumerYaml, nil, "ubuntu-drivers-observe")
}

func (s *UbuntuDriversObserveInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "ubuntu-drivers-observe")
}

func (s *UbuntuDriversObserveInterfaceSuite) TestSanitize(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *UbuntuDriversObserveInterfaceSuite) TestAppArmorConnectedPlug(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})

	snippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(snippet, testutil.Contains, "#include <abstractions/dbus-strict>")
	c.Check(strings.Count(snippet, "path=/com/ubuntu/Drivers"), Equals, 4)
	c.Check(strings.Count(snippet, "interface=com.ubuntu.Drivers"), Equals, 2)
	c.Check(strings.Count(snippet, "member=drivers"), Equals, 2)
	c.Check(strings.Count(snippet, "interface=org.freedesktop.DBus.Introspectable"), Equals, 2)
	c.Check(strings.Count(snippet, "member=Introspect"), Equals, 2)
	c.Check(strings.Count(snippet, "peer=(name=com.ubuntu.Drivers)"), Equals, 2)
	c.Check(strings.Count(snippet, "peer=(label=unconfined)"), Equals, 2)
	c.Check(snippet, Not(testutil.Contains), "interface=com.ubuntu.*")
	c.Check(snippet, Not(testutil.Contains), "path=/com/ubuntu/**")
	c.Check(snippet, Not(testutil.Contains), "org.freedesktop.DBus.Properties")
}

func (s *UbuntuDriversObserveInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.ImplicitOnCore, Equals, false)
	c.Check(si.ImplicitOnClassic, Equals, true)
	c.Check(si.Summary, Equals, "allows querying the Ubuntu drivers service")
	c.Check(si.BaseDeclarationPlugs, Equals, "")
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "ubuntu-drivers-observe")
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "slot-snap-type:\n        - core")
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *UbuntuDriversObserveInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *UbuntuDriversObserveInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
