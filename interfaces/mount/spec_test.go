// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package mount_test

import (
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type specSuite struct {
	iface    *ifacetest.TestInterface
	spec     *mount.Specification
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
}

var _ = Suite(&specSuite{
	iface: &ifacetest.TestInterface{
		InterfaceName: "test",
		MountConnectedPlugCallback: func(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			return spec.AddMountEntry(osutil.MountEntry{Dir: "dir-a", Name: "connected-plug"})
		},
		MountConnectedSlotCallback: func(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			return spec.AddMountEntry(osutil.MountEntry{Dir: "dir-b", Name: "connected-slot"})
		},
		MountPermanentPlugCallback: func(spec *mount.Specification, plug *snap.PlugInfo) error {
			return spec.AddMountEntry(osutil.MountEntry{Dir: "dir-c", Name: "permanent-plug"})
		},
		MountPermanentSlotCallback: func(spec *mount.Specification, slot *snap.SlotInfo) error {
			return spec.AddMountEntry(osutil.MountEntry{Dir: "dir-d", Name: "permanent-slot"})
		},
	},
	plugInfo: &snap.PlugInfo{
		Snap:      &snap.Info{SuggestedName: "snap"},
		Name:      "name",
		Interface: "test",
	},
	slotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "snap"},
		Name:      "name",
		Interface: "test",
	},
})

func (s *specSuite) SetUpTest(c *C) {
	s.spec = &mount.Specification{}
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
}

// AddMountEntry and AddUserMountEntry are not broken
func (s *specSuite) TestSmoke(c *C) {
	ent0 := osutil.MountEntry{Dir: "dir-a", Name: "fs1"}
	ent1 := osutil.MountEntry{Dir: "dir-b", Name: "fs2"}
	ent2 := osutil.MountEntry{Dir: "dir-c", Name: "fs3"}

	uent0 := osutil.MountEntry{Dir: "per-user-a", Name: "fs1"}
	uent1 := osutil.MountEntry{Dir: "per-user-b", Name: "fs2"}

	c.Assert(s.spec.AddMountEntry(ent0), IsNil)
	c.Assert(s.spec.AddMountEntry(ent1), IsNil)
	c.Assert(s.spec.AddMountEntry(ent2), IsNil)

	c.Assert(s.spec.AddUserMountEntry(uent0), IsNil)
	c.Assert(s.spec.AddUserMountEntry(uent1), IsNil)

	c.Assert(s.spec.MountEntries(), DeepEquals, []osutil.MountEntry{ent0, ent1, ent2})
	c.Assert(s.spec.UserMountEntries(), DeepEquals, []osutil.MountEntry{uent0, uent1})
}

// Added entries can clash and are automatically renamed by MountEntries
func (s *specSuite) TestMountEntriesDeclash(c *C) {
	buf, restore := logger.MockLogger()
	defer restore()

	c.Assert(s.spec.AddMountEntry(osutil.MountEntry{Dir: "foo", Name: "fs1"}), IsNil)
	c.Assert(s.spec.AddMountEntry(osutil.MountEntry{Dir: "foo", Name: "fs2"}), IsNil)
	c.Assert(s.spec.MountEntries(), DeepEquals, []osutil.MountEntry{
		{Dir: "foo", Name: "fs1"},
		{Dir: "foo-2", Name: "fs2"},
	})

	c.Assert(s.spec.AddUserMountEntry(osutil.MountEntry{Dir: "bar", Name: "fs1"}), IsNil)
	c.Assert(s.spec.AddUserMountEntry(osutil.MountEntry{Dir: "bar", Name: "fs2"}), IsNil)
	c.Assert(s.spec.AddUserMountEntry(osutil.MountEntry{Dir: "bar", Name: "", Options: []string{"x-snapd.kind=ensure-dir"}}), IsNil)
	c.Assert(s.spec.AddUserMountEntry(osutil.MountEntry{Dir: "bar", Name: "", Options: []string{"x-snapd.kind=ensure-dir"}}), IsNil)

	c.Assert(s.spec.UserMountEntries(), DeepEquals, []osutil.MountEntry{
		// First entry: leave intact
		{Dir: "bar", Name: "fs1"},
		// Different name: rename
		{Dir: "bar-2", Name: "fs2"},
		// Different name, Kind ensure-dir: leave intact, append to end
		{Dir: "bar", Options: []string{"x-snapd.kind=ensure-dir"}},
		// Same name , Kind ensure-dir: leave intact, append to end
		{Dir: "bar", Options: []string{"x-snapd.kind=ensure-dir"}},
	})

	// extract the relevant part of the log
	loggedMsgs := strings.Split(buf.String(), "\n")
	msg := strings.SplitAfter(strings.TrimSpace(loggedMsgs[0]), ": ")[1]
	c.Assert(msg, Equals, `renaming mount entry for directory "foo" to "foo-2" to avoid a clash`)
	msg = strings.SplitAfter(strings.TrimSpace(loggedMsgs[1]), ": ")[1]
	c.Assert(msg, Equals, `renaming mount entry for directory "bar" to "bar-2" to avoid a clash`)
}

