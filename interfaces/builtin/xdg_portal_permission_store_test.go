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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type XdgPortalPermissionStoreInterfaceSuite struct {
	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

var _ = Suite(&XdgPortalPermissionStoreInterfaceSuite{
	iface: builtin.MustInterface("xdg-portal-permission-store"),
})

func (s *XdgPortalPermissionStoreInterfaceSuite) SetUpTest(c *C) {
	const coreYaml = `name: core
version: 0
type: os
slots:
  xdg-portal-permission-store:
    interface: xdg-portal-permission-store
`
	s.slot, s.slotInfo = MockConnectedSlot(c, coreYaml, nil, "xdg-portal-permission-store")

	const consumerYaml = `name: consumer
version: 0
apps:
  app:
    plugs: [xdg-portal-permission-store]
`
	s.plug, s.plugInfo = MockConnectedPlug(c, consumerYaml, nil, "xdg-portal-permission-store")
}

func (s *XdgPortalPermissionStoreInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "xdg-portal-permission-store")
}

func (s *XdgPortalPermissionStoreInterfaceSuite) TestSanitize(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *XdgPortalPermissionStoreInterfaceSuite) TestAppArmorConnectedPlug(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "#include <abstractions/dbus-session-strict>")
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "path=/org/freedesktop/impl/portal/PermissionStore")
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "interface=org.freedesktop.impl.portal.PermissionStore")
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "interface=org.freedesktop.DBus.Properties")
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "interface=org.freedesktop.DBus.Peer")
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "interface=org.freedesktop.DBus.Introspectable")
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(label=unconfined)")
}

func (s *XdgPortalPermissionStoreInterfaceSuite) TestAppArmorConnectedSlot(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.slot.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *XdgPortalPermissionStoreInterfaceSuite) TestAppArmorPermanentSlot(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *XdgPortalPermissionStoreInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.ImplicitOnCore, Equals, true)
	c.Check(si.ImplicitOnClassic, Equals, true)
	c.Check(si.Summary, Equals, "allows access to the XDG Desktop Portal PermissionStore service")
	c.Check(si.BaseDeclarationPlugs, testutil.Contains, "xdg-portal-permission-store")
	c.Check(si.BaseDeclarationPlugs, testutil.Contains, "allow-installation: false")
	c.Check(si.BaseDeclarationPlugs, testutil.Contains, "deny-auto-connection: true")
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "xdg-portal-permission-store")
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *XdgPortalPermissionStoreInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
