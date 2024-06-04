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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type utilsSuite struct {
	iface        interfaces.Interface
	slotOS       *snap.SlotInfo
	slotApp      *snap.SlotInfo
	slotSnapd    *snap.SlotInfo
	slotGadget   *snap.SlotInfo
	conSlotOS    *interfaces.ConnectedSlot
	conSlotSnapd *interfaces.ConnectedSlot
	conSlotApp   *interfaces.ConnectedSlot
}

var _ = Suite(&utilsSuite{
	iface:        &ifacetest.TestInterface{InterfaceName: "iface"},
	slotOS:       &snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeOS}},
	slotApp:      &snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeApp}},
	slotSnapd:    &snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeSnapd, SuggestedName: "snapd"}},
	slotGadget:   &snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeGadget}},
	conSlotOS:    interfaces.NewConnectedSlot(&snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeOS}}, nil, nil),
	conSlotSnapd: interfaces.NewConnectedSlot(&snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeSnapd}}, nil, nil),
	conSlotApp:   interfaces.NewConnectedSlot(&snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeApp}}, nil, nil),
})

func (s *utilsSuite) TestIsSlotSystemSlot(c *C) {
	c.Assert(builtin.ImplicitSystemPermanentSlot(s.slotApp), Equals, false)
	c.Assert(builtin.ImplicitSystemPermanentSlot(s.slotOS), Equals, true)
	c.Assert(builtin.ImplicitSystemPermanentSlot(s.slotSnapd), Equals, true)
}

func (s *utilsSuite) TestImplicitSystemConnectedSlot(c *C) {
	c.Assert(builtin.ImplicitSystemConnectedSlot(s.conSlotApp), Equals, false)
	c.Assert(builtin.ImplicitSystemConnectedSlot(s.conSlotOS), Equals, true)
	c.Assert(builtin.ImplicitSystemConnectedSlot(s.conSlotSnapd), Equals, true)
}

func (s *utilsSuite) TestAareExclusivePatterns(c *C) {
	res := builtin.AareExclusivePatterns("foo-bar")
	c.Check(res, DeepEquals, []string{
		"[^f]*",
		"f[^o]*",
		"fo[^o]*",
		"foo[^-]*",
		"foo-[^b]*",
		"foo-b[^a]*",
		"foo-ba[^r]*",
	})
}

func (s *utilsSuite) TestAareExclusivePatternsInstance(c *C) {
	res := builtin.AareExclusivePatterns("foo-bar+baz")
	c.Check(res, DeepEquals, []string{
		"[^f]*",
		"f[^o]*",
		"fo[^o]*",
		"foo[^-]*",
		"foo-[^b]*",
		"foo-b[^a]*",
		"foo-ba[^r]*",
		"foo-bar[^+]*",
		"foo-bar+[^b]*",
		"foo-bar+b[^a]*",
		"foo-bar+ba[^z]*",
	})
}

func (s *utilsSuite) TestAareExclusivePatternsInvalid(c *C) {
	bad := []string{
		// AARE in name (man apparmor.d: AARE = ?*[]{}^)
		"bad{",
		"ba}d",
		"b[ad",
		"]bad",
		"b^d",
		"b*d",
		"b?d",
		"bad{+good",
		"ba}d+good",
		"b[ad+good",
		"]bad+good",
		"b^d+good",
		"b*d+good",
		"b?d+good",
		// AARE in instance (man apparmor.d: AARE = ?*[]{}^)
		"good+bad{",
		"good+ba}d",
		"good+b[ad",
		"good+]bad",
		"good+b^d",
		"good+b*d",
		"good+b?d",
		// various other unexpected in name
		"+good",
		"/bad",
		"bad,",
		".bad.",
		"ba'd",
		"b\"ad",
		"=bad",
		"b\\0d",
		"b\ad",
		"(bad",
		"bad)",
		"b<ad",
		"b>ad",
		"bad!",
		"b#d",
		":bad",
		"b@d",
		"@{BAD}",
		"b**d",
		"bad -> evil",
		"b a d",
		// various other unexpected in instance
		"good+",
		"good+/bad",
		"good+bad,",
		"good+.bad.",
		"good+ba'd",
		"good+b\"ad",
		"good+=bad",
		"good+b\\0d",
		"good+b\ad",
		"good+(bad",
		"good+bad)",
		"good+b<ad",
		"good+b>ad",
		"good+bad!",
		"good+b#d",
		"good+:bad",
		"good+b@d",
		"good+@{BAD}",
		"good+b**d",
		"good+bad -> evil",
	}

	for _, s := range bad {
		res := builtin.AareExclusivePatterns(s)
		c.Check(res, IsNil)
	}
}

