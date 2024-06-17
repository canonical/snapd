// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type NetworkManagerInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const netmgrMockPlugSnapInfoYaml = `name: network-manager-client
version: 1.0
plugs:
 network-manager:
  interface: network-manager
apps:
 nmcli:
  command: foo
  plugs:
   - network-manager
`
const netmgrMockSlotSnapInfoYaml = `name: network-manager
version: 1.0
apps:
 nm:
  command: foo
  slots: [network-manager]
`

var _ = Suite(&NetworkManagerInterfaceSuite{
	iface: builtin.MustInterface("network-manager"),
})

func (s *NetworkManagerInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, netmgrMockPlugSnapInfoYaml, nil, "network-manager")
	s.slot, s.slotInfo = MockConnectedSlot(c, netmgrMockSlotSnapInfoYaml, nil, "network-manager")
}

func (s *NetworkManagerInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "network-manager")
}

// The label glob when all apps are bound to the network-manager slot
func (s *NetworkManagerInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	appSet := appSetWithApps(c, "network-manager", "app1", "app2")
	si := appSet.Info()
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap:      si,
		Name:      "network-manager",
		Interface: "network-manager",
		Apps:      map[string]*snap.AppInfo{"app1": si.Apps["app1"], "app2": si.Apps["app2"]},
	}, appSet, nil, nil)

	release.OnClassic = false

	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.network-manager-client.nmcli"})
	c.Assert(apparmorSpec.SnippetForTag("snap.network-manager-client.nmcli"), testutil.Contains, `peer=(label="snap.network-manager.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the network-manager slot
func (s *NetworkManagerInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	appSet := appSetWithApps(c, "network-manager", "app1", "app2", "app3")
	si := appSet.Info()
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap:      si,
		Name:      "network-manager",
		Interface: "network-manager",
		Apps:      map[string]*snap.AppInfo{"app1": si.Apps["app1"], "app2": si.Apps["app2"]},
	}, appSet, nil, nil)

	release.OnClassic = false

	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.network-manager-client.nmcli"})
	c.Assert(apparmorSpec.SnippetForTag("snap.network-manager-client.nmcli"), testutil.Contains, `peer=(label="snap.network-manager{.app1,.app2}"),`)
}

// The label uses short form when exactly one app is bound to the network-manager slot
func (s *NetworkManagerInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	appSet := appSetWithApps(c, "network-manager", "app")
	si := appSet.Info()
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap:      si,
		Name:      "network-manager",
		Interface: "network-manager",
		Apps:      map[string]*snap.AppInfo{"app": si.Apps["app"]},
	}, appSet, nil, nil)

	release.OnClassic = false
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.network-manager-client.nmcli"})
	c.Assert(apparmorSpec.SnippetForTag("snap.network-manager-client.nmcli"), testutil.Contains, `peer=(label="snap.network-manager.app"),`)
}

func (s *NetworkManagerInterfaceSuite) TestConnectedPlugSnippedUsesUnconfinedLabelOnClassic(c *C) {
	release.OnClassic = true
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.network-manager-client.nmcli"})
	c.Assert(apparmorSpec.SnippetForTag("snap.network-manager-client.nmcli"), testutil.Contains, "peer=(label=unconfined),")
}

func (s *NetworkManagerInterfaceSuite) TestConnectedPlugIntrospectionOnCore(c *C) {
	release.OnClassic = false
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.network-manager-client.nmcli"})
	c.Assert(apparmorSpec.SnippetForTag("snap.network-manager-client.nmcli"), testutil.Contains, "Allow us to introspect the network-manager providing snap")
}

func (s *NetworkManagerInterfaceSuite) TestConnectedSlotIntrospectionOnCore(c *C) {
	release.OnClassic = false
	apparmorSpec := apparmor.NewSpecification(s.slot.AppSet())
	err := apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.network-manager.nm"})
	c.Assert(apparmorSpec.SnippetForTag("snap.network-manager.nm"), testutil.Contains, "# Allow plugs to introspect us")
}

func (s *NetworkManagerInterfaceSuite) TestConnectedPlugIntrospectionOnClassic(c *C) {
	release.OnClassic = true
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.network-manager-client.nmcli"})
	c.Assert(apparmorSpec.SnippetForTag("snap.network-manager-client.nmcli"), Not(testutil.Contains), "Allow us to introspect the network-manager providing snap")
}

func (s *NetworkManagerInterfaceSuite) TestConnectedSlotIntrospectionOnClassic(c *C) {
	release.OnClassic = true
	apparmorSpec := apparmor.NewSpecification(s.slot.AppSet())
	err := apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.network-manager.nm"})
	c.Assert(apparmorSpec.SnippetForTag("snap.network-manager.nm"), Not(testutil.Contains), "# Allow plugs to introspect us")
}

func (s *NetworkManagerInterfaceSuite) TestConnectedSlotSnippetAppArmor(c *C) {
	apparmorSpec := apparmor.NewSpecification(s.slot.AppSet())
	err := apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.network-manager.nm"})
	c.Assert(apparmorSpec.SnippetForTag("snap.network-manager.nm"), testutil.Contains, `/org/freedesktop/NetworkManager`)
}

func (s *NetworkManagerInterfaceSuite) TestUsedSecuritySystems(c *C) {
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 1)

	apparmorSpec = apparmor.NewSpecification(s.slot.AppSet())
	err = apparmorSpec.AddPermanentSlot(s.iface, s.slotInfo)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 1)

	dbusSpec := dbus.NewSpecification(s.slot.AppSet())
	err = dbusSpec.AddPermanentSlot(s.iface, s.slotInfo)
	c.Assert(err, IsNil)
	c.Assert(dbusSpec.SecurityTags(), HasLen, 1)

	dbusSpec = dbus.NewSpecification(s.plug.AppSet())
	err = dbusSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(dbusSpec.SecurityTags(), HasLen, 0)
}

func (s *NetworkManagerInterfaceSuite) TestSecCompPermanentSlot(c *C) {
	seccompSpec := seccomp.NewSpecification(s.slot.AppSet())
	err := seccompSpec.AddPermanentSlot(s.iface, s.slotInfo)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.network-manager.nm"})
	c.Check(seccompSpec.SnippetForTag("snap.network-manager.nm"), testutil.Contains, "listen\n")
}

func (s *NetworkManagerInterfaceSuite) TestUDevPermanentSlot(c *C) {
	spec := udev.NewSpecification(s.slot.AppSet())
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets(), testutil.Contains, `# network-manager
KERNEL=="rfkill", TAG+="snap_network-manager_nm"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_network-manager_nm", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_network-manager_nm $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *NetworkManagerInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
