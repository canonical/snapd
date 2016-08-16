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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/boot"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
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

	err = ioutil.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), nil, 0644)
	c.Assert(err, IsNil)
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

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: foo
   revision: 128
   snap-id: snapidsnapid
   developer-id: developerid
   file: %s
`, filepath.Base(targetSnapFile)))
	err = ioutil.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644)
	c.Assert(err, IsNil)

	// run the firstboot stuff
	err = boot.PopulateStateFromInstalled()
	c.Assert(err, IsNil)

	// and check the snap got correctly installed
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapSnapsDir, "foo", "128", "meta", "snap.yaml")), Equals, true)

	// verify
	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	state, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	state.Lock()
	defer state.Unlock()
	info, err := snapstate.CurrentInfo(state, "foo")
	c.Assert(err, IsNil)
	c.Assert(info.SideInfo.SnapID, Equals, "snapidsnapid")
	c.Assert(info.SideInfo.DeveloperID, Equals, "developerid")
}

func (s *FirstBootTestSuite) TestFirstBootOnClassicNoEnableEther(c *C) {
	release.MockOnClassic(true)
	firstBootEnableFirstEtherRun := false
	restore := boot.MockFirstbootEnableFirstEther(func() error {
		firstBootEnableFirstEtherRun = true
		return nil
	})
	defer restore()

	c.Assert(boot.FirstBoot(), IsNil)
	c.Assert(firstBootEnableFirstEtherRun, Equals, false)
}

func (s *FirstBootTestSuite) TestFirstBootnableEther(c *C) {
	release.MockOnClassic(false)
	firstBootEnableFirstEtherRun := false
	restore := boot.MockFirstbootEnableFirstEther(func() error {
		firstBootEnableFirstEtherRun = true
		return nil
	})
	defer restore()

	c.Assert(boot.FirstBoot(), IsNil)
	c.Assert(firstBootEnableFirstEtherRun, Equals, true)
}
