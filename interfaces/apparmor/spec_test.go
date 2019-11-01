// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

	"github.com/snapcore/snapd/testutil"
)

type specSuite struct {
	testutil.BaseTest
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
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.spec = &apparmor.Specification{}
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
}

func (s *specSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
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
	restore := apparmor.SetSpecScope(s.spec, []string{"snap.demo.command", "snap.demo.service"})
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
	restore := apparmor.SetSpecScope(s.spec, []string{"snap.demo.command", "snap.demo.service"})
	defer restore()

	// Add a two snap-update-ns snippets in the context we are in.
	s.spec.AddUpdateNS("s-u-n snippet 1")
	s.spec.AddUpdateNS("s-u-n snippet 2")

	// The snippets were recorded correctly and in the right place.
	c.Assert(s.spec.UpdateNS(), DeepEquals, []string{
		"s-u-n snippet 1", "s-u-n snippet 2",
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
	restore := apparmor.SetSpecScope(s.spec, []string{"snap.vanguard.vanguard"})
	defer restore()

	s.spec.AddLayout(snapInfo)
	c.Assert(s.spec.Snippets(), DeepEquals, map[string][]string{
		"snap.vanguard.vanguard": {
			"# Layout path: /etc/foo.conf\n/etc/foo.conf mrwklix,",
			"# Layout path: /usr/foo\n/usr/foo{,/**} mrwklix,",
			"# Layout path: /var/cache/mylink\n# (no extra permissions required for symlink)",
			"# Layout path: /var/tmp\n/var/tmp{,/**} mrwklix,",
		},
	})
	updateNS := s.spec.UpdateNS()

	profile0 := `  # Layout /etc/foo.conf: bind-file $SNAP/foo.conf
  mount options=(bind, rw) /snap/vanguard/42/foo.conf -> /etc/foo.conf,
  mount options=(rprivate) -> /etc/foo.conf,
  umount /etc/foo.conf,
  # Writable mimic /etc
  # .. permissions for traversing the prefix that is assumed to exist
  / r,
  # .. variant with mimic at /etc/
  # Allow reading the mimic directory, it must exist in the first place.
  /etc/ r,
  # Allow setting the read-only directory aside via a bind mount.
  /tmp/.snap/etc/ rw,
  mount options=(rbind, rw) /etc/ -> /tmp/.snap/etc/,
  # Allow mounting tmpfs over the read-only directory.
  mount fstype=tmpfs options=(rw) tmpfs -> /etc/,
  # Allow creating empty files and directories for bind mounting things
  # to reconstruct the now-writable parent directory.
  /tmp/.snap/etc/*/ rw,
  /etc/*/ rw,
  mount options=(rbind, rw) /tmp/.snap/etc/*/ -> /etc/*/,
  /tmp/.snap/etc/* rw,
  /etc/* rw,
  mount options=(bind, rw) /tmp/.snap/etc/* -> /etc/*,
  # Allow unmounting the auxiliary directory.
  # TODO: use fstype=tmpfs here for more strictness (LP: #1613403)
  mount options=(rprivate) -> /tmp/.snap/etc/,
  umount /tmp/.snap/etc/,
  # Allow unmounting the destination directory as well as anything
  # inside.  This lets us perform the undo plan in case the writable
  # mimic fails.
  mount options=(rprivate) -> /etc/,
  mount options=(rprivate) -> /etc/*,
  mount options=(rprivate) -> /etc/*/,
  umount /etc/,
  umount /etc/*,
  umount /etc/*/,
  # Writable mimic /snap/vanguard/42
  # .. permissions for traversing the prefix that is assumed to exist
  / r,
  /snap/ r,
  /snap/vanguard/ r,
  # .. variant with mimic at /snap/vanguard/42/
  # Allow reading the mimic directory, it must exist in the first place.
  /snap/vanguard/42/ r,
  # Allow setting the read-only directory aside via a bind mount.
  /tmp/.snap/snap/vanguard/42/ rw,
  mount options=(rbind, rw) /snap/vanguard/42/ -> /tmp/.snap/snap/vanguard/42/,
  # Allow mounting tmpfs over the read-only directory.
  mount fstype=tmpfs options=(rw) tmpfs -> /snap/vanguard/42/,
  # Allow creating empty files and directories for bind mounting things
  # to reconstruct the now-writable parent directory.
  /tmp/.snap/snap/vanguard/42/*/ rw,
  /snap/vanguard/42/*/ rw,
  mount options=(rbind, rw) /tmp/.snap/snap/vanguard/42/*/ -> /snap/vanguard/42/*/,
  /tmp/.snap/snap/vanguard/42/* rw,
  /snap/vanguard/42/* rw,
  mount options=(bind, rw) /tmp/.snap/snap/vanguard/42/* -> /snap/vanguard/42/*,
  # Allow unmounting the auxiliary directory.
  # TODO: use fstype=tmpfs here for more strictness (LP: #1613403)
  mount options=(rprivate) -> /tmp/.snap/snap/vanguard/42/,
  umount /tmp/.snap/snap/vanguard/42/,
  # Allow unmounting the destination directory as well as anything
  # inside.  This lets us perform the undo plan in case the writable
  # mimic fails.
  mount options=(rprivate) -> /snap/vanguard/42/,
  mount options=(rprivate) -> /snap/vanguard/42/*,
  mount options=(rprivate) -> /snap/vanguard/42/*/,
  umount /snap/vanguard/42/,
  umount /snap/vanguard/42/*,
  umount /snap/vanguard/42/*/,
`
	c.Assert(updateNS[0], Equals, profile0)

	profile1 := `  # Layout /usr/foo: bind $SNAP/usr/foo
  mount options=(rbind, rw) /snap/vanguard/42/usr/foo/ -> /usr/foo/,
  mount options=(rprivate) -> /usr/foo/,
  umount /usr/foo/,
  # Writable mimic /usr
  # .. permissions for traversing the prefix that is assumed to exist
  / r,
  # .. variant with mimic at /usr/
  # Allow reading the mimic directory, it must exist in the first place.
  /usr/ r,
  # Allow setting the read-only directory aside via a bind mount.
  /tmp/.snap/usr/ rw,
  mount options=(rbind, rw) /usr/ -> /tmp/.snap/usr/,
  # Allow mounting tmpfs over the read-only directory.
  mount fstype=tmpfs options=(rw) tmpfs -> /usr/,
  # Allow creating empty files and directories for bind mounting things
  # to reconstruct the now-writable parent directory.
  /tmp/.snap/usr/*/ rw,
  /usr/*/ rw,
  mount options=(rbind, rw) /tmp/.snap/usr/*/ -> /usr/*/,
  /tmp/.snap/usr/* rw,
  /usr/* rw,
  mount options=(bind, rw) /tmp/.snap/usr/* -> /usr/*,
  # Allow unmounting the auxiliary directory.
  # TODO: use fstype=tmpfs here for more strictness (LP: #1613403)
  mount options=(rprivate) -> /tmp/.snap/usr/,
  umount /tmp/.snap/usr/,
  # Allow unmounting the destination directory as well as anything
  # inside.  This lets us perform the undo plan in case the writable
  # mimic fails.
  mount options=(rprivate) -> /usr/,
  mount options=(rprivate) -> /usr/*,
  mount options=(rprivate) -> /usr/*/,
  umount /usr/,
  umount /usr/*,
  umount /usr/*/,
  # Writable mimic /snap/vanguard/42/usr
  # .. permissions for traversing the prefix that is assumed to exist
  / r,
  /snap/ r,
  /snap/vanguard/ r,
  # .. variant with mimic at /snap/vanguard/42/
  # Allow reading the mimic directory, it must exist in the first place.
  /snap/vanguard/42/ r,
  # Allow setting the read-only directory aside via a bind mount.
  /tmp/.snap/snap/vanguard/42/ rw,
  mount options=(rbind, rw) /snap/vanguard/42/ -> /tmp/.snap/snap/vanguard/42/,
  # Allow mounting tmpfs over the read-only directory.
  mount fstype=tmpfs options=(rw) tmpfs -> /snap/vanguard/42/,
  # Allow creating empty files and directories for bind mounting things
  # to reconstruct the now-writable parent directory.
  /tmp/.snap/snap/vanguard/42/*/ rw,
  /snap/vanguard/42/*/ rw,
  mount options=(rbind, rw) /tmp/.snap/snap/vanguard/42/*/ -> /snap/vanguard/42/*/,
  /tmp/.snap/snap/vanguard/42/* rw,
  /snap/vanguard/42/* rw,
  mount options=(bind, rw) /tmp/.snap/snap/vanguard/42/* -> /snap/vanguard/42/*,
  # Allow unmounting the auxiliary directory.
  # TODO: use fstype=tmpfs here for more strictness (LP: #1613403)
  mount options=(rprivate) -> /tmp/.snap/snap/vanguard/42/,
  umount /tmp/.snap/snap/vanguard/42/,
  # Allow unmounting the destination directory as well as anything
  # inside.  This lets us perform the undo plan in case the writable
  # mimic fails.
  mount options=(rprivate) -> /snap/vanguard/42/,
  mount options=(rprivate) -> /snap/vanguard/42/*,
  mount options=(rprivate) -> /snap/vanguard/42/*/,
  umount /snap/vanguard/42/,
  umount /snap/vanguard/42/*,
  umount /snap/vanguard/42/*/,
  # .. variant with mimic at /snap/vanguard/42/usr/
  # Allow reading the mimic directory, it must exist in the first place.
  /snap/vanguard/42/usr/ r,
  # Allow setting the read-only directory aside via a bind mount.
  /tmp/.snap/snap/vanguard/42/usr/ rw,
  mount options=(rbind, rw) /snap/vanguard/42/usr/ -> /tmp/.snap/snap/vanguard/42/usr/,
  # Allow mounting tmpfs over the read-only directory.
  mount fstype=tmpfs options=(rw) tmpfs -> /snap/vanguard/42/usr/,
  # Allow creating empty files and directories for bind mounting things
  # to reconstruct the now-writable parent directory.
  /tmp/.snap/snap/vanguard/42/usr/*/ rw,
  /snap/vanguard/42/usr/*/ rw,
  mount options=(rbind, rw) /tmp/.snap/snap/vanguard/42/usr/*/ -> /snap/vanguard/42/usr/*/,
  /tmp/.snap/snap/vanguard/42/usr/* rw,
  /snap/vanguard/42/usr/* rw,
  mount options=(bind, rw) /tmp/.snap/snap/vanguard/42/usr/* -> /snap/vanguard/42/usr/*,
  # Allow unmounting the auxiliary directory.
  # TODO: use fstype=tmpfs here for more strictness (LP: #1613403)
  mount options=(rprivate) -> /tmp/.snap/snap/vanguard/42/usr/,
  umount /tmp/.snap/snap/vanguard/42/usr/,
  # Allow unmounting the destination directory as well as anything
  # inside.  This lets us perform the undo plan in case the writable
  # mimic fails.
  mount options=(rprivate) -> /snap/vanguard/42/usr/,
  mount options=(rprivate) -> /snap/vanguard/42/usr/*,
  mount options=(rprivate) -> /snap/vanguard/42/usr/*/,
  umount /snap/vanguard/42/usr/,
  umount /snap/vanguard/42/usr/*,
  umount /snap/vanguard/42/usr/*/,
`
	c.Assert(updateNS[1], Equals, profile1)

	profile2 := `  # Layout /var/cache/mylink: symlink $SNAP_DATA/link/target
  /var/cache/mylink rw,
  # Writable mimic /var/cache
  # .. permissions for traversing the prefix that is assumed to exist
  / r,
  # .. variant with mimic at /var/
  # Allow reading the mimic directory, it must exist in the first place.
  /var/ r,
  # Allow setting the read-only directory aside via a bind mount.
  /tmp/.snap/var/ rw,
  mount options=(rbind, rw) /var/ -> /tmp/.snap/var/,
  # Allow mounting tmpfs over the read-only directory.
  mount fstype=tmpfs options=(rw) tmpfs -> /var/,
  # Allow creating empty files and directories for bind mounting things
  # to reconstruct the now-writable parent directory.
  /tmp/.snap/var/*/ rw,
  /var/*/ rw,
  mount options=(rbind, rw) /tmp/.snap/var/*/ -> /var/*/,
  /tmp/.snap/var/* rw,
  /var/* rw,
  mount options=(bind, rw) /tmp/.snap/var/* -> /var/*,
  # Allow unmounting the auxiliary directory.
  # TODO: use fstype=tmpfs here for more strictness (LP: #1613403)
  mount options=(rprivate) -> /tmp/.snap/var/,
  umount /tmp/.snap/var/,
  # Allow unmounting the destination directory as well as anything
  # inside.  This lets us perform the undo plan in case the writable
  # mimic fails.
  mount options=(rprivate) -> /var/,
  mount options=(rprivate) -> /var/*,
  mount options=(rprivate) -> /var/*/,
  umount /var/,
  umount /var/*,
  umount /var/*/,
  # .. variant with mimic at /var/cache/
  # Allow reading the mimic directory, it must exist in the first place.
  /var/cache/ r,
  # Allow setting the read-only directory aside via a bind mount.
  /tmp/.snap/var/cache/ rw,
  mount options=(rbind, rw) /var/cache/ -> /tmp/.snap/var/cache/,
  # Allow mounting tmpfs over the read-only directory.
  mount fstype=tmpfs options=(rw) tmpfs -> /var/cache/,
  # Allow creating empty files and directories for bind mounting things
  # to reconstruct the now-writable parent directory.
  /tmp/.snap/var/cache/*/ rw,
  /var/cache/*/ rw,
  mount options=(rbind, rw) /tmp/.snap/var/cache/*/ -> /var/cache/*/,
  /tmp/.snap/var/cache/* rw,
  /var/cache/* rw,
  mount options=(bind, rw) /tmp/.snap/var/cache/* -> /var/cache/*,
  # Allow unmounting the auxiliary directory.
  # TODO: use fstype=tmpfs here for more strictness (LP: #1613403)
  mount options=(rprivate) -> /tmp/.snap/var/cache/,
  umount /tmp/.snap/var/cache/,
  # Allow unmounting the destination directory as well as anything
  # inside.  This lets us perform the undo plan in case the writable
  # mimic fails.
  mount options=(rprivate) -> /var/cache/,
  mount options=(rprivate) -> /var/cache/*,
  mount options=(rprivate) -> /var/cache/*/,
  umount /var/cache/,
  umount /var/cache/*,
  umount /var/cache/*/,
`
	c.Assert(updateNS[2], Equals, profile2)

	profile3 := `  # Layout /var/tmp: type tmpfs, mode: 01777
  mount fstype=tmpfs tmpfs -> /var/tmp/,
  mount options=(rprivate) -> /var/tmp/,
  umount /var/tmp/,
  # Writable mimic /var
  # .. permissions for traversing the prefix that is assumed to exist
  / r,
  # .. variant with mimic at /var/
  # Allow reading the mimic directory, it must exist in the first place.
  /var/ r,
  # Allow setting the read-only directory aside via a bind mount.
  /tmp/.snap/var/ rw,
  mount options=(rbind, rw) /var/ -> /tmp/.snap/var/,
  # Allow mounting tmpfs over the read-only directory.
  mount fstype=tmpfs options=(rw) tmpfs -> /var/,
  # Allow creating empty files and directories for bind mounting things
  # to reconstruct the now-writable parent directory.
  /tmp/.snap/var/*/ rw,
  /var/*/ rw,
  mount options=(rbind, rw) /tmp/.snap/var/*/ -> /var/*/,
  /tmp/.snap/var/* rw,
  /var/* rw,
  mount options=(bind, rw) /tmp/.snap/var/* -> /var/*,
  # Allow unmounting the auxiliary directory.
  # TODO: use fstype=tmpfs here for more strictness (LP: #1613403)
  mount options=(rprivate) -> /tmp/.snap/var/,
  umount /tmp/.snap/var/,
  # Allow unmounting the destination directory as well as anything
  # inside.  This lets us perform the undo plan in case the writable
  # mimic fails.
  mount options=(rprivate) -> /var/,
  mount options=(rprivate) -> /var/*,
  mount options=(rprivate) -> /var/*/,
  umount /var/,
  umount /var/*,
  umount /var/*/,
`
	c.Assert(updateNS[3], Equals, profile3)
	c.Assert(updateNS, DeepEquals, []string{profile0, profile1, profile2, profile3})
}

const snapTrivial = `
name: some-snap
version: 0
apps:
  app:
    command: app-command
`

func (s *specSuite) TestApparmorOvernameSnippetsNotInstanceKeyed(c *C) {
	snapInfo := snaptest.MockInfo(c, snapTrivial, &snap.SideInfo{Revision: snap.R(42)})
	restore := apparmor.SetSpecScope(s.spec, []string{"snap.some-snap.app"})
	defer restore()

	s.spec.AddOvername(snapInfo)
	c.Assert(s.spec.Snippets(), HasLen, 0)
	// non instance-keyed snaps require no extra snippets
	c.Assert(s.spec.UpdateNS(), HasLen, 0)
}

func (s *specSuite) TestApparmorOvernameSnippets(c *C) {
	snapInfo := snaptest.MockInfo(c, snapTrivial, &snap.SideInfo{Revision: snap.R(42)})
	snapInfo.InstanceKey = "instance"

	restore := apparmor.SetSpecScope(s.spec, []string{"snap.some-snap_instace.app"})
	defer restore()

	s.spec.AddOvername(snapInfo)
	c.Assert(s.spec.Snippets(), HasLen, 0)

	updateNS := s.spec.UpdateNS()
	c.Assert(updateNS, HasLen, 1)

	profile := `  # Allow parallel instance snap mount namespace adjustments
  mount options=(rw rbind) /snap/some-snap_instance/ -> /snap/some-snap/,
  mount options=(rw rbind) /var/snap/some-snap_instance/ -> /var/snap/some-snap/,
`
	c.Assert(updateNS[0], Equals, profile)
}

func (s *specSuite) TestUsesPtraceTrace(c *C) {
	c.Assert(s.spec.UsesPtraceTrace(), Equals, false)
	s.spec.SetUsesPtraceTrace()
	c.Assert(s.spec.UsesPtraceTrace(), Equals, true)
}

func (s *specSuite) TestSuppressPtraceTrace(c *C) {
	c.Assert(s.spec.SuppressPtraceTrace(), Equals, false)
	s.spec.SetSuppressPtraceTrace()
	c.Assert(s.spec.SuppressPtraceTrace(), Equals, true)
}

func (s *specSuite) TestSetSuppressHomeIx(c *C) {
	c.Assert(s.spec.SuppressHomeIx(), Equals, false)
	s.spec.SetSuppressHomeIx()
	c.Assert(s.spec.SuppressHomeIx(), Equals, true)
}
