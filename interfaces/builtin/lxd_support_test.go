// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

type LxdSupportInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&LxdSupportInterfaceSuite{
	iface: builtin.MustInterface("lxd-support"),
})

const lxdSupportConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [lxd-support]
`

const lxdSupportCoreYaml = `name: core
version: 0
type: os
slots:
  lxd-support:
`

func (s *LxdSupportInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, lxdSupportConsumerYaml, nil, "lxd-support")
	s.slot, s.slotInfo = MockConnectedSlot(c, lxdSupportCoreYaml, nil, "lxd-support")
}

func (s *LxdSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "lxd-support")
}

func (s *LxdSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *LxdSupportInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *LxdSupportInterfaceSuite) TestSanitizePlugInvalid(c *C) {
	const lxdSupportInvalidConsumerYaml = `name: consumer
version: 0
plugs:
  lxd-support-invalid-attr:
    interface: lxd-support
    enable-unconfined-mode: 1

apps:
 app:
  plugs:
    - lxd-support-invalid-attr
`

	_, plugInfo := MockConnectedPlug(c, lxdSupportInvalidConsumerYaml, nil, "lxd-support-invalid-attr")
	c.Assert(interfaces.BeforePreparePlug(s.iface, plugInfo), ErrorMatches, "lxd-support plug requires bool with 'enable-unconfined-mode'")
}

func (s *LxdSupportInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/{,usr/}{,s}bin/aa-exec ux,\n")
}

func (s *LxdSupportInterfaceSuite) TestAppArmorSpecUserNS(c *C) {
	r := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer r()
	r = apparmor_sandbox.MockFeatures(nil, nil, []string{"userns"}, nil)
	defer r()
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "userns,\n")
}

func (s *LxdSupportInterfaceSuite) TestAppArmorSpecUnconfined(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plugInfo.Snap))
	c.Assert(spec.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(spec.Unconfined(), Equals, apparmor.UnconfinedSupported)

	// Unconfined mode is enabled by the plug when it enables it via the
	// enable-unconfined-mode attribute
	const lxdSupportWithUnconfinedModeConsumerYaml = `name: consumer
version: 0
plugs:
  lxd-support-with-unconfined-mode:
    interface: lxd-support
    enable-unconfined-mode: true
apps:
 app:
  plugs: [lxd-support-with-unconfined-mode]
`

	plug, _ := MockConnectedPlug(c, lxdSupportWithUnconfinedModeConsumerYaml, nil, "lxd-support-with-unconfined-mode")

	c.Assert(spec.AddConnectedPlug(s.iface, plug, s.slot), IsNil)
	c.Assert(spec.Unconfined(), Equals, apparmor.UnconfinedEnabled)
}

func (s *LxdSupportInterfaceSuite) TestSecCompSpec(c *C) {
	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "@unrestricted\n")
}

func (s *LxdSupportInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows operating as the LXD service`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "lxd-support")
	c.Assert(si.BaseDeclarationPlugs, testutil.Contains, "lxd-support")
}

func (s *LxdSupportInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, true)
}

func (s *LxdSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *LxdSupportInterfaceSuite) TestServicePermanentPlugSnippets(c *C) {
	snips, err := interfaces.PermanentPlugServiceSnippets(s.iface, s.plugInfo)
	c.Assert(err, IsNil)
	c.Check(snips, DeepEquals, []string{"Delegate=true"})
}
