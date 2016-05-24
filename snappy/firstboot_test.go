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

package snappy

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/systemd"
)

type FirstBootTestSuite struct {
	gadgetConfig map[string]interface{}
	globs        []string
	ethdir       string
	ifup         string
	e            error
	snapMap      map[string]*Snap
	snapMapErr   error
}

var _ = Suite(&FirstBootTestSuite{})

func (s *FirstBootTestSuite) SetUpTest(c *C) {
	tempdir := c.MkDir()
	dirs.SetRootDir(tempdir)
	os.MkdirAll(dirs.SnapSnapsDir, 0755)
	stampFile = filepath.Join(c.MkDir(), "stamp")

	// mock the world!
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}

	err := os.MkdirAll(filepath.Join(tempdir, "etc", "systemd", "system", "multi-user.target.wants"), 0755)
	c.Assert(err, IsNil)

	s.globs = globs
	globs = nil
	s.ethdir = ethdir
	ethdir = c.MkDir()
	s.ifup = ifup
	ifup = "/bin/true"
	newSnapMap = s.newSnapMap

	s.e = nil
	s.snapMap = nil
	s.snapMapErr = nil
}

func (s *FirstBootTestSuite) TearDownTest(c *C) {
	globs = s.globs
	ethdir = s.ethdir
	ifup = s.ifup
	newSnapMap = newSnapMapImpl
}

func (s *FirstBootTestSuite) newSnapMap() (map[string]*Snap, error) {
	return s.snapMap, s.snapMapErr
}

func (s *FirstBootTestSuite) TestTwoRuns(c *C) {
	c.Assert(FirstBoot(), IsNil)
	_, err := os.Stat(stampFile)
	c.Assert(err, IsNil)

	c.Assert(FirstBoot(), Equals, ErrNotFirstBoot)
}

func (s *FirstBootTestSuite) TestNoErrorWhenNoGadget(c *C) {
	c.Assert(FirstBoot(), IsNil)
	_, err := os.Stat(stampFile)
	c.Assert(err, IsNil)
}

func (s *FirstBootTestSuite) TestEnableFirstEther(c *C) {
	c.Check(enableFirstEther(), IsNil)
	fs, _ := filepath.Glob(filepath.Join(ethdir, "*"))
	c.Assert(fs, HasLen, 0)
}

func (s *FirstBootTestSuite) TestEnableFirstEtherSomeEth(c *C) {
	dir := c.MkDir()
	_, err := os.Create(filepath.Join(dir, "eth42"))
	c.Assert(err, IsNil)

	globs = []string{filepath.Join(dir, "eth*")}
	c.Check(enableFirstEther(), IsNil)
	fs, _ := filepath.Glob(filepath.Join(ethdir, "*"))
	c.Assert(fs, HasLen, 1)
	bs, err := ioutil.ReadFile(fs[0])
	c.Assert(err, IsNil)
	c.Check(string(bs), Equals, "allow-hotplug eth42\niface eth42 inet dhcp\n")

}

func (s *FirstBootTestSuite) TestEnableFirstEtherBadEthDir(c *C) {
	dir := c.MkDir()
	_, err := os.Create(filepath.Join(dir, "eth42"))
	c.Assert(err, IsNil)

	ethdir = "/no/such/thing"
	globs = []string{filepath.Join(dir, "eth*")}
	err = enableFirstEther()
	c.Check(err, NotNil)
	c.Check(os.IsNotExist(err), Equals, true)
}

var mockOSYaml = `
name: ubuntu-core
version: 1.0
type: os
`

var mockKernelYaml = `
name: canonical-linux-pc
version: 1.0
type: kernel
`

func (s *FirstBootTestSuite) ensureSystemSnapIsEnabledOnFirstBoot(c *C, yaml string, expectActivated bool) {
	_, err := makeInstalledMockSnap(yaml, 11)
	c.Assert(err, IsNil)

	all, err := (&Overlord{}).Installed()
	c.Check(err, IsNil)
	c.Assert(all, HasLen, 1)
	c.Check(all[0].IsActive(), Equals, false)

	c.Assert(FirstBoot(), IsNil)

	all, err = (&Overlord{}).Installed()
	c.Check(err, IsNil)
	c.Assert(all, HasLen, 1)
	c.Check(all[0].IsActive(), Equals, expectActivated)
}

func (s *FirstBootTestSuite) TestSystemSnapsEnablesOS(c *C) {
	s.ensureSystemSnapIsEnabledOnFirstBoot(c, mockOSYaml, true)
}

func (s *FirstBootTestSuite) TestSystemSnapsEnablesKernel(c *C) {
	s.ensureSystemSnapIsEnabledOnFirstBoot(c, mockKernelYaml, true)
}

func (s *FirstBootTestSuite) TestSystemSnapsDoesEnableApps(c *C) {
	s.ensureSystemSnapIsEnabledOnFirstBoot(c, "", true)
}
