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

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/snaptest"
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
	snapInfo := snaptest.MockSnap(c, sampleYaml, &snap.SideInfo{Revision: 42})
	// Data from YAML is used
	c.Check(snapInfo.Name(), Equals, "sample")
	// Data from SideInfo is used
	c.Check(snapInfo.Revision, Equals, 42)
	// The YAML is placed on disk
	stat, err := os.Stat(filepath.Join(dirs.SnapSnapsDir, "sample", "42", "meta", "snap.yaml"))
	c.Assert(err, IsNil)
	c.Check(stat.Size(), Equals, int64(len(sampleYaml)))
}
