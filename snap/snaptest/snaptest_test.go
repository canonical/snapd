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

package snaptest_test

import (
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/testutil"
)

func TestSnapTest(t *testing.T) { TestingT(t) }

const sampleYaml = `
name: sample
version: 1
apps:
 app:
   command: foo
plugs:
 network:
  interface: network
`

type snapTestSuite struct{}

var _ = Suite(&snapTestSuite{})

func (s *snapTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *snapTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *snapTestSuite) TestMockSnap(c *C) {
	snapInfo := snaptest.MockSnap(c, sampleYaml, &snap.SideInfo{Revision: snap.R(42)})
	// Data from YAML is used
	c.Check(snapInfo.InstanceName(), Equals, "sample")
	// Data from SideInfo is used
	c.Check(snapInfo.Revision, Equals, snap.R(42))
	// The YAML is placed on disk
	c.Check(filepath.Join(dirs.SnapMountDir, "sample", "42", "meta", "snap.yaml"),
		testutil.FileEquals, sampleYaml)

	// More
	c.Check(snapInfo.Apps["app"].Command, Equals, "foo")
	c.Check(snapInfo.Plugs["network"].Interface, Equals, "network")
}

func (s *snapTestSuite) TestMockSnapInstance(c *C) {
	snapInfo := snaptest.MockSnapInstance(c, "sample_instance", sampleYaml, &snap.SideInfo{Revision: snap.R(42)})
	// Data from YAML and parameters is used
	c.Check(snapInfo.InstanceName(), Equals, "sample_instance")
	c.Check(snapInfo.SnapName(), Equals, "sample")
	c.Check(snapInfo.InstanceKey, Equals, "instance")

	// Data from SideInfo is used
	c.Check(snapInfo.Revision, Equals, snap.R(42))
	// The YAML is placed on disk
	c.Check(filepath.Join(dirs.SnapMountDir, "sample_instance", "42", "meta", "snap.yaml"),
		testutil.FileEquals, sampleYaml)

	// More
	c.Check(snapInfo.Apps["app"].Command, Equals, "foo")
	c.Check(snapInfo.Plugs["network"].Interface, Equals, "network")
}

func (s *snapTestSuite) TestMockSnapCurrent(c *C) {
	snapInfo := snaptest.MockSnapCurrent(c, sampleYaml, &snap.SideInfo{Revision: snap.R(42)})
	// Data from YAML is used
	c.Check(snapInfo.InstanceName(), Equals, "sample")
	// Data from SideInfo is used
	c.Check(snapInfo.Revision, Equals, snap.R(42))
	// The YAML is placed on disk
	c.Check(filepath.Join(dirs.SnapMountDir, "sample", "42", "meta", "snap.yaml"),
		testutil.FileEquals, sampleYaml)
	link, err := os.Readlink(filepath.Join(dirs.SnapMountDir, "sample", "current"))
	c.Check(err, IsNil)
	c.Check(link, Equals, "42")
}

func (s *snapTestSuite) TestMockSnapInstanceCurrent(c *C) {
	snapInfo := snaptest.MockSnapInstanceCurrent(c, "sample_instance", sampleYaml, &snap.SideInfo{Revision: snap.R(42)})
	// Data from YAML and parameters is used
	c.Check(snapInfo.InstanceName(), Equals, "sample_instance")
	c.Check(snapInfo.SnapName(), Equals, "sample")
	c.Check(snapInfo.InstanceKey, Equals, "instance")
	// Data from SideInfo is used
	c.Check(snapInfo.Revision, Equals, snap.R(42))
	// The YAML is placed on disk
	c.Check(filepath.Join(dirs.SnapMountDir, "sample_instance", "42", "meta", "snap.yaml"),
		testutil.FileEquals, sampleYaml)
	link, err := os.Readlink(filepath.Join(dirs.SnapMountDir, "sample_instance", "current"))
	c.Check(err, IsNil)
	c.Check(link, Equals, filepath.Join(dirs.SnapMountDir, "sample_instance", "42"))
}

func (s *snapTestSuite) TestMockInfo(c *C) {
	snapInfo := snaptest.MockInfo(c, sampleYaml, &snap.SideInfo{Revision: snap.R(42)})
	// Data from YAML is used
	c.Check(snapInfo.InstanceName(), Equals, "sample")
	// Data from SideInfo is used
	c.Check(snapInfo.Revision, Equals, snap.R(42))
	// The YAML is *not* placed on disk
	_, err := os.Stat(filepath.Join(dirs.SnapMountDir, "sample", "42", "meta", "snap.yaml"))
	c.Assert(os.IsNotExist(err), Equals, true)
	// More
	c.Check(snapInfo.Apps["app"].Command, Equals, "foo")
	c.Check(snapInfo.Plugs["network"].Interface, Equals, "network")
}

func (s *snapTestSuite) TestMockInvalidInfo(c *C) {
	snapInfo := snaptest.MockInvalidInfo(c, sampleYaml+"\nslots:\n network:\n", &snap.SideInfo{Revision: snap.R(42)})
	// Data from YAML is used
	c.Check(snapInfo.InstanceName(), Equals, "sample")
	// Data from SideInfo is used
	c.Check(snapInfo.Revision, Equals, snap.R(42))
	// The YAML is *not* placed on disk
	_, err := os.Stat(filepath.Join(dirs.SnapMountDir, "sample", "42", "meta", "snap.yaml"))
	c.Assert(os.IsNotExist(err), Equals, true)
	// More
	c.Check(snapInfo.Apps["app"].Command, Equals, "foo")
	c.Check(snapInfo.Plugs["network"].Interface, Equals, "network")
	c.Check(snapInfo.Slots["network"].Interface, Equals, "network")
	// They info object is not valid
	c.Check(snap.Validate(snapInfo), ErrorMatches, `cannot have plug and slot with the same name: "network"`)
}