// The mount.Specification can be used through the interfaces.Specification interface
func (s *specSuite) TestSpecificationIface(c *C) {
	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(r.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(s.spec.MountEntries(), DeepEquals, []osutil.MountEntry{
		{Dir: "dir-a", Name: "connected-plug"},
		{Dir: "dir-b", Name: "connected-slot"},
		{Dir: "dir-c", Name: "permanent-plug"},
		{Dir: "dir-d", Name: "permanent-slot"},
	})
}

const snapWithLayout = `
name: vanguard
version: 0
layout:
  /usr:
    bind: $SNAP/usr
  /lib/mytmp:
    type: tmpfs
    mode: 1777
  /lib/mylink:
    symlink: $SNAP/link/target
  /etc/foo.conf:
    bind-file: $SNAP/foo.conf
`

func (s *specSuite) TestMountEntryFromLayout(c *C) {
	snapInfo := snaptest.MockInfo(c, snapWithLayout, &snap.SideInfo{Revision: snap.R(42)})
	s.spec.AddLayout(snapInfo)
	c.Assert(s.spec.MountEntries(), DeepEquals, []osutil.MountEntry{
		// Layout result is sorted by mount path.
		{Dir: "/etc/foo.conf", Name: "/snap/vanguard/42/foo.conf", Options: []string{"bind", "rw", "x-snapd.kind=file", "x-snapd.origin=layout"}},
		{Dir: "/lib/mylink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/snap/vanguard/42/link/target", "x-snapd.origin=layout"}},
		{Dir: "/lib/mytmp", Name: "tmpfs", Type: "tmpfs", Options: []string{"x-snapd.mode=01777", "x-snapd.origin=layout"}},
		{Dir: "/usr", Name: "/snap/vanguard/42/usr", Options: []string{"rbind", "rw", "x-snapd.origin=layout"}},
	})
}

func (s *specSuite) TestMountEntryFromExtraLayouts(c *C) {
	extraLayouts := []snap.Layout{
		{
			Path: "/test",
			Bind: "/usr/home/test",
			Mode: 0755,
		},
	}

	s.spec.AddExtraLayouts(extraLayouts)
	c.Assert(s.spec.MountEntries(), DeepEquals, []osutil.MountEntry{
		{Dir: "/test", Name: "/usr/home/test", Options: []string{"rbind", "rw", "x-snapd.origin=layout"}},
	})
}

func (s *specSuite) TestParallelInstanceMountEntryFromLayout(c *C) {
	snapInfo := snaptest.MockInfo(c, snapWithLayout, &snap.SideInfo{Revision: snap.R(42)})
	snapInfo.InstanceKey = "instance"
	s.spec.AddLayout(snapInfo)
	s.spec.AddOvername(snapInfo)
	c.Assert(s.spec.MountEntries(), DeepEquals, []osutil.MountEntry{
		// Parallel instance mappings come first
		{Dir: "/snap/vanguard", Name: "/snap/vanguard_instance", Options: []string{"rbind", "x-snapd.origin=overname"}},
		{Dir: "/var/snap/vanguard", Name: "/var/snap/vanguard_instance", Options: []string{"rbind", "x-snapd.origin=overname"}},
		// Layout result is sorted by mount path.
		{Dir: "/etc/foo.conf", Name: "/snap/vanguard/42/foo.conf", Options: []string{"bind", "rw", "x-snapd.kind=file", "x-snapd.origin=layout"}},
		{Dir: "/lib/mylink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/snap/vanguard/42/link/target", "x-snapd.origin=layout"}},
		{Dir: "/lib/mytmp", Name: "tmpfs", Type: "tmpfs", Options: []string{"x-snapd.mode=01777", "x-snapd.origin=layout"}},
		{Dir: "/usr", Name: "/snap/vanguard/42/usr", Options: []string{"rbind", "rw", "x-snapd.origin=layout"}},
	})
}

func (s *specSuite) TestSpecificationUberclash(c *C) {
	// When everything clashes for access to /usr/foo, what happens?
	const uberclashYaml = `name: uberclash
version: 0
layout:
  /usr/foo:
    type: tmpfs
`
	snapInfo := snaptest.MockInfo(c, uberclashYaml, &snap.SideInfo{Revision: snap.R(42)})
	entry := osutil.MountEntry{Dir: "/usr/foo", Type: "tmpfs", Name: "tmpfs"}
	s.spec.AddMountEntry(entry)
	s.spec.AddUserMountEntry(entry)
	s.spec.AddLayout(snapInfo)
	c.Assert(s.spec.MountEntries(), DeepEquals, []osutil.MountEntry{
		{Dir: "/usr/foo", Type: "tmpfs", Name: "tmpfs", Options: []string{"x-snapd.origin=layout"}},
		// This is the non-layout entry, it was renamed to "foo-2"
		{Dir: "/usr/foo-2", Type: "tmpfs", Name: "tmpfs"},
	})
	c.Assert(s.spec.UserMountEntries(), DeepEquals, []osutil.MountEntry{
		// This is the user entry, it was _not_ renamed and it would clash with
		// /foo but there is no way to request things like that for now.
		{Dir: "/usr/foo", Type: "tmpfs", Name: "tmpfs"},
	})
}

