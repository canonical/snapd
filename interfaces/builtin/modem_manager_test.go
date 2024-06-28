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
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type ModemManagerInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const modemmgrMockSlotSnapInfoYaml = `name: modem-manager
version: 1.0
apps:
 mm:
  command: foo
  slots: [modem-manager]
`

const modemmgrMockPlugSnapInfoYaml = `name: modem-manager
version: 1.0
plugs:
 modem-manager:
  interface: modem-manager
apps:
 mmcli:
  command: foo
  plugs:
   - modem-manager
`

var _ = Suite(&ModemManagerInterfaceSuite{
	iface: builtin.MustInterface("modem-manager"),
})

func (s *ModemManagerInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, modemmgrMockPlugSnapInfoYaml, nil, "modem-manager")
	s.slot, s.slotInfo = MockConnectedSlot(c, modemmgrMockSlotSnapInfoYaml, nil, "modem-manager")
}

func (s *ModemManagerInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "modem-manager")
}

// The label glob when all apps are bound to the modem-manager slot
func (s *ModemManagerInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	appSet := appSetWithApps(c, "modem-manager-prod", "app1", "app2")
	si := appSet.Info()
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap:      si,
		Name:      "modem-manager",
		Interface: "modem-manager",
		Apps:      map[string]*snap.AppInfo{"app1": si.Apps["app1"], "app2": si.Apps["app2"]},
	}, appSet, nil, nil)

	release.OnClassic = false

	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.modem-manager.mmcli"})
	c.Assert(apparmorSpec.SnippetForTag("snap.modem-manager.mmcli"), testutil.Contains, `peer=(label="snap.modem-manager-prod.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the modem-manager slot
func (s *ModemManagerInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	appSet := appSetWithApps(c, "modem-manager", "app1", "app2", "app3")
	si := appSet.Info()
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap:      si,
		Name:      "modem-manager",
		Interface: "modem-manager",
		Apps:      map[string]*snap.AppInfo{"app1": si.Apps["app1"], "app2": si.Apps["app2"]},
	}, appSet, nil, nil)

	release.OnClassic = false

	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.modem-manager.mmcli"})
	c.Assert(apparmorSpec.SnippetForTag("snap.modem-manager.mmcli"), testutil.Contains, `peer=(label="snap.modem-manager{.app1,.app2}"),`)
}

// The label uses short form when exactly one app is bound to the modem-manager slot
func (s *ModemManagerInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	appSet := appSetWithApps(c, "modem-manager", "app")
	si := appSet.Info()
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap:      si,
		Name:      "modem-manager",
		Interface: "modem-manager",
		Apps:      map[string]*snap.AppInfo{"app": si.Apps["app"]},
	}, appSet, nil, nil)

	release.OnClassic = false

	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.modem-manager.mmcli"})
	c.Assert(apparmorSpec.SnippetForTag("snap.modem-manager.mmcli"), testutil.Contains, `peer=(label="snap.modem-manager.app"),`)
}

func (s *ModemManagerInterfaceSuite) TestConnectedPlugSnippetUsesUnconfinedLabelNot(c *C) {
	release.OnClassic = false
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.modem-manager.mmcli"})
	snippet := apparmorSpec.SnippetForTag("snap.modem-manager.mmcli")
	c.Assert(snippet, Not(testutil.Contains), "peer=(label=unconfined),")
	c.Assert(snippet, testutil.Contains, "org/freedesktop/ModemManager1")
}

func (s *ModemManagerInterfaceSuite) TestConnectedPlugSnippetUsesUnconfinedLabelOnClassic(c *C) {
	release.OnClassic = true

	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.modem-manager.mmcli"})
	c.Assert(apparmorSpec.SnippetForTag("snap.modem-manager.mmcli"), testutil.Contains, "peer=(label=unconfined),")
}

func (s *ModemManagerInterfaceSuite) TestUsedSecuritySystems(c *C) {
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 1)

	dbusSpec := dbus.NewSpecification(s.plug.AppSet())
	err = dbusSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(dbusSpec.SecurityTags(), HasLen, 0)

	dbusSpec = dbus.NewSpecification(s.slot.AppSet())
	err = dbusSpec.AddPermanentSlot(s.iface, s.slotInfo)
	c.Assert(err, IsNil)
	c.Assert(dbusSpec.SecurityTags(), HasLen, 1)

	udevSpec := udev.NewSpecification(s.slot.AppSet())
	c.Assert(udevSpec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(udevSpec.Snippets(), HasLen, 3)
	c.Assert(udevSpec.Snippets()[0], testutil.Contains, `SUBSYSTEMS=="usb"`)
	c.Assert(udevSpec.Snippets(), testutil.Contains, `# modem-manager
KERNEL=="rfcomm*|tty[a-zA-Z]*[0-9]*|cdc-wdm[0-9]*|*MBIM|*QMI|*AT|*QCDM", TAG+="snap_modem-manager_mm"`)
	c.Assert(udevSpec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_modem-manager_mm", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_modem-manager_mm $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *ModemManagerInterfaceSuite) TestPermanentSlotDBus(c *C) {
	dbusSpec := dbus.NewSpecification(s.slot.AppSet())
	err := dbusSpec.AddPermanentSlot(s.iface, s.slotInfo)
	c.Assert(err, IsNil)
	c.Assert(dbusSpec.SecurityTags(), DeepEquals, []string{"snap.modem-manager.mm"})
	snippet := dbusSpec.SnippetForTag("snap.modem-manager.mm")
	c.Assert(snippet, testutil.Contains, "allow own=\"org.freedesktop.ModemManager1\"")
	c.Assert(snippet, testutil.Contains, "allow send_destination=\"org.freedesktop.ModemManager1\"")
}

func (s *ModemManagerInterfaceSuite) TestPermanentSlotSecComp(c *C) {
	seccompSpec := seccomp.NewSpecification(s.slot.AppSet())
	err := seccompSpec.AddPermanentSlot(s.iface, s.slotInfo)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.modem-manager.mm"})
	c.Check(seccompSpec.SnippetForTag("snap.modem-manager.mm"), testutil.Contains, "listen\n")
}

func (s *ModemManagerInterfaceSuite) TestConnectedPlugDBus(c *C) {
	release.OnClassic = false
	dbusSpec := dbus.NewSpecification(s.plug.AppSet())
	err := dbusSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(dbusSpec.SecurityTags(), DeepEquals, []string(nil))
}

func (s *ModemManagerInterfaceSuite) TestConnectedPlugDBusClassic(c *C) {
	release.OnClassic = true
	dbusSpec := dbus.NewSpecification(s.plug.AppSet())
	err := dbusSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(dbusSpec.SecurityTags(), DeepEquals, []string(nil))
}

func (s *ModemManagerInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
