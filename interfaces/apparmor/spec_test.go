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

package apparmor_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type specSuite struct {
	iface    *ifacetest.TestInterface
	spec     *apparmor.Specification
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
}

var _ = Suite(&specSuite{
	iface: &ifacetest.TestInterface{
		InterfaceName: "test",
		AppArmorConnectedPlugCallback: func(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddSnippet("connected-plug")
			return nil
		},
		AppArmorConnectedSlotCallback: func(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddSnippet("connected-slot")
			return nil
		},
		AppArmorPermanentPlugCallback: func(spec *apparmor.Specification, plug *snap.PlugInfo) error {
			spec.AddSnippet("permanent-plug")
			return nil
		},
		AppArmorPermanentSlotCallback: func(spec *apparmor.Specification, slot *snap.SlotInfo) error {
			spec.AddSnippet("permanent-slot")
			return nil
		},
	},
	plugInfo: &snap.PlugInfo{
		Snap:      &snap.Info{SuggestedName: "snap1"},
		Name:      "name",
		Interface: "test",
		Apps: map[string]*snap.AppInfo{
			"app1": {
				Snap: &snap.Info{
					SuggestedName: "snap1",
				},
				Name: "app1"}},
	},
	slotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "snap2"},
		Name:      "name",
		Interface: "test",
		Apps: map[string]*snap.AppInfo{
			"app2": {
				Snap: &snap.Info{
					SuggestedName: "snap2",
				},
				Name: "app2"}},
	},
})

func (s *specSuite) SetUpTest(c *C) {
	s.spec = &apparmor.Specification{}
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil)
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil)
}