func (s *specSuite) TestSpecificationMergedClash(c *C) {
	defaultEntry := osutil.MountEntry{
		Dir:  "/usr/foo",
		Type: "tmpfs",
		Name: "/here",
	}
	for _, td := range []struct {
		// Options for all the clashing mount entries
		Options [][]string
		// Expected options for the merged mount entry
		ExpectedOptions []string
	}{
		{
			// If all entries are read-only, the merged entry is also RO
			Options:         [][]string{{"noatime", "ro"}, {"ro"}},
			ExpectedOptions: []string{"noatime", "ro"},
		},
		{
			// If one entry is rbind, the recursiveness is preserved
			Options:         [][]string{{"bind", "rw"}, {"rbind", "ro"}},
			ExpectedOptions: []string{"rbind"},
		},
		{
			// With simple bind, no recursiveness is added
			Options:         [][]string{{"bind", "noatime"}, {"bind", "noexec"}},
			ExpectedOptions: []string{"noatime", "noexec", "bind"},
		},
		{
			// Ordinary flags are preserved
			Options:         [][]string{{"noexec", "noatime"}, {"noatime", "nomand"}, {"nodev"}},
			ExpectedOptions: []string{"noexec", "noatime", "nomand", "nodev"},
		},
	} {
		for _, options := range td.Options {
			entry := defaultEntry
			entry.Options = options
			s.spec.AddMountEntry(entry)
		}
		c.Check(s.spec.MountEntries(), DeepEquals, []osutil.MountEntry{
			{Dir: "/usr/foo", Name: "/here", Type: "tmpfs", Options: td.ExpectedOptions},
		}, Commentf("Clashing entries: %q", td.Options))

		// reset the spec after each iteration, or flags will leak
		s.spec = &mount.Specification{}
	}
}

func (s *specSuite) TestParallelInstanceMountEntriesNoInstanceKey(c *C) {
	snapInfo := &snap.Info{SideInfo: snap.SideInfo{RealName: "foo", Revision: snap.R(42)}}
	s.spec.AddOvername(snapInfo)
	c.Assert(s.spec.MountEntries(), HasLen, 0)
	c.Assert(s.spec.UserMountEntries(), HasLen, 0)
}

func (s *specSuite) TestParallelInstanceMountEntriesReal(c *C) {
	snapInfo := &snap.Info{SideInfo: snap.SideInfo{RealName: "foo", Revision: snap.R(42)}, InstanceKey: "instance"}
	s.spec.AddOvername(snapInfo)
	c.Assert(s.spec.MountEntries(), DeepEquals, []osutil.MountEntry{
		// /snap/foo_instance -> /snap/foo
		{Name: "/snap/foo_instance", Dir: "/snap/foo", Options: []string{"rbind", "x-snapd.origin=overname"}},
		// /var/snap/foo_instance -> /var/snap/foo
		{Name: "/var/snap/foo_instance", Dir: "/var/snap/foo", Options: []string{"rbind", "x-snapd.origin=overname"}},
	})
	c.Assert(s.spec.UserMountEntries(), HasLen, 0)
}

func (s *specSuite) TestAddUserEnsureDirHappy(c *C) {
	ensureDirSpecs := []*interfaces.EnsureDirSpec{
		{MustExistDir: "$HOME", EnsureDir: "$HOME/.local/share"},
		{MustExistDir: "$HOME", EnsureDir: "$HOME/other/other"},
		{MustExistDir: "/dir1", EnsureDir: "/dir1/dir2"},
	}
	mylog.Check(s.spec.AddUserEnsureDirs(ensureDirSpecs))

	c.Assert(s.spec.UserMountEntries(), DeepEquals, []osutil.MountEntry{
		{Dir: "$HOME/.local/share", Options: []string{"x-snapd.kind=ensure-dir", "x-snapd.must-exist-dir=$HOME"}},
		{Dir: "$HOME/other/other", Options: []string{"x-snapd.kind=ensure-dir", "x-snapd.must-exist-dir=$HOME"}},
		{Dir: "/dir1/dir2", Options: []string{"x-snapd.kind=ensure-dir", "x-snapd.must-exist-dir=/dir1"}},
	})
}

func (s *specSuite) TestAddUserEnsureErrorValidate(c *C) {
	ensureDirSpecs := []*interfaces.EnsureDirSpec{
		{MustExistDir: "$HOME", EnsureDir: "$HOME/.local/share"},
		{MustExistDir: "$HOME", EnsureDir: "$HOME/other/other"},
		{MustExistDir: "$SNAP_HOME", EnsureDir: "$SNAP_HOME/dir"},
		{MustExistDir: "/dir1", EnsureDir: "/dir1/dir2"},
	}
	mylog.Check(s.spec.AddUserEnsureDirs(ensureDirSpecs))
	c.Assert(err, ErrorMatches, `internal error: cannot use ensure-dir mount specification: directory that must exist "\$SNAP_HOME" prefix "\$SNAP_HOME" is not allowed`)
}
