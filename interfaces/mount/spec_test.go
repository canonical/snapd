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
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil)
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil)
}

// AddMountEntry and AddUserMountEntry are not not broken
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
	c.Assert(s.spec.UserMountEntries(), DeepEquals, []osutil.MountEntry{
		{Dir: "bar", Name: "fs1"},
		{Dir: "bar-2", Name: "fs2"},
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
		{Dir: "dir-d", Name: "permanent-slot"}})
}

const snapWithLayout = `
name: vanguard
version: 0
layout:
  /usr:
    bind: $SNAP/usr
  /mytmp:
    type: tmpfs
    mode: 1777
  /mylink:
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
		{Dir: "/mylink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/snap/vanguard/42/link/target", "x-snapd.origin=layout"}},
		{Dir: "/mytmp", Name: "tmpfs", Type: "tmpfs", Options: []string{"x-snapd.mode=01777", "x-snapd.origin=layout"}},
		{Dir: "/usr", Name: "/snap/vanguard/42/usr", Options: []string{"rbind", "rw", "x-snapd.origin=layout"}},
	})
}

func (s *specSuite) TestSpecificationUberclash(c *C) {
	// When everything clashes for access to /foo, what happens?
	const uberclashYaml = `name: uberclash
version: 0
layout:
  /foo:
    type: tmpfs
`
	snapInfo := snaptest.MockInfo(c, uberclashYaml, &snap.SideInfo{Revision: snap.R(42)})
	entry := osutil.MountEntry{Dir: "/foo", Type: "tmpfs", Name: "tmpfs"}
	s.spec.AddMountEntry(entry)
	s.spec.AddUserMountEntry(entry)
	s.spec.AddLayout(snapInfo)
	c.Assert(s.spec.MountEntries(), DeepEquals, []osutil.MountEntry{
		{Dir: "/foo", Type: "tmpfs", Name: "tmpfs", Options: []string{"x-snapd.origin=layout"}},
		// This is the non-layout entry, it was renamed to "foo-2"
		{Dir: "/foo-2", Type: "tmpfs", Name: "tmpfs"},
	})
	c.Assert(s.spec.UserMountEntries(), DeepEquals, []osutil.MountEntry{
		// This is the user entry, it was _not_ renamed and it would clash with
		// /foo but there is no way to request things like that for now.
		{Dir: "/foo", Type: "tmpfs", Name: "tmpfs"},
	})
}