func (s *snapTestSuite) TestRenameSlot(c *C) {
	snapInfo := snaptest.MockInfo(c, `name: core
type: os
version: 0
slots:
  old:
    interface: interface
  slot:
    interface: interface
plugs:
  plug:
    interface: interface
apps:
  app:
hooks:
  configure:
`, nil)

	// Rename "old" to "plug"
	err := snaptest.RenameSlot(snapInfo, "old", "plug")
	c.Assert(err, ErrorMatches, `cannot rename slot "old" to "plug": existing plug with that name`)

	// Rename "old" to "slot"
	err = snaptest.RenameSlot(snapInfo, "old", "slot")
	c.Assert(err, ErrorMatches, `cannot rename slot "old" to "slot": existing slot with that name`)

	// Rename "old" to "bad name"
	err = snaptest.RenameSlot(snapInfo, "old", "bad name")
	c.Assert(err, ErrorMatches, `cannot rename slot "old" to "bad name": invalid slot name: "bad name"`)

	// Rename "old" to "new"
	err = snaptest.RenameSlot(snapInfo, "old", "new")
	c.Assert(err, IsNil)

	// Check that there's no trace of the old slot name.
	c.Assert(snapInfo.Slots["old"], IsNil)
	c.Assert(snapInfo.Slots["new"], NotNil)
	c.Assert(snapInfo.Slots["new"].Name, Equals, "new")
	c.Assert(snapInfo.Apps["app"].Slots["old"], IsNil)
	c.Assert(snapInfo.Apps["app"].Slots["new"], DeepEquals, snapInfo.Slots["new"])
	c.Assert(snapInfo.Hooks["configure"].Slots["old"], IsNil)
	c.Assert(snapInfo.Hooks["configure"].Slots["new"], DeepEquals, snapInfo.Slots["new"])

	// Rename "old" to "new" (again)
	err = snaptest.RenameSlot(snapInfo, "old", "new")
	c.Assert(err, ErrorMatches, `cannot rename slot "old" to "new": no such slot`)

}

func (s *snapTestSuite) TestMockSnapWithFiles(c *C) {
	snapInfo := snaptest.MockSnapWithFiles(c, sampleYaml, &snap.SideInfo{Revision: snap.R(42)}, [][]string{
		{"foo/bar", "bar"},
		{"bar", "not foo"},
		{"meta/gadget.yaml", "gadget yaml\nmulti line"},
	})
	// Data from YAML is used
	c.Check(snapInfo.InstanceName(), Equals, "sample")
	// Data from SideInfo is used
	c.Check(snapInfo.Revision, Equals, snap.R(42))
	c.Check(filepath.Join(snapInfo.MountDir(), "bar"), testutil.FileEquals, "not foo")
	c.Check(filepath.Join(snapInfo.MountDir(), "foo/bar"), testutil.FileEquals, "bar")
	c.Check(filepath.Join(snapInfo.MountDir(), "meta/gadget.yaml"), testutil.FileEquals, "gadget yaml\nmulti line")
}

func (s *snapTestSuite) TestMockContainerMinimal(c *C) {
	cont := snaptest.MockContainer(c, nil)
	err := snap.ValidateSnapContainer(cont, &snap.Info{}, nil)
	c.Check(err, IsNil)
}

func (s *snapTestSuite) TestMockContainer(c *C) {
	defer snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})()

	const snapYaml = `name: gadget
type: gadget
version: 1.0`
	const gadgetYaml = `defaults:
`
	cont := snaptest.MockContainer(c, [][]string{
		{"meta/snap.yaml", snapYaml},
		{"meta/gadget.yaml", gadgetYaml},
	})

	info, err := snap.ReadInfoFromSnapFile(cont, nil)
	c.Assert(err, IsNil)
	c.Check(info.SnapName(), Equals, "gadget")
	err = snap.ValidateSnapContainer(cont, info, nil)
	c.Assert(err, IsNil)
	readGadgetYaml, err := cont.ReadFile("meta/gadget.yaml")
	c.Assert(err, IsNil)
	c.Check(readGadgetYaml, DeepEquals, []byte(gadgetYaml))
}

func (s *snapTestSuite) TestMakeSnapFileAndDir(c *C) {
	files := [][]string{
		{"canary", "canary"},
		{"foo", "foo"},
	}
	info := snaptest.MakeSnapFileAndDir(c, sampleYaml, files, &snap.SideInfo{
		Revision: snap.R(3),
	})
	c.Check(filepath.Join(info.MountDir(), "canary"), testutil.FileEquals, "canary")
	c.Assert(info.MountFile(), testutil.FilePresent)
	c.Check(squashfs.FileHasSquashfsHeader(info.MountFile()), Equals, true)
	f, err := snapfile.Open(info.MountFile())
	c.Assert(err, IsNil)
	can, err := f.ReadFile("canary")
	c.Assert(err, IsNil)
	c.Check(can, DeepEquals, []byte("canary"))
}
