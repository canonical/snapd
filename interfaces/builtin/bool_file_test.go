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
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) {
	TestingT(t)
}

type BoolFileInterfaceSuite struct {
	testutil.BaseTest
	iface                 interfaces.Interface
	gpioSlotInfo          *snap.SlotInfo
	gpioSlot              *interfaces.ConnectedSlot
	gpioCleanedSlotInfo   *snap.SlotInfo
	gpioCleanedSlot       *interfaces.ConnectedSlot
	ledSlotInfo           *snap.SlotInfo
	ledSlot               *interfaces.ConnectedSlot
	badPathSlotInfo       *snap.SlotInfo
	badPathSlot           *interfaces.ConnectedSlot
	parentDirPathSlotInfo *snap.SlotInfo
	parentDirPathSlot     *interfaces.ConnectedSlot
	missingPathSlotInfo   *snap.SlotInfo
	missingPathSlot       *interfaces.ConnectedSlot
	badInterfaceSlot      *interfaces.ConnectedSlot
	plugInfo              *snap.PlugInfo
	plug                  *interfaces.ConnectedPlug
	badInterfaceSlotInfo  *snap.SlotInfo
	badInterfacePlugInfo  *snap.PlugInfo
	badInterfacePlug      *interfaces.ConnectedPlug
}

var _ = Suite(&BoolFileInterfaceSuite{
	iface: builtin.MustInterface("bool-file"),
})

func (s *BoolFileInterfaceSuite) SetUpTest(c *C) {
	plugSnapinfo := snaptest.MockInfo(c, `
name: other
version: 0
plugs:
 plug: bool-file
apps:
 app:
  command: foo
`, nil)
	info := snaptest.MockInfo(c, `
name: ubuntu-core
version: 0
slots:
    gpio:
        interface: bool-file
        path: /sys/class/gpio/gpio13/value
    gpio-unclean:
        interface: bool-file
        path: /sys/class/gpio/gpio14/value///
    led:
        interface: bool-file
        path: "/sys/class/leds/input27::capslock/brightness"
    missing-path: bool-file
    bad-path:
        interface: bool-file
        path: path
    parent-dir-path:
        interface: bool-file
        path: "/sys/class/gpio/../value"
    bad-interface-slot: other-interface
plugs:
    plug: bool-file
    bad-interface-plug: other-interface
`, &snap.SideInfo{})
	s.gpioSlotInfo = info.Slots["gpio"]
	s.gpioSlot = interfaces.NewConnectedSlot(s.gpioSlotInfo, nil, nil)
	s.gpioCleanedSlotInfo = info.Slots["gpio-unclean"]
	s.gpioCleanedSlot = interfaces.NewConnectedSlot(s.gpioCleanedSlotInfo, nil, nil)
	s.ledSlotInfo = info.Slots["led"]
	s.ledSlot = interfaces.NewConnectedSlot(s.ledSlotInfo, nil, nil)
	s.missingPathSlotInfo = info.Slots["missing-path"]
	s.missingPathSlot = interfaces.NewConnectedSlot(s.missingPathSlotInfo, nil, nil)
	s.badPathSlotInfo = info.Slots["bad-path"]
	s.badPathSlot = interfaces.NewConnectedSlot(s.badPathSlotInfo, nil, nil)
	s.parentDirPathSlotInfo = info.Slots["parent-dir-path"]
	s.parentDirPathSlot = interfaces.NewConnectedSlot(s.parentDirPathSlotInfo, nil, nil)
	s.badInterfaceSlotInfo = info.Slots["bad-interface-slot"]
	s.badInterfaceSlot = interfaces.NewConnectedSlot(s.badInterfaceSlotInfo, nil, nil)
	s.plugInfo = plugSnapinfo.Plugs["plug"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
	s.badInterfacePlugInfo = info.Plugs["bad-interface-plug"]
	s.badInterfacePlug = interfaces.NewConnectedPlug(s.badInterfacePlugInfo, nil, nil)
}

// TODO: add test for permanent slot when we have hook support.

func (s *BoolFileInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "bool-file")
}

func (s *BoolFileInterfaceSuite) TestSanitizeSlot(c *C) {
	// Both LED and GPIO slots are accepted
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.ledSlotInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gpioSlotInfo), IsNil)
	// Slots without the "path" attribute are rejected.
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.missingPathSlotInfo), ErrorMatches,
		"bool-file must contain the path attribute")
	// Slots without the "path" attribute are rejected.
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.parentDirPathSlotInfo), ErrorMatches,
		"bool-file can only point at LED brightness or GPIO value")
	// Slots with incorrect value of the "path" attribute are rejected.
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.badPathSlotInfo), ErrorMatches,
		"bool-file can only point at LED brightness or GPIO value")
	// Verify historically filepath.Clean()d paths are still valid
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gpioCleanedSlotInfo), IsNil)
}

