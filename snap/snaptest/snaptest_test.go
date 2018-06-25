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
	"github.com/snapcore/snapd/snap/snaptest"
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
	c.Check(snapInfo.StoreName(), Equals, "sample")
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
	c.Check(link, Equals, filepath.Join(dirs.SnapMountDir, "sample", "42"))
}

func (s *snapTestSuite) TestMockSnapInstanceCurrent(c *C) {
	snapInfo := snaptest.MockSnapInstanceCurrent(c, "sample_instance", sampleYaml, &snap.SideInfo{Revision: snap.R(42)})
	// Data from YAML and parameters is used
	c.Check(snapInfo.InstanceName(), Equals, "sample_instance")
	c.Check(snapInfo.StoreName(), Equals, "sample")
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
