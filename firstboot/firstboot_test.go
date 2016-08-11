// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package firstboot

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

func TestStore(t *testing.T) { TestingT(t) }

type FirstBootTestSuite struct {
	globs        []string
	nplandir     string
	enableConfig []string
	e            error

	udevadm *testutil.MockCmd
}

var _ = Suite(&FirstBootTestSuite{})

func (s *FirstBootTestSuite) SetUpTest(c *C) {
	tempdir := c.MkDir()
	dirs.SetRootDir(tempdir)

	s.globs = globs
	globs = nil
	s.nplandir = nplandir
	nplandir = c.MkDir()
	s.enableConfig = enableConfig
	enableConfig = []string{"/bin/true"}

	s.e = nil
	s.udevadm = testutil.MockCommand(c, "udevadm", "")
}

func (s *FirstBootTestSuite) TearDownTest(c *C) {
	globs = s.globs
	nplandir = s.nplandir
	enableConfig = s.enableConfig
	s.udevadm.Restore()
}

func (s *FirstBootTestSuite) TestEnableFirstEther(c *C) {
	c.Check(EnableFirstEther(), IsNil)
	fs, _ := filepath.Glob(filepath.Join(nplandir, "*"))
	c.Assert(fs, HasLen, 0)
}

func (s *FirstBootTestSuite) TestEnableFirstEtherSomeEth(c *C) {
	dir := c.MkDir()
	_, err := os.Create(filepath.Join(dir, "eth42"))
	c.Assert(err, IsNil)

	globs = []string{filepath.Join(dir, "eth*")}
	c.Check(EnableFirstEther(), IsNil)
	fs, _ := filepath.Glob(filepath.Join(nplandir, "*"))
	c.Assert(fs, HasLen, 1)
	bs, err := ioutil.ReadFile(fs[0])
	c.Assert(err, IsNil)
	c.Check(string(bs), Equals, "network:\n version: 2\n ethernets:\n  eth42:\n   dhcp4: true\n")
}

func (s *FirstBootTestSuite) TestEnableFirstEtherBadNplandir(c *C) {
	dir := c.MkDir()
	_, err := os.Create(filepath.Join(dir, "eth42"))
	c.Assert(err, IsNil)

	nplandir = "/no/such/thing"
	globs = []string{filepath.Join(dir, "eth*")}
	err = EnableFirstEther()
	c.Check(err, NotNil)
	c.Check(os.IsNotExist(err), Equals, true)
}
