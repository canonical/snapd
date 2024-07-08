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
	"strings"

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
})

func (s *specSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	const plugYaml = `name: snap1
version: 1
apps:
 app1:
  plugs: [name]
`
	s.plug, s.plugInfo = ifacetest.MockConnectedPlug(c, plugYaml, nil, "name")

	s.spec = apparmor.NewSpecification(s.plug.AppSet())

	const slotYaml = `name: snap2
version: 1
slots:
 name:
  interface: test
apps:
 app2:
`
	s.slot, s.slotInfo = ifacetest.MockConnectedSlot(c, slotYaml, nil, "name")
}

func (s *specSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

// The spec.Specification can be used through the interfaces.Specification interface
func (s *specSuite) TestSpecificationIface(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plugInfo.Snap, nil)
	c.Assert(err, IsNil)

	spec := apparmor.NewSpecification(appSet)
	var r interfaces.Specification = spec
	c.Assert(r.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(spec.Snippets(), DeepEquals, map[string][]string{
		"snap.snap1.app1": {"connected-plug", "permanent-plug"},
	})

	appSet, err = interfaces.NewSnapAppSet(s.slotInfo.Snap, nil)
	c.Assert(err, IsNil)

	spec = apparmor.NewSpecification(appSet)
	r = spec
	c.Assert(r.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(spec.Snippets(), DeepEquals, map[string][]string{
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
	c.Assert(s.spec.SnippetsForTag("snap.demo.command"), DeepEquals, []string{"snippet 1", "snippet 2"})
	c.Assert(s.spec.SnippetForTag("snap.demo.command"), Equals, "snippet 1\nsnippet 2")
	c.Assert(s.spec.SecurityTags(), DeepEquals, []string{"snap.demo.command", "snap.demo.service"})
}

// AddDeduplicatedSnippet adds a snippet for the given security tag.
func (s *specSuite) TestAddDeduplicatedSnippet(c *C) {
	restore := apparmor.SetSpecScope(s.spec, []string{"snap.demo.command", "snap.demo.service"})
	defer restore()

	// Add two snippets in the context we are in.
	s.spec.AddDeduplicatedSnippet("dedup snippet 1")
	s.spec.AddDeduplicatedSnippet("dedup snippet 1")
	s.spec.AddDeduplicatedSnippet("dedup snippet 2")
	s.spec.AddDeduplicatedSnippet("dedup snippet 2")

	// The snippets were recorded correctly.
	c.Assert(s.spec.UpdateNS(), HasLen, 0)
	c.Assert(s.spec.Snippets(), DeepEquals, map[string][]string{
		"snap.demo.command": {"dedup snippet 1", "dedup snippet 2"},
		"snap.demo.service": {"dedup snippet 1", "dedup snippet 2"},
	})
	c.Assert(s.spec.SnippetsForTag("snap.demo.command"), DeepEquals, []string{"dedup snippet 1", "dedup snippet 2"})
	c.Assert(s.spec.SnippetForTag("snap.demo.command"), Equals, "dedup snippet 1\ndedup snippet 2")
	c.Assert(s.spec.SecurityTags(), DeepEquals, []string{"snap.demo.command", "snap.demo.service"})
}

func (s *specSuite) TestAddParametricSnippet(c *C) {
	restore := apparmor.SetSpecScope(s.spec, []string{"snap.demo.command", "snap.demo.service"})
	defer restore()

	s.spec.AddParametricSnippet([]string{"prefix ", " postfix"}, "param1")
	s.spec.AddParametricSnippet([]string{"prefix ", " postfix"}, "param1")
	s.spec.AddParametricSnippet([]string{"prefix ", " postfix"}, "param2")
	s.spec.AddParametricSnippet([]string{"prefix ", " postfix"}, "param2")
	s.spec.AddParametricSnippet([]string{"other "}, "param")
	c.Assert(s.spec.SnippetsForTag("snap.demo.command"), DeepEquals, []string{
		"other param",
		"prefix {param1,param2} postfix",
	})
	c.Assert(s.spec.SnippetForTag("snap.demo.command"), Equals, "other param\nprefix {param1,param2} postfix")
	c.Assert(s.spec.Snippets(), DeepEquals, map[string][]string{
		"snap.demo.command": {"other param", "prefix {param1,param2} postfix"},
		"snap.demo.service": {"other param", "prefix {param1,param2} postfix"},
	})
}

// All of AddSnippet, AddDeduplicatedSnippet, AddParameticSnippet work correctly together.
func (s *specSuite) TestAddSnippetAndAddDeduplicatedAndParamSnippet(c *C) {
	restore := apparmor.SetSpecScope(s.spec, []string{"snap.demo.command", "snap.demo.service"})
	defer restore()

	// Add three snippets in the context we are in.
	s.spec.AddSnippet("normal")
	s.spec.AddDeduplicatedSnippet("dedup")
	s.spec.AddParametricSnippet([]string{""}, "param")

	// The snippets were recorded correctly.
	c.Assert(s.spec.UpdateNS(), HasLen, 0)
	c.Assert(s.spec.Snippets(), DeepEquals, map[string][]string{
		"snap.demo.command": {"normal", "dedup", "param"},
		"snap.demo.service": {"normal", "dedup", "param"},
	})
	c.Assert(s.spec.SnippetsForTag("snap.demo.command"), DeepEquals, []string{"normal", "dedup", "param"})
	c.Assert(s.spec.SnippetForTag("snap.demo.command"), Equals, "normal\ndedup\nparam")
	c.Assert(s.spec.SecurityTags(), DeepEquals, []string{"snap.demo.command", "snap.demo.service"})
}

// Define tags but don't add any snippets.
func (s *specSuite) TestTagsButNoSnippets(c *C) {
	restore := apparmor.SetSpecScope(s.spec, []string{"snap.demo.command", "snap.demo.service"})
	defer restore()

	c.Assert(s.spec.UpdateNS(), HasLen, 0)
	c.Assert(s.spec.Snippets(), DeepEquals, map[string][]string{})
	c.Assert(s.spec.SnippetsForTag("snap.demo.command"), DeepEquals, []string(nil))
	c.Assert(s.spec.SnippetForTag("snap.demo.command"), Equals, "")
	c.Assert(s.spec.SecurityTags(), DeepEquals, []string(nil))
}

// Don't define any tags but add snippets.
func (s *specSuite) TestNoTagsButWithSnippets(c *C) {
	restore := apparmor.SetSpecScope(s.spec, []string{})
	defer restore()

	s.spec.AddSnippet("normal")
	s.spec.AddDeduplicatedSnippet("dedup")
	s.spec.AddParametricSnippet([]string{""}, "param")

	c.Assert(s.spec.UpdateNS(), HasLen, 0)
	c.Assert(s.spec.Snippets(), DeepEquals, map[string][]string{})
	c.Assert(s.spec.SnippetsForTag("snap.demo.command"), DeepEquals, []string(nil))
	c.Assert(s.spec.SnippetForTag("snap.demo.command"), Equals, "")
	c.Assert(s.spec.SecurityTags(), DeepEquals, []string(nil))
}

// Don't define any tags or snippets.
func (s *specSuite) TestsNoTagsOrSnippets(c *C) {
	restore := apparmor.SetSpecScope(s.spec, []string{})
	defer restore()

	c.Assert(s.spec.UpdateNS(), HasLen, 0)
	c.Assert(s.spec.Snippets(), DeepEquals, map[string][]string{})
	c.Assert(s.spec.SnippetsForTag("snap.demo.command"), DeepEquals, []string(nil))
	c.Assert(s.spec.SnippetForTag("snap.demo.command"), Equals, "")
	c.Assert(s.spec.SecurityTags(), DeepEquals, []string(nil))
}

// AddUpdateNS adds a snippet for the snap-update-ns profile for a given snap.
func (s *specSuite) TestAddUpdateNS(c *C) {
	restore := apparmor.SetSpecScope(s.spec, []string{"snap.demo.command", "snap.demo.service"})
	defer restore()

	// Add a two snap-update-ns snippets in the context we are in.
	s.spec.AddUpdateNS("s-u-n snippet 1")
	s.spec.AddUpdateNS("s-u-n snippet 2")

	// Check the order of the snippets can be retrieved.
	idx, ok := s.spec.UpdateNSIndexOf("s-u-n snippet 2")
	c.Assert(ok, Equals, true)
	c.Check(idx, Equals, 1)

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

	appSet, err := interfaces.NewSnapAppSet(snapInfo, nil)
	c.Assert(err, IsNil)

	s.spec.AddLayout(appSet)

	c.Assert(s.spec.Snippets(), DeepEquals, map[string][]string{
		"snap.vanguard.vanguard": {
			"# Layout path: /etc/foo.conf\n\"/etc/foo.conf\" mrwklix,",
			"# Layout path: /usr/foo\n\"/usr/foo{,/**}\" mrwklix,",
			"# Layout path: /var/cache/mylink\n# (no extra permissions required for symlink)",
			"# Layout path: /var/tmp\n\"/var/tmp{,/**}\" mrwklix,",
		},
	})
	updateNS := s.spec.UpdateNS()

	profile0 := `  # Layout /etc/foo.conf: bind-file $SNAP/foo.conf
  mount options=(bind, rw) "/snap/vanguard/42/foo.conf" -> "/etc/foo.conf",
  mount options=(rprivate) -> "/etc/foo.conf",
  umount "/etc/foo.conf",
  # Writable mimic /etc
  # .. permissions for traversing the prefix that is assumed to exist
  "/" r,
  # .. variant with mimic at /etc/
  # Allow reading the mimic directory, it must exist in the first place.
  "/etc/" r,
  # Allow setting the read-only directory aside via a bind mount.
  "/tmp/.snap/etc/" rw,
  mount options=(rbind, rw) "/etc/" -> "/tmp/.snap/etc/",
  # Allow mounting tmpfs over the read-only directory.
  mount fstype=tmpfs options=(rw) tmpfs -> "/etc/",
  # Allow creating empty files and directories for bind mounting things
  # to reconstruct the now-writable parent directory.
  "/tmp/.snap/etc/*/" rw,
  "/etc/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/etc/*/" -> "/etc/*/",
  "/tmp/.snap/etc/*" rw,
  "/etc/*" rw,
  mount options=(bind, rw) "/tmp/.snap/etc/*" -> "/etc/*",
  # Allow unmounting the auxiliary directory.
  # TODO: use fstype=tmpfs here for more strictness (LP: #1613403)
  mount options=(rprivate) -> "/tmp/.snap/etc/",
  umount "/tmp/.snap/etc/",
  # Allow unmounting the destination directory as well as anything
  # inside.  This lets us perform the undo plan in case the writable
  # mimic fails.
  mount options=(rprivate) -> "/etc/",
  mount options=(rprivate) -> "/etc/*",
  mount options=(rprivate) -> "/etc/*/",
  umount "/etc/",
  umount "/etc/*",
  umount "/etc/*/",
  # Writable mimic /snap/vanguard/42
  "/snap/" r,
  "/snap/vanguard/" r,
  # .. variant with mimic at /snap/vanguard/42/
  "/snap/vanguard/42/" r,
  "/tmp/.snap/snap/vanguard/42/" rw,
  mount options=(rbind, rw) "/snap/vanguard/42/" -> "/tmp/.snap/snap/vanguard/42/",
  mount fstype=tmpfs options=(rw) tmpfs -> "/snap/vanguard/42/",
  "/tmp/.snap/snap/vanguard/42/*/" rw,
  "/snap/vanguard/42/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/snap/vanguard/42/*/" -> "/snap/vanguard/42/*/",
  "/tmp/.snap/snap/vanguard/42/*" rw,
  "/snap/vanguard/42/*" rw,
  mount options=(bind, rw) "/tmp/.snap/snap/vanguard/42/*" -> "/snap/vanguard/42/*",
  mount options=(rprivate) -> "/tmp/.snap/snap/vanguard/42/",
  umount "/tmp/.snap/snap/vanguard/42/",
  mount options=(rprivate) -> "/snap/vanguard/42/",
  mount options=(rprivate) -> "/snap/vanguard/42/*",
  mount options=(rprivate) -> "/snap/vanguard/42/*/",
  umount "/snap/vanguard/42/",
  umount "/snap/vanguard/42/*",
  umount "/snap/vanguard/42/*/",
`
	// Find the slice that describes profile0 by looking for the first unique
	// line of the next profile.
	start := 0
	end, _ := s.spec.UpdateNSIndexOf("  # Layout /usr/foo: bind $SNAP/usr/foo\n")
	c.Assert(strings.Join(updateNS[start:end], ""), Equals, profile0)

	profile1 := `  # Layout /usr/foo: bind $SNAP/usr/foo
  mount options=(rbind, rw) "/snap/vanguard/42/usr/foo/" -> "/usr/foo/",
  mount options=(rprivate) -> "/usr/foo/",
  umount "/usr/foo/",
  # Writable mimic /usr
  # .. variant with mimic at /usr/
  "/usr/" r,
  "/tmp/.snap/usr/" rw,
  mount options=(rbind, rw) "/usr/" -> "/tmp/.snap/usr/",
  mount fstype=tmpfs options=(rw) tmpfs -> "/usr/",
  "/tmp/.snap/usr/*/" rw,
  "/usr/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/usr/*/" -> "/usr/*/",
  "/tmp/.snap/usr/*" rw,
  "/usr/*" rw,
  mount options=(bind, rw) "/tmp/.snap/usr/*" -> "/usr/*",
  mount options=(rprivate) -> "/tmp/.snap/usr/",
  umount "/tmp/.snap/usr/",
  mount options=(rprivate) -> "/usr/",
  mount options=(rprivate) -> "/usr/*",
  mount options=(rprivate) -> "/usr/*/",
  umount "/usr/",
  umount "/usr/*",
  umount "/usr/*/",
  # Writable mimic /snap/vanguard/42/usr
  # .. variant with mimic at /snap/vanguard/42/usr/
  "/snap/vanguard/42/usr/" r,
  "/tmp/.snap/snap/vanguard/42/usr/" rw,
  mount options=(rbind, rw) "/snap/vanguard/42/usr/" -> "/tmp/.snap/snap/vanguard/42/usr/",
  mount fstype=tmpfs options=(rw) tmpfs -> "/snap/vanguard/42/usr/",
  "/tmp/.snap/snap/vanguard/42/usr/*/" rw,
  "/snap/vanguard/42/usr/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/snap/vanguard/42/usr/*/" -> "/snap/vanguard/42/usr/*/",
  "/tmp/.snap/snap/vanguard/42/usr/*" rw,
  "/snap/vanguard/42/usr/*" rw,
  mount options=(bind, rw) "/tmp/.snap/snap/vanguard/42/usr/*" -> "/snap/vanguard/42/usr/*",
  mount options=(rprivate) -> "/tmp/.snap/snap/vanguard/42/usr/",
  umount "/tmp/.snap/snap/vanguard/42/usr/",
  mount options=(rprivate) -> "/snap/vanguard/42/usr/",
  mount options=(rprivate) -> "/snap/vanguard/42/usr/*",
  mount options=(rprivate) -> "/snap/vanguard/42/usr/*/",
  umount "/snap/vanguard/42/usr/",
  umount "/snap/vanguard/42/usr/*",
  umount "/snap/vanguard/42/usr/*/",
`
	// Find the slice that describes profile1 by looking for the first unique
	// line of the next profile.
	start = end
	end, _ = s.spec.UpdateNSIndexOf("  # Layout /var/cache/mylink: symlink $SNAP_DATA/link/target\n")
	c.Assert(strings.Join(updateNS[start:end], ""), Equals, profile1)

	profile2 := `  # Layout /var/cache/mylink: symlink $SNAP_DATA/link/target
  "/var/cache/mylink" rw,
  # Writable mimic /var/cache
  # .. variant with mimic at /var/
  "/var/" r,
  "/tmp/.snap/var/" rw,
  mount options=(rbind, rw) "/var/" -> "/tmp/.snap/var/",
  mount fstype=tmpfs options=(rw) tmpfs -> "/var/",
  "/tmp/.snap/var/*/" rw,
  "/var/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/var/*/" -> "/var/*/",
  "/tmp/.snap/var/*" rw,
  "/var/*" rw,
  mount options=(bind, rw) "/tmp/.snap/var/*" -> "/var/*",
  mount options=(rprivate) -> "/tmp/.snap/var/",
  umount "/tmp/.snap/var/",
  mount options=(rprivate) -> "/var/",
  mount options=(rprivate) -> "/var/*",
  mount options=(rprivate) -> "/var/*/",
  umount "/var/",
  umount "/var/*",
  umount "/var/*/",
  # .. variant with mimic at /var/cache/
  "/var/cache/" r,
  "/tmp/.snap/var/cache/" rw,
  mount options=(rbind, rw) "/var/cache/" -> "/tmp/.snap/var/cache/",
  mount fstype=tmpfs options=(rw) tmpfs -> "/var/cache/",
  "/tmp/.snap/var/cache/*/" rw,
  "/var/cache/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/var/cache/*/" -> "/var/cache/*/",
  "/tmp/.snap/var/cache/*" rw,
  "/var/cache/*" rw,
  mount options=(bind, rw) "/tmp/.snap/var/cache/*" -> "/var/cache/*",
  mount options=(rprivate) -> "/tmp/.snap/var/cache/",
  umount "/tmp/.snap/var/cache/",
  mount options=(rprivate) -> "/var/cache/",
  mount options=(rprivate) -> "/var/cache/*",
  mount options=(rprivate) -> "/var/cache/*/",
  umount "/var/cache/",
  umount "/var/cache/*",
  umount "/var/cache/*/",
`
	// Find the slice that describes profile2 by looking for the first unique
	// line of the next profile.
	start = end
	end, _ = s.spec.UpdateNSIndexOf("  # Layout /var/tmp: type tmpfs, mode: 01777\n")
	c.Assert(strings.Join(updateNS[start:end], ""), Equals, profile2)

	profile3 := `  # Layout /var/tmp: type tmpfs, mode: 01777
  mount fstype=tmpfs tmpfs -> "/var/tmp/",
  mount options=(rprivate) -> "/var/tmp/",
  umount "/var/tmp/",
  # Writable mimic /var
`
	// Find the slice that describes profile2 by looking till the end of the list.
	start = end
	c.Assert(strings.Join(updateNS[start:], ""), Equals, profile3)
	c.Assert(strings.Join(updateNS, ""), DeepEquals, strings.Join([]string{profile0, profile1, profile2, profile3}, ""))
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

func (s *specSuite) TestApparmorExtraLayouts(c *C) {
	snapInfo := snaptest.MockInfo(c, snapTrivial, &snap.SideInfo{Revision: snap.R(42)})
	snapInfo.InstanceKey = "instance"

	restore := apparmor.SetSpecScope(s.spec, []string{"snap.some-snap_instace.app"})
	defer restore()

	extraLayouts := []snap.Layout{
		{
			Path: "/test",
			Bind: "/usr/home/test",
			Mode: 0755,
		},
	}

	s.spec.AddExtraLayouts(snapInfo, extraLayouts)

	updateNS := s.spec.UpdateNS()

	// verify that updateNS does indeed add all the additional layout
	// lines. This just so happens to be 10 in this case because of reverse
	// traversal for the path /usr/home/test
	c.Assert(updateNS, HasLen, 10)

	// make sure the extra layout is added
	c.Assert(updateNS[0], Equals, "  # Layout /test: bind /usr/home/test\n")
	c.Assert(updateNS[1], Equals, "  mount options=(rbind, rw) \"/usr/home/test/\" -> \"/test/\",\n")
	c.Assert(updateNS[2], Equals, "  mount options=(rprivate) -> \"/test/\",\n")
	// lines 3..9 is the traversal of the prefix for /usr/home/test
}

func (s *specSuite) TestAddEnsureDirMounts(c *C) {
	ensureDirSpecs := []*interfaces.EnsureDirSpec{
		{MustExistDir: "$HOME", EnsureDir: "$HOME/.local/share"},
		{MustExistDir: "$HOME", EnsureDir: "$HOME/dir1/dir2"},
		{MustExistDir: "/", EnsureDir: "/dir1/dir2"},
		{MustExistDir: "/dir1", EnsureDir: "/dir1"},
	}
	s.spec.AddEnsureDirMounts("personal-files", ensureDirSpecs)
	c.Check("\n"+strings.Join(s.spec.UpdateNS(), "\n"), Equals, `
  # Allow the personal-files interface to create potentially missing directories
  owner @{HOME}/ rw,
  owner @{HOME}/.local/ rw,
  owner @{HOME}/.local/share/ rw,
  owner @{HOME}/dir1/ rw,
  owner @{HOME}/dir1/dir2/ rw,
  owner / rw,
  owner /dir1/ rw,
  owner /dir1/dir2/ rw,`)
}

func (s *specSuite) TestAddEnsureDirMountsReturnsOnDirsMatch(c *C) {
	ensureDirSpecs := []*interfaces.EnsureDirSpec{
		{MustExistDir: "/dir", EnsureDir: "/dir"},
	}
	s.spec.AddEnsureDirMounts("personal-files", ensureDirSpecs)
	c.Check(s.spec.UpdateNS(), HasLen, 0)
}

func (s *specSuite) TestAddEnsureDirMountsReturnsOnPathIteratorError(c *C) {
	ensureDirSpecs := []*interfaces.EnsureDirSpec{
		{MustExistDir: "/dir1", EnsureDir: "/../"},
	}
	s.spec.AddEnsureDirMounts("personal-files", ensureDirSpecs)
	c.Check(s.spec.UpdateNS(), HasLen, 0)
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

func (s *specSuite) TestSetSuppressPycacheDeny(c *C) {
	c.Assert(s.spec.SuppressPycacheDeny(), Equals, false)
	s.spec.SetSuppressPycacheDeny()
	c.Assert(s.spec.SuppressPycacheDeny(), Equals, true)
}

var key1 = apparmor.RegisterSnippetKey("testkey1")
var key2 = apparmor.RegisterSnippetKey("testkey2")

func (s *specSuite) TestPrioritySnippets(c *C) {
	restoreScope1 := apparmor.SetSpecScope(s.spec, []string{"snap.demo.scope1"})
	defer restoreScope1()

	// Test a scope with a normal snippet and prioritized ones
	s.spec.AddSnippet("Test snippet 1")
	s.spec.AddPrioritizedSnippet("Prioritized snippet 1", key1, 0)
	s.spec.AddPrioritizedSnippet("Prioritized snippet 2", key1, 0)
	s.spec.AddPrioritizedSnippet("Prioritized snippet 3", key2, 1)
	s.spec.AddPrioritizedSnippet("Prioritized snippet 4", key2, 2)
	s.spec.AddPrioritizedSnippet("Prioritized snippet 5", key2, 0)

	// Test a scope with only prioritized snippets
	restoreScope2 := apparmor.SetSpecScope(s.spec, []string{"snap.demo.scope2"})
	defer restoreScope2()

	s.spec.AddPrioritizedSnippet("Prioritized snippet 6", key1, 0)
	s.spec.AddPrioritizedSnippet("Prioritized snippet 7", key1, 0)
	s.spec.AddPrioritizedSnippet("Prioritized snippet 8", key2, 1)
	s.spec.AddPrioritizedSnippet("Prioritized snippet 9", key2, 2)
	s.spec.AddPrioritizedSnippet("Prioritized snippet 10", key2, 0)

	snippets := s.spec.SnippetForTag("snap.demo.scope1")
	c.Assert(snippets, testutil.Contains, "Test snippet 1")
	c.Assert(snippets, testutil.Contains, "Prioritized snippet 1")
	c.Assert(snippets, testutil.Contains, "Prioritized snippet 2")
	c.Assert(snippets, Not(testutil.Contains), "Prioritized snippet 3")
	c.Assert(snippets, testutil.Contains, "Prioritized snippet 4")
	c.Assert(snippets, Not(testutil.Contains), "Prioritized snippet 5")

	snippets = s.spec.SnippetForTag("snap.demo.scope2")
	c.Assert(snippets, testutil.Contains, "Prioritized snippet 6")
	c.Assert(snippets, testutil.Contains, "Prioritized snippet 7")
	// Overridden by higher-priority snippet 9 with the same key (key2)
	c.Assert(snippets, Not(testutil.Contains), "Prioritized snippet 8")
	c.Assert(snippets, testutil.Contains, "Prioritized snippet 9")
	// Overridden by higher-priority snippet 9 with the same key (key2)
	c.Assert(snippets, Not(testutil.Contains), "Prioritized snippet 10")

	tags := s.spec.SecurityTags()
	c.Assert(tags, testutil.Contains, "snap.demo.scope1")
	c.Assert(tags, testutil.Contains, "snap.demo.scope2")
}

func (s *specSuite) TestPrioritySnippetsNoRegisteredKey(c *C) {
	var key1 apparmor.SnippetKey = apparmor.SnippetKey{}
	c.Assert(func() { s.spec.AddPrioritizedSnippet("Prioritized snippet 1", key1, 0) }, PanicMatches, "priority key  is not registered")
}

func (s *specSuite) TestRegisterSameSnippetKeyTwice(c *C) {
	c.Assert(func() { apparmor.RegisterSnippetKey("testkey1") }, PanicMatches, "priority key testkey1 is already registered")
}

func (s *specSuite) TestMoreSnippets(c *C) {
	keylist := apparmor.RegisteredSnippetKeys()
	c.Assert(keylist, testutil.Contains, "testkey1")
	c.Assert(keylist, testutil.Contains, "testkey2")
	c.Assert(len(keylist), Equals, 2)

}
