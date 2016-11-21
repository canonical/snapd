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
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
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
const sampleContents = ""

type snapTestSuite struct{}

var _ = Suite(&snapTestSuite{})

func (s *snapTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *snapTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *snapTestSuite) TestMockSnap(c *C) {
	snapInfo := snaptest.MockSnap(c, sampleYaml, sampleContents, &snap.SideInfo{Revision: snap.R(42)})
	// Data from YAML is used
	c.Check(snapInfo.Name(), Equals, "sample")
	// Data from SideInfo is used
	c.Check(snapInfo.Revision, Equals, snap.R(42))
	// The YAML is placed on disk
	cont, err := ioutil.ReadFile(filepath.Join(dirs.SnapMountDir, "sample", "42", "meta", "snap.yaml"))
	c.Assert(err, IsNil)

	c.Check(string(cont), Equals, sampleYaml)

	// More
	c.Check(snapInfo.Apps["app"].Command, Equals, "foo")
	c.Check(snapInfo.Plugs["network"].Interface, Equals, "network")
}

func (s *snapTestSuite) TestMockInfo(c *C) {
	snapInfo := snaptest.MockInfo(c, sampleYaml, &snap.SideInfo{Revision: snap.R(42)})
	// Data from YAML is used
	c.Check(snapInfo.Name(), Equals, "sample")
	// Data from SideInfo is used
	c.Check(snapInfo.Revision, Equals, snap.R(42))
	// The YAML is *not* placed on disk
	_, err := os.Stat(filepath.Join(dirs.SnapMountDir, "sample", "42", "meta", "snap.yaml"))
	c.Assert(os.IsNotExist(err), Equals, true)
	// More
	c.Check(snapInfo.Apps["app"].Command, Equals, "foo")
	c.Check(snapInfo.Plugs["network"].Interface, Equals, "network")
}
