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

package boot_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/boot"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type FirstBootTestSuite struct {
	systemctl *testutil.MockCmd
}

var _ = Suite(&FirstBootTestSuite{})

func (s *FirstBootTestSuite) SetUpTest(c *C) {
	tempdir := c.MkDir()
	dirs.SetRootDir(tempdir)

	// mock the world!
	err := os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "snaps"), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	os.Setenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS", "1")
	s.systemctl = testutil.MockCommand(c, "systemctl", "")
}

func (s *FirstBootTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
	os.Unsetenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS")
	s.systemctl.Restore()
}

func (s *FirstBootTestSuite) TestTwoRuns(c *C) {
	c.Assert(boot.FirstBoot(), IsNil)
	_, err := os.Stat(dirs.SnapFirstBootStamp)
	c.Assert(err, IsNil)

	c.Assert(boot.FirstBoot(), Equals, boot.ErrNotFirstBoot)
}

func (s *FirstBootTestSuite) TestNoErrorWhenNoGadget(c *C) {
	c.Assert(boot.FirstBoot(), IsNil)
	_, err := os.Stat(dirs.SnapFirstBootStamp)
	c.Assert(err, IsNil)
}

func (s *FirstBootTestSuite) TestPopulateFromInstalledErrorsOnState(c *C) {
	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	err = ioutil.WriteFile(dirs.SnapStateFile, nil, 0644)
	c.Assert(err, IsNil)

	err = boot.PopulateStateFromInstalled()
	c.Assert(err, ErrorMatches, "cannot create state: state .* already exists")
}

func (s *FirstBootTestSuite) TestPopulateFromInstalledSimpleNoSideInfo(c *C) {
	// put a firstboot snap into the SnapBlobDir
	snapYaml := `name: foo
version: 1.0`
	mockSnapFile := snaptest.MakeTestSnapWithFiles(c, snapYaml, nil)
	targetSnapFile := filepath.Join(dirs.SnapSeedDir, "snaps", filepath.Base(mockSnapFile))
	err := os.Rename(mockSnapFile, targetSnapFile)
	c.Assert(err, IsNil)

	// run the firstboot stuff
	err = boot.PopulateStateFromInstalled()
	c.Assert(err, IsNil)

	// and check the snap got correctly installed
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapSnapsDir, "foo", "x1", "meta", "snap.yaml")), Equals, true)
}

func (s *FirstBootTestSuite) TestPopulateFromInstalledErrors(c *C) {
	// put a broken firstboot snap into the SnapBlobDir
	//
	// (type: kernel without meta/kernel.yaml triggers an error on install)
	snapYaml := `name: illegal-type
type: kernel
version: 1.0`
	mockSnapFile := snaptest.MakeTestSnapWithFiles(c, snapYaml, nil)
	targetSnapFile := filepath.Join(dirs.SnapSeedDir, "snaps", filepath.Base(mockSnapFile))
	err := os.Rename(mockSnapFile, targetSnapFile)
	c.Assert(err, IsNil)

	// run the firstboot stuff
	err = overlord.PopulateStateFromInstalled()
	c.Assert(err, ErrorMatches, "(?ms).*cannot read kernel snap details.*")

}
