// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/seccomp"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

const plugSnapInfoYaml = `name: consumer
version: 0

apps:
  app:
    command: foo
    plugs: [userns]
`

const userNSCoreYaml = `name: core
version: 0
type: os
slots:
  userns:
`

type UserNSInterfaceSuite struct {
	iface interfaces.Interface

	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&UserNSInterfaceSuite{
	iface: builtin.MustInterface("userns"),
})

func (s *UserNSInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, plugSnapInfoYaml, nil, "userns")
	s.slot, s.slotInfo = MockConnectedSlot(c, userNSCoreYaml, nil, "userns")
}

func (s *UserNSInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "userns")
}

func (s *UserNSInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *UserNSInterfaceSuite) TestNoAppArmor(c *C) {
	// Ensure that the interface does not fail if AppArmor is unsupported
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Unsupported)
	defer restore()

	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *UserNSInterfaceSuite) TestFeatureDetection(c *C) {
	// Ensure that the interface does not fail if the userns feature is not present
	restore := apparmor_sandbox.MockFeatures(nil, nil, nil, nil)
	defer restore()
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *UserNSInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	restore := apparmor_sandbox.MockFeatures(nil, nil, []string{"userns"}, nil)
	defer restore()
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "userns,\n")
}

func (s *UserNSInterfaceSuite) TestSeccompSpec(c *C) {
	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "unshare\n")
}

func (s *UserNSInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *UserNSInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(nil, nil), Equals, true)
}

func (s *UserNSInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.ImplicitOnCore, Equals, true)
	c.Check(si.ImplicitOnClassic, Equals, true)
	c.Check(si.Summary, Equals, `allows the ability to use user namespaces`)
}
