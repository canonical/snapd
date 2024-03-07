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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type PackageKitControlInterfaceSuite struct {
	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

var _ = Suite(&PackageKitControlInterfaceSuite{
	iface: builtin.MustInterface("packagekit-control"),
})

func (s *PackageKitControlInterfaceSuite) SetUpTest(c *C) {
	const coreYaml = `name: core
version: 0
type: os
slots:
 packagekit-control:
  interface: packagekit-control
`
	s.slot, s.slotInfo = MockConnectedSlot(c, coreYaml, nil, "packagekit-control")

	var consumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [packagekit-control]
`
	s.plug, s.plugInfo = MockConnectedPlug(c, consumerYaml, nil, "packagekit-control")
}

func (s *PackageKitControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "packagekit-control")
}

func (s *PackageKitControlInterfaceSuite) TestSanitize(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *PackageKitControlInterfaceSuite) TestAppArmorConnectedPlug(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 1)
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.PackageKit`)
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.PackageKit.Offline`)
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.PackageKit.Transaction`)
}

func (s *PackageKitControlInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.ImplicitOnCore, Equals, false)
	c.Check(si.ImplicitOnClassic, Equals, true)
	c.Check(si.Summary, Equals, "allows control of the PackageKit service")
	c.Check(si.BaseDeclarationPlugs, testutil.Contains, "packagekit-control")
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "packagekit-control")
}

func (s *PackageKitControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
