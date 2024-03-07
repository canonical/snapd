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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/osutil"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type NetworkControlInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&NetworkControlInterfaceSuite{
	iface: builtin.MustInterface("network-control"),
})

const networkControlConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [network-control]
`

const networkControlCoreYaml = `name: core
version: 0
type: os
slots:
  network-control:
`

func (s *NetworkControlInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, networkControlConsumerYaml, nil, "network-control")
	s.slot, s.slotInfo = MockConnectedSlot(c, networkControlCoreYaml, nil, "network-control")
}

func (s *NetworkControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "network-control")
}

func (s *NetworkControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *NetworkControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *NetworkControlInterfaceSuite) TestAppArmorSpec(c *C) {
	r := apparmor_sandbox.MockFeatures(nil, nil, nil, nil)
	defer r()

	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.SuppressSysModuleCapability(), Equals, true)
	c.Check(spec.UsesSysModuleCapability(), Equals, false)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/run/netns/* rw,\n")
	// No "xdp" feature is available, so this rule should not be added
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), "network xdp,")
	c.Assert(spec.UpdateNS(), DeepEquals, []string{`
/var/ r,
/var/lib/ r,
/var/lib/snapd/ r,
/var/lib/snapd/hostfs/ r,
/var/lib/snapd/hostfs/var/ r,
/var/lib/snapd/hostfs/var/lib/ r,
/var/lib/snapd/hostfs/var/lib/dhcp/ r,
/var/lib/dhcp/ r,
mount options=(rw bind) /var/lib/snapd/hostfs/var/lib/dhcp/ -> /var/lib/dhcp/,
umount /var/lib/dhcp/,
`})
}

func (s *NetworkControlInterfaceSuite) TestAppArmorSpecWithNoAppArmor(c *C) {
	r := apparmor_sandbox.MockLevel(apparmor_sandbox.Unsupported)
	defer r()

	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	// Check a rule that should always be there...
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/run/netns/* rw,\n")
	// Since no sandbox is there, this should also not be there
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), "network xdp,\n")
}

func (s *NetworkControlInterfaceSuite) TestAppArmorSpecWithXdpFeature(c *C) {
	r := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer r()
	r = apparmor_sandbox.MockFeatures(nil, nil, []string{"feat1", "xdp", "feat2"}, nil)
	defer r()

	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	// Check a rule that should always be there...
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/run/netns/* rw,\n")
	// ...and one which requires XDP support in AppArmor
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "network xdp,\n")
}

func (s *NetworkControlInterfaceSuite) TestSecCompSpec(c *C) {
	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "setns - CLONE_NEWNET\n")
}

func (s *NetworkControlInterfaceSuite) TestUDevSpec(c *C) {
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 3)
	c.Assert(spec.Snippets(), testutil.Contains, `# network-control
KERNEL=="tun", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *NetworkControlInterfaceSuite) TestMountSpec(c *C) {
	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.MountEntries(), HasLen, 1)
	c.Assert(spec.MountEntries(), DeepEquals, []osutil.MountEntry{{
		Name:    "/var/lib/snapd/hostfs/var/lib/dhcp",
		Dir:     "/var/lib/dhcp",
		Options: []string{"bind", "rw", "x-snapd.ignore-missing"},
	}})
}

func (s *NetworkControlInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows configuring networking and network namespaces`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "network-control")
	c.Assert(si.AffectsPlugOnRefresh, Equals, true)
}

func (s *NetworkControlInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}
func (s *NetworkControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
