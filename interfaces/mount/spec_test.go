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
			return spec.AddMountEntry(mount.Entry{Dir: "dir-a", Name: "connected-plug"})
		},
		MountConnectedSlotCallback: func(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			return spec.AddMountEntry(mount.Entry{Dir: "dir-b", Name: "connected-slot"})
		},
		MountPermanentPlugCallback: func(spec *mount.Specification, plug *snap.PlugInfo) error {
			return spec.AddMountEntry(mount.Entry{Dir: "dir-c", Name: "permanent-plug"})
		},
		MountPermanentSlotCallback: func(spec *mount.Specification, slot *snap.SlotInfo) error {
			return spec.AddMountEntry(mount.Entry{Dir: "dir-d", Name: "permanent-slot"})
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

// AddMountEntry is not broken
func (s *specSuite) TestSmoke(c *C) {
	ent0 := mount.Entry{Dir: "dir-a", Name: "fs1"}
	ent1 := mount.Entry{Dir: "dir-b", Name: "fs2"}
	c.Assert(s.spec.AddMountEntry(ent0), IsNil)
	c.Assert(s.spec.AddMountEntry(ent1), IsNil)
	c.Assert(s.spec.MountEntries(), DeepEquals, []mount.Entry{ent0, ent1})
}

// Added entries can clash and are automatically renamed by MountEntries
func (s *specSuite) TestMountEntriesDeclash(c *C) {
	buf, restore := logger.MockLogger()
	defer restore()
	c.Assert(s.spec.AddMountEntry(mount.Entry{Dir: "foo", Name: "fs1"}), IsNil)
	c.Assert(s.spec.AddMountEntry(mount.Entry{Dir: "foo", Name: "fs2"}), IsNil)
	c.Assert(s.spec.MountEntries(), DeepEquals, []mount.Entry{
		{Dir: "foo", Name: "fs1"},
		{Dir: "foo-2", Name: "fs2"},
	})
	// extract the relevant part of the log
	msg := strings.SplitAfter(strings.TrimSpace(buf.String()), ": ")[1]
	c.Assert(msg, Equals, `renaming mount entry for directory "foo" to "foo-2" to avoid a clash`)
}

// The mount.Specification can be used through the interfaces.Specification interface
func (s *specSuite) TestSpecificationIface(c *C) {
	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(r.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(s.spec.MountEntries(), DeepEquals, []mount.Entry{
		{Dir: "dir-a", Name: "connected-plug"},
		{Dir: "dir-b", Name: "connected-slot"},
		{Dir: "dir-c", Name: "permanent-plug"},
		{Dir: "dir-d", Name: "permanent-slot"}})
}

const snapWithLayout = `
name: vanguard
layout:
  /usr:
    bind: $SNAP/usr
  /mytmp:
    type: tmpfs
    mode: 1777
  /mylink:
    symlink: /link/target
`

func (s *specSuite) TestMountEntryFromLayout(c *C) {
	snapInfo := snaptest.MockInfo(c, snapWithLayout, &snap.SideInfo{Revision: snap.R(42)})
	s.spec.AddSnapLayout(snapInfo)
	c.Assert(s.spec.MountEntries(), DeepEquals, []mount.Entry{
		{Dir: "/mylink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/link/target"}},
		{Name: "tmpfs", Dir: "/mytmp", Type: "tmpfs", Options: []string{"x-snapd.mode=01777"}},
		{Name: "/snap/vanguard/42/usr", Dir: "/usr", Options: []string{"bind", "rw"}},
	})
}