func (s *BoolFileInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *BoolFileInterfaceSuite) TestPlugSnippetHandlesSymlinkErrors(c *C) {
	// Symbolic link traversal is handled correctly
	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		return "", fmt.Errorf("broken symbolic link")
	})

	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	apparmorSpec := apparmor.NewSpecification(appSet)
	mylog.Check(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.gpioSlot))
	c.Assert(err, ErrorMatches, "cannot compute plug security snippet: broken symbolic link")
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 0)
}

func (s *BoolFileInterfaceSuite) TestAddConnectedPlugAdditionalSnippetsForLeds(c *C) {
	// Use a fake eval that returns just the path
	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		return path, nil
	})
	// Using a led that doesn't match, does not add
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	apparmorSpec := apparmor.NewSpecification(appSet)
	mylog.Check(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.ledSlot))

	c.Assert(apparmorSpec.Snippets(), DeepEquals, map[string][]string{
		"snap.other.app": {
			"/sys/class/leds/input27::capslock/brightness rwk,",
		},
	})

	// Make the fake eval return a path that successfully leads to added snippets
	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		return "/sys/devices/platform/leds/leds/status-grn-led/brightness", nil
	})

	// Make sure that using a path that matches the boolFileLedPattern adds the
	// additional snippets
	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	apparmorSpec2 := apparmor.NewSpecification(appSet)
	mylog.Check(apparmorSpec2.AddConnectedPlug(s.iface, s.plug, s.ledSlot))

	c.Assert(apparmorSpec2.Snippets(), DeepEquals, map[string][]string{
		"snap.other.app": {
			"/sys/devices/platform/leds/leds/status-grn-led/brightness rwk,",
			"/sys/devices/platform/leds/leds/status-grn-led/delay_off rw,",
			"/sys/devices/platform/leds/leds/status-grn-led/delay_on rw,",
			"/sys/devices/platform/leds/leds/status-grn-led/trigger rw,",
		},
	})
}

func (s *BoolFileInterfaceSuite) TestPlugSnippetDereferencesSymlinks(c *C) {
	// Use a fake (successful) dereferencing function for the remainder of the test.
	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		return "(dereferenced)" + path, nil
	})
	// Extra apparmor permission to access GPIO value
	// The path uses dereferenced symbolic links.
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	apparmorSpec := apparmor.NewSpecification(appSet)
	mylog.Check(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.gpioSlot))

	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), Equals, "(dereferenced)/sys/class/gpio/gpio13/value rwk,")
	// Extra apparmor permission to access LED brightness.
	// The path uses dereferenced symbolic links.
	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	apparmorSpec = apparmor.NewSpecification(appSet)
	mylog.Check(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.ledSlot))

	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), Equals, "(dereferenced)/sys/class/leds/input27::capslock/brightness rwk,")
}

func (s *BoolFileInterfaceSuite) TestConnectedPlugSnippetPanicksOnUnsanitizedSlots(c *C) {
	// Unsanitized slots should never be used and cause a panic.
	c.Assert(func() {
		appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

		apparmorSpec := apparmor.NewSpecification(appSet)
		apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.missingPathSlot)
	}, PanicMatches, "slot is not sanitized")
}

func (s *BoolFileInterfaceSuite) TestConnectedPlugSnippetUnusedSecuritySystems(c *C) {
	for _, slot := range []*interfaces.ConnectedSlot{s.ledSlot, s.gpioSlot} {
		// No extra seccomp permissions for plug
		appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

		seccompSpec := seccomp.NewSpecification(appSet)
		mylog.Check(seccompSpec.AddConnectedPlug(s.iface, s.plug, slot))

		c.Assert(seccompSpec.Snippets(), HasLen, 0)
		// No extra dbus permissions for plug
		appSet = mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

		dbusSpec := dbus.NewSpecification(appSet)
		mylog.Check(dbusSpec.AddConnectedPlug(s.iface, s.plug, slot))

		c.Assert(dbusSpec.Snippets(), HasLen, 0)
		// No extra udev permissions for plug
		appSet = mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

		udevSpec := udev.NewSpecification(appSet)
		c.Assert(udevSpec.AddConnectedPlug(s.iface, s.plug, slot), IsNil)
		c.Assert(udevSpec.Snippets(), HasLen, 0)
	}
}

func (s *BoolFileInterfaceSuite) TestPermanentPlugSnippetUnusedSecuritySystems(c *C) {
	// No extra seccomp permissions for plug
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plugInfo.Snap, nil))

	seccompSpec := seccomp.NewSpecification(appSet)
	mylog.Check(seccompSpec.AddPermanentPlug(s.iface, s.plugInfo))

	c.Assert(seccompSpec.Snippets(), HasLen, 0)
	// No extra dbus permissions for plug
	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.plugInfo.Snap, nil))

	dbusSpec := dbus.NewSpecification(appSet)
	mylog.Check(dbusSpec.AddPermanentPlug(s.iface, s.plugInfo))

	c.Assert(dbusSpec.Snippets(), HasLen, 0)
	// No extra udev permissions for plug
	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.plugInfo.Snap, nil))

	udevSpec := udev.NewSpecification(appSet)
	c.Assert(udevSpec.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(udevSpec.Snippets(), HasLen, 0)
}

func (s *BoolFileInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