// The spec.Specification can be used through the interfaces.Specification interface
func (s *specSuite) TestSpecificationIface(c *C) {
	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(r.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(s.spec.Snippets(), DeepEquals, map[string][]string{
		"snap.snap1.app1": {"connected-plug", "permanent-plug"},
		"snap.snap2.app2": {"connected-slot", "permanent-slot"},
	})
}

// AddSnippet adds a snippet for the given security tag.
func (s *specSuite) TestAddSnippet(c *C) {
	restore := apparmor.SetSpecScope(s.spec, []string{"snap.demo.command", "snap.demo.service"}, "demo")
	defer restore()

	// Add two snippets in the context we are in.
	s.spec.AddSnippet("snippet 1")
	s.spec.AddSnippet("snippet 2")

	// The snippets were recorded correctly.
	c.Assert(s.spec.UpdateNS(), HasLen, 0)
	c.Assert(s.spec.Snippets(), DeepEquals, map[string][]string{
		"snap.demo.command": {"snippet 1", "snippet 2"},
		"snap.demo.service": {"snippet 1", "snippet 2"},
	})
	c.Assert(s.spec.SnippetForTag("snap.demo.command"), Equals, "snippet 1\nsnippet 2")
	c.Assert(s.spec.SecurityTags(), DeepEquals, []string{"snap.demo.command", "snap.demo.service"})
}

// AddUpdateNS adds a snippet for the snap-update-ns profile for a given snap.
func (s *specSuite) TestAddUpdateNS(c *C) {
	restore := apparmor.SetSpecScope(s.spec, []string{"snap.demo.command", "snap.demo.service"}, "demo")
	defer restore()

	// Add a two snap-update-ns snippets in the context we are in.
	s.spec.AddUpdateNS("s-u-n snippet 1")
	s.spec.AddUpdateNS("s-u-n snippet 2")

	// The snippets were recorded correctly and in the right place.
	c.Assert(s.spec.UpdateNS(), DeepEquals, map[string][]string{
		"demo": {"s-u-n snippet 1", "s-u-n snippet 2"},
	})
	c.Assert(s.spec.SnippetForTag("snap.demo.command"), Equals, "")
	c.Assert(s.spec.SecurityTags(), HasLen, 0)
}

const snapWithLayout = `
name: vanguard
version: 0
apps:
  vanguard:
    command: vanguard
layout:
  /usr/foo:
    bind: $SNAP/usr/foo
  /var/tmp:
    type: tmpfs
    mode: 1777
  /var/cache/mylink:
    symlink: $SNAP_DATA/link/target
  /etc/foo.conf:
    bind-file: $SNAP/foo.conf
`

func (s *specSuite) TestApparmorSnippetsFromLayout(c *C) {
	snapInfo := snaptest.MockInfo(c, snapWithLayout, &snap.SideInfo{Revision: snap.R(42)})
	restore := apparmor.SetSpecScope(s.spec, []string{"snap.vanguard.vanguard"}, "vanguard")
	defer restore()

	s.spec.AddSnapLayout(snapInfo)
	c.Assert(s.spec.Snippets(), DeepEquals, map[string][]string{
		"snap.vanguard.vanguard": {
			"# Layout path: /etc/foo.conf\n/etc/foo.conf mrwklix,",
			"# Layout path: /usr/foo\n/usr/foo{,/**} mrwklix,",
			"# Layout path: /var/cache/mylink\n# (no extra permissions required for symlink)",
			"# Layout path: /var/tmp\n/var/tmp{,/**} mrwklix,",
		},
	})

	profile0 := `  # Layout /etc/foo.conf: bind-file $SNAP/foo.conf
  mount options=(bind, rw) /snap/vanguard/42/foo.conf -> /etc/foo.conf,
  umount /etc/foo.conf,
  # Writable mimic /etc
  mount options=(rbind, rw) /etc/ -> /tmp/.snap/etc/,
  mount fstype=tmpfs options=(rw) tmpfs -> /etc/,
  mount options=(rbind, rw) /tmp/.snap/etc/** -> /etc/**,
  mount options=(bind, rw) /tmp/.snap/etc/* -> /etc/*,
  umount /tmp/.snap/etc/,
  umount /etc{,/**},
  /etc/** rw,
  /tmp/.snap/etc/** rw,
  /tmp/.snap/etc/ rw,
  /tmp/.snap/ rw,
  # Writable mimic /snap/vanguard/42
  mount options=(rbind, rw) /snap/vanguard/42/ -> /tmp/.snap/snap/vanguard/42/,
  mount fstype=tmpfs options=(rw) tmpfs -> /snap/vanguard/42/,
  mount options=(rbind, rw) /tmp/.snap/snap/vanguard/42/** -> /snap/vanguard/42/**,
  mount options=(bind, rw) /tmp/.snap/snap/vanguard/42/* -> /snap/vanguard/42/*,
  umount /tmp/.snap/snap/vanguard/42/,
  umount /snap/vanguard/42{,/**},
  /snap/vanguard/42/** rw,
  /snap/vanguard/42/ rw,
  /snap/vanguard/ rw,
  /tmp/.snap/snap/vanguard/42/** rw,
  /tmp/.snap/snap/vanguard/42/ rw,
  /tmp/.snap/snap/vanguard/ rw,
  /tmp/.snap/snap/ rw,
  /tmp/.snap/ rw,
`
	profile1 := `  # Layout /usr/foo: bind $SNAP/usr/foo
  mount options=(rbind, rw) /snap/vanguard/42/usr/foo/ -> /usr/foo/,
  umount /usr/foo/,
  # Writable mimic /usr
  mount options=(rbind, rw) /usr/ -> /tmp/.snap/usr/,
  mount fstype=tmpfs options=(rw) tmpfs -> /usr/,
  mount options=(rbind, rw) /tmp/.snap/usr/** -> /usr/**,
  mount options=(bind, rw) /tmp/.snap/usr/* -> /usr/*,
  umount /tmp/.snap/usr/,
  umount /usr{,/**},
  /usr/** rw,
  /tmp/.snap/usr/** rw,
  /tmp/.snap/usr/ rw,
  /tmp/.snap/ rw,
  # Writable mimic /snap/vanguard/42/usr
  mount options=(rbind, rw) /snap/vanguard/42/usr/ -> /tmp/.snap/snap/vanguard/42/usr/,
  mount fstype=tmpfs options=(rw) tmpfs -> /snap/vanguard/42/usr/,
  mount options=(rbind, rw) /tmp/.snap/snap/vanguard/42/usr/** -> /snap/vanguard/42/usr/**,
  mount options=(bind, rw) /tmp/.snap/snap/vanguard/42/usr/* -> /snap/vanguard/42/usr/*,
  umount /tmp/.snap/snap/vanguard/42/usr/,
  umount /snap/vanguard/42/usr{,/**},
  /snap/vanguard/42/usr/** rw,
  /snap/vanguard/42/usr/ rw,
  /snap/vanguard/42/ rw,
  /snap/vanguard/ rw,
  /tmp/.snap/snap/vanguard/42/usr/** rw,
  /tmp/.snap/snap/vanguard/42/usr/ rw,
  /tmp/.snap/snap/vanguard/42/ rw,
  /tmp/.snap/snap/vanguard/ rw,
  /tmp/.snap/snap/ rw,
  /tmp/.snap/ rw,
`
	profile2 := `  # Layout /var/cache/mylink: symlink $SNAP_DATA/link/target
  /var/cache/mylink rw,
  # Writable mimic /var/cache
  mount options=(rbind, rw) /var/cache/ -> /tmp/.snap/var/cache/,
  mount fstype=tmpfs options=(rw) tmpfs -> /var/cache/,
  mount options=(rbind, rw) /tmp/.snap/var/cache/** -> /var/cache/**,
  mount options=(bind, rw) /tmp/.snap/var/cache/* -> /var/cache/*,
  umount /tmp/.snap/var/cache/,
  umount /var/cache{,/**},
  /var/cache/** rw,
  /var/cache/ rw,
  /tmp/.snap/var/cache/** rw,
  /tmp/.snap/var/cache/ rw,
  /tmp/.snap/var/ rw,
  /tmp/.snap/ rw,
`
	profile3 := `  # Layout /var/tmp: type tmpfs, mode: 01777
  mount fstype=tmpfs tmpfs -> /var/tmp/,
  umount /var/tmp/,
  # Writable mimic /var
  mount options=(rbind, rw) /var/ -> /tmp/.snap/var/,
  mount fstype=tmpfs options=(rw) tmpfs -> /var/,
  mount options=(rbind, rw) /tmp/.snap/var/** -> /var/**,
  mount options=(bind, rw) /tmp/.snap/var/* -> /var/*,
  umount /tmp/.snap/var/,
  umount /var{,/**},
  /var/** rw,
  /tmp/.snap/var/** rw,
  /tmp/.snap/var/ rw,
  /tmp/.snap/ rw,
`
	updateNS := s.spec.UpdateNS()["vanguard"]
	c.Assert(updateNS[0], Equals, profile0)
	c.Assert(updateNS[1], Equals, profile1)
	c.Assert(updateNS[2], Equals, profile2)
	c.Assert(updateNS[3], Equals, profile3)
	c.Assert(updateNS, DeepEquals, []string{profile0, profile1, profile2, profile3})
}