func (s *utilsSuite) TestGetDesktopFileRules(c *C) {
	res := builtin.GetDesktopFileRules("foo-bar")
	c.Check(res, DeepEquals, []string{
		"# Support applications which use the unity messaging menu, xdg-mime, etc",
		"# This leaks the names of snaps with desktop files",
		"/var/lib/snapd/desktop/applications/ r,",
		"# Allowing reading only our desktop files (required by (at least) the unity",
		"# messaging menu).",
		"# parallel-installs: this leaks read access to desktop files owned by keyed",
		"# instances of @{SNAP_NAME} to @{SNAP_NAME} snap",
		"/var/lib/snapd/desktop/applications/@{SNAP_INSTANCE_DESKTOP}_*.desktop r,",
		"# Explicitly deny access to other snap's desktop files",
		"deny /var/lib/snapd/desktop/applications/@{SNAP_INSTANCE_DESKTOP}[^_.]*.desktop r,",
		"deny /var/lib/snapd/desktop/applications/[^f]* r,",
		"deny /var/lib/snapd/desktop/applications/f[^o]* r,",
		"deny /var/lib/snapd/desktop/applications/fo[^o]* r,",
		"deny /var/lib/snapd/desktop/applications/foo[^-]* r,",
		"deny /var/lib/snapd/desktop/applications/foo-[^b]* r,",
		"deny /var/lib/snapd/desktop/applications/foo-b[^a]* r,",
		"deny /var/lib/snapd/desktop/applications/foo-ba[^r]* r,",
	})
}

func MockPlug(c *C, yaml string, si *snap.SideInfo, plugName string) *snap.PlugInfo {
	return builtin.MockPlug(c, yaml, si, plugName)
}

func MockSlot(c *C, yaml string, si *snap.SideInfo, slotName string) *snap.SlotInfo {
	return builtin.MockSlot(c, yaml, si, slotName)
}

func MockConnectedPlug(c *C, yaml string, si *snap.SideInfo, plugName string) (*interfaces.ConnectedPlug, *snap.PlugInfo) {
	info := snaptest.MockInfo(c, yaml, si)
	if plugInfo, ok := info.Plugs[plugName]; ok {
		return interfaces.NewConnectedPlug(plugInfo, nil, nil), plugInfo
	}
	panic(fmt.Sprintf("cannot find plug %q in snap %q", plugName, info.InstanceName()))
}

func MockConnectedSlot(c *C, yaml string, si *snap.SideInfo, slotName string) (*interfaces.ConnectedSlot, *snap.SlotInfo) {
	info := snaptest.MockInfo(c, yaml, si)
	if slotInfo, ok := info.Slots[slotName]; ok {
		return interfaces.NewConnectedSlot(slotInfo, nil, nil), slotInfo
	}
	panic(fmt.Sprintf("cannot find slot %q in snap %q", slotName, info.InstanceName()))
}

func MockHotplugSlot(c *C, yaml string, si *snap.SideInfo, hotplugKey snap.HotplugKey, ifaceName, slotName string, staticAttrs map[string]interface{}) *snap.SlotInfo {
	info := snaptest.MockInfo(c, yaml, si)
	if _, ok := info.Slots[slotName]; ok {
		panic(fmt.Sprintf("slot %q already present in the snap yaml", slotName))
	}
	return &snap.SlotInfo{
		Snap:       info,
		Name:       slotName,
		Attrs:      staticAttrs,
		HotplugKey: hotplugKey,
	}
}

func (s *utilsSuite) TestStringListAttributeHappy(c *C) {
	const snapYaml = `name: consumer
version: 0
plugs:
 personal-files:
  write: ["$HOME/dir1", "/etc/.hidden2"]
slots:
 shared-memory:
  write: ["foo", "bar"]
`
	snap := snaptest.MockInfo(c, snapYaml, nil)
	plugInfo := snap.Plugs["personal-files"]
	slotInfo := snap.Slots["shared-memory"]
	plug := interfaces.NewConnectedPlug(plugInfo, nil, nil)
	slot := interfaces.NewConnectedSlot(slotInfo, nil, nil)
	list, err := builtin.StringListAttribute(plug, "write")
	c.Assert(err, IsNil)
	c.Check(list, DeepEquals, []string{"$HOME/dir1", "/etc/.hidden2"})
	list, err = builtin.StringListAttribute(plug, "read")
	c.Assert(err, IsNil)
	c.Check(list, IsNil)
	list, err = builtin.StringListAttribute(slot, "write")
	c.Assert(err, IsNil)
	c.Check(list, DeepEquals, []string{"foo", "bar"})
}

func (s *utilsSuite) TestStringListAttributeErrorNotListStrings(c *C) {
	const snapYaml = `name: consumer
version: 0
plugs:
 personal-files:
  write: [1, "two"]
`
	snap := snaptest.MockInfo(c, snapYaml, nil)
	plugInfo := snap.Plugs["personal-files"]
	plug := interfaces.NewConnectedPlug(plugInfo, nil, nil)
	list, err := builtin.StringListAttribute(plug, "write")
	c.Assert(list, IsNil)
	c.Assert(err, ErrorMatches, `"write" attribute must be a list of strings, not "\[1 two\]"`)
}
