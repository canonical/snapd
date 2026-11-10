// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
 *
 * This program is free software; you can redistribute it and/or modify
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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type dracutSupportInterfaceSuite struct {
	iface interfaces.Interface

	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&dracutSupportInterfaceSuite{
	iface: builtin.MustInterface("dracut-support"),
})

const dracutSupportPlugSnapYaml = `name: consumer
version: 0
apps:
  app:
    command: foo
    plugs: [dracut-support]
`

const dracutSupportCoreYaml = `name: core
version: 0
type: os
slots:
  dracut-support:
`

func (s *dracutSupportInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, dracutSupportPlugSnapYaml, nil, "dracut-support")
	s.slot, s.slotInfo = MockConnectedSlot(c, dracutSupportCoreYaml, nil, "dracut-support")
}

func (s *dracutSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "dracut-support")
}

func (s *dracutSupportInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *dracutSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *dracutSupportInterfaceSuite) TestAppArmorSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *dracutSupportInterfaceSuite) TestSecCompSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := seccomp.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	// no snippets are added, but the default setuid/setgid file creation
	// deny rules are suppressed
	c.Assert(spec.SecurityTags(), HasLen, 0)
	c.Assert(spec.SuppressSuidCreationDeny(), Equals, true)
}

func (s *dracutSupportInterfaceSuite) TestSecCompSpecUnconnected(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := seccomp.NewSpecification(appSet)
	c.Assert(spec.SuppressSuidCreationDeny(), Equals, false)
}

func (s *dracutSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *dracutSupportInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(nil, nil), Equals, true)
}

func (s *dracutSupportInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.ImplicitOnCore, Equals, true)
	c.Check(si.ImplicitOnClassic, Equals, true)
	c.Check(si.Summary, Equals, `allows creating files with setuid/setgid bits set (eg staging initrd images)`)
}

func (s *dracutSupportInterfaceSuite) TestBaseDeclaration(c *C) {
	// the interface is super-privileged: installation requires a store
	// assertion and connections are never automatic
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.BaseDeclarationPlugs, testutil.Contains, "allow-installation: false")
	c.Check(si.BaseDeclarationPlugs, testutil.Contains, "deny-auto-connection: true")
	c.Check(si.BaseDeclarationPlugs, testutil.Contains, "deny-connection: true")
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "deny-connection: true")
}
