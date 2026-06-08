// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package backend_test

import (
	"errors"
	"fmt"
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/systemd/systemdtest"
	"github.com/snapcore/snapd/testutil"
)

type mountunitSuite struct {
	testutil.BaseTest
}

var _ = Suite(&mountunitSuite{})

func (s *mountunitSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *mountunitSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *mountunitSuite) TestAddMountUnit(c *C) {
	s.testAddMountUnit(c, backend.MountUnitFlags{})
}

func (s *mountunitSuite) TestAddBeforeDriversMountUnit(c *C) {
	s.testAddMountUnit(c, backend.MountUnitFlags{StartBeforeDriversLoad: true})
}

func (s *mountunitSuite) testAddMountUnit(c *C, flags backend.MountUnitFlags) {
	expectedErr := errors.New("creation error")

	var sysd *systemdtest.FakeSystemd
	restore := systemd.MockNewSystemd(func(be systemd.Backend, roodDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &systemdtest.FakeSystemd{}
		sysd.EnsureMountUnitFileResult.Err = expectedErr
		return sysd
	})
	defer restore()

	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(13),
		},
		Version:       "1.1",
		Architectures: []string{"all"},
	}
	err := backend.AddMountUnit(info, systemd.New(systemd.SystemMode, progress.Null), flags)
	c.Check(err, Equals, expectedErr)

	// ensure correct parameters
	expectedMountUnitParameters := systemdtest.ParamsForConfigureMountUnitOptions{
		What:               "/var/lib/snapd/snaps/foo_13.snap",
		Fstype:             "squashfs",
		StartBeforeDrivers: flags.StartBeforeDriversLoad,
	}
	c.Check(sysd.ConfigureMountUnitOptionsCalls, DeepEquals, []systemdtest.ParamsForConfigureMountUnitOptions{
		expectedMountUnitParameters,
	})

	expectedParameters := &systemd.MountUnitOptions{
		Lifetime:    systemd.Persistent,
		Description: "Mount unit for foo, revision 13",
		What:        "/var/lib/snapd/snaps/foo_13.snap",
		Where:       fmt.Sprintf("%s/foo/13", dirs.StripRootDir(dirs.SnapMountDir)),
	}
	c.Check(sysd.EnsureMountUnitFileCalls, DeepEquals, []*systemd.MountUnitOptions{
		expectedParameters,
	})
}

func (s *mountunitSuite) TestRemoveMountUnit(c *C) {
	expectedErr := errors.New("removal error")

	var sysd *systemdtest.FakeSystemd
	restore := systemd.MockNewSystemd(func(be systemd.Backend, roodDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &systemdtest.FakeSystemd{}
		sysd.RemoveMountUnitFileResult = expectedErr
		return sysd
	})
	defer restore()

	err := backend.RemoveMountUnit("/some/where", progress.Null)
	c.Check(err, Equals, expectedErr)
	c.Check(sysd.RemoveMountUnitFileCalls, HasLen, 1)
	c.Check(sysd.RemoveMountUnitFileCalls[0], Equals, "/some/where")
}

func (s *mountunitSuite) TestRemoveSnapMountUnitsFailOnList(c *C) {
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(13),
		},
		Version:       "1.1",
		Architectures: []string{"all"},
	}

	expectedErr := errors.New("listing error")

	var sysd *systemdtest.FakeSystemd
	restore := systemd.MockNewSystemd(func(be systemd.Backend, roodDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &systemdtest.FakeSystemd{}
		sysd.ListMountUnitsResult.Err = expectedErr
		return sysd
	})
	defer restore()

	b := backend.Backend{}
	err := b.RemoveContainerMountUnits(info, progress.Null, "", nil)
	c.Check(err, Equals, expectedErr)
	c.Check(sysd.ListMountUnitsCalls, HasLen, 1)
	c.Check(sysd.ListMountUnitsCalls, DeepEquals, []systemdtest.ParamsForListMountUnits{
		{SnapName: "foo", Origin: ""},
	})
	c.Check(sysd.RemoveMountUnitFileCalls, HasLen, 0)
}

func (s *mountunitSuite) TestRemoveSnapMountUnitsFailOnRemoval(c *C) {
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(13),
		},
		Version:       "1.1",
		Architectures: []string{"all"},
	}

	expectedErr := errors.New("removal error")
	returnedMountPoints := []string{"/here", "/and/there"}

	var sysd *systemdtest.FakeSystemd
	restore := systemd.MockNewSystemd(func(be systemd.Backend, roodDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &systemdtest.FakeSystemd{}
		sysd.ListMountUnitsResult.MountPoints = returnedMountPoints
		sysd.RemoveMountUnitFileResult = expectedErr
		return sysd
	})
	defer restore()

	b := backend.Backend{}
	err := b.RemoveContainerMountUnits(info, progress.Null, "", nil)
	c.Check(err, Equals, expectedErr)
	c.Check(sysd.ListMountUnitsCalls, HasLen, 1)
	c.Check(sysd.ListMountUnitsCalls, DeepEquals, []systemdtest.ParamsForListMountUnits{
		{SnapName: "foo", Origin: ""},
	})

	c.Check(sysd.RemoveMountUnitFileCalls, HasLen, 1)
	c.Check(sysd.RemoveMountUnitFileCalls, DeepEquals, []string{"/here"})
}

func (s *mountunitSuite) TestRemoveSnapMountUnitsHappy(c *C) {
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(13),
		},
		Version:       "1.1",
		Architectures: []string{"all"},
	}

	returnedMountPoints := []string{"/here", "/and/there", "/here/too"}

	var sysd *systemdtest.FakeSystemd
	restore := systemd.MockNewSystemd(func(be systemd.Backend, roodDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &systemdtest.FakeSystemd{}
		sysd.ListMountUnitsResult.MountPoints = returnedMountPoints
		return sysd
	})
	defer restore()

	b := backend.Backend{}
	err := b.RemoveContainerMountUnits(info, progress.Null, "", nil)
	c.Check(err, IsNil)
	c.Check(sysd.ListMountUnitsCalls, HasLen, 1)
	c.Check(sysd.ListMountUnitsCalls, DeepEquals, []systemdtest.ParamsForListMountUnits{
		{SnapName: "foo", Origin: ""},
	})

	c.Check(sysd.RemoveMountUnitFileCalls, HasLen, 3)
	c.Check(sysd.RemoveMountUnitFileCalls, DeepEquals, returnedMountPoints)
}

func (s *mountunitSuite) TestRemoveSnapMountUnitsFiltersBaseDirs(c *C) {
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "some-snap",
			Revision: snap.R(1),
		},
	}

	// Only mount points under the specified base dirs should be removed.
	baseDirs := []string{"/var/snap/some-snap/1", "/var/snap/some-snap/common"}
	mountPoints := []string{
		"/var/snap/some-snap/1/target",   // under revision base dir: matched
		"/var/snap/some-snap/common/dir", // under common base dir: matched
		"/var/snap/other-snap/1/target",  // unrelated snap: not matched
	}

	var sysd *systemdtest.FakeSystemd
	restore := systemd.MockNewSystemd(func(be systemd.Backend, rootDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &systemdtest.FakeSystemd{}
		sysd.ListMountUnitsResult.MountPoints = mountPoints
		return sysd
	})
	defer restore()

	b := backend.Backend{}
	err := b.RemoveContainerMountUnits(info, progress.Null, "mount-control", baseDirs)
	c.Assert(err, IsNil)

	c.Check(sysd.ListMountUnitsCalls, DeepEquals, []systemdtest.ParamsForListMountUnits{
		{SnapName: "some-snap", Origin: "mount-control"},
	})
	// Only the two matching mount points should have been removed.
	c.Assert(sysd.RemoveMountUnitFileCalls, HasLen, 2)
	c.Assert(sysd.RemoveMountUnitFileCalls, DeepEquals, []string{
		"/var/snap/some-snap/1/target",
		"/var/snap/some-snap/common/dir",
	})
}

func (s *mountunitSuite) TestIsUnderAnyDirDisallowExactMatch(c *C) {
	disallowExactMatch := backend.IsUnderAnyDirOptions{}
	// Subdirectory matches.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1/bar", []string{"/var/snap/foo/1"}, disallowExactMatch), Equals, true)
	// Trailing slash on path must not affect matching.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1/bar/", []string{"/var/snap/foo/1"}, disallowExactMatch), Equals, true)
	// Trailing slash on candidate must not affect matching.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1/bar", []string{"/var/snap/foo/1/"}, disallowExactMatch), Equals, true)
	// Trailing slash on both must not affect matching.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1/bar/", []string{"/var/snap/foo/1/"}, disallowExactMatch), Equals, true)
	// Path equal to candidate must not match (strict subdirectory check).
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1", []string{"/var/snap/foo/1"}, disallowExactMatch), Equals, false)
	// Path equal to candidate with trailing slash on path must not match.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1/", []string{"/var/snap/foo/1"}, disallowExactMatch), Equals, false)
	// Path equal to candidate with trailing slash on candidate must not match.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1", []string{"/var/snap/foo/1/"}, disallowExactMatch), Equals, false)
	// Unrelated path must not match.
	c.Check(backend.IsUnderAnyDir("/var/snap/other/1/bar", []string{"/var/snap/foo/1"}, disallowExactMatch), Equals, false)
	// Path with ".." components that escape the candidate after cleaning must not match.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1/../../bar", []string{"/var/snap/foo/1"}, disallowExactMatch), Equals, false)
	// Path with internal double slash that normalizes to a subdirectory must match.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo//1/bar", []string{"/var/snap/foo/1"}, disallowExactMatch), Equals, true)
	// Relative candidate causes filepath.Rel to error; must not match.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1/bar", []string{"var/snap/foo/1"}, disallowExactMatch), Equals, false)
}

func (s *mountunitSuite) TestIsUnderAnyDirAllowExactMatch(c *C) {
	allowExactMatch := backend.IsUnderAnyDirOptions{AllowExactMatch: true}
	// Subdirectory still matches.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1/bar", []string{"/var/snap/foo/1"}, allowExactMatch), Equals, true)
	// Path equal to candidate now matches.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1", []string{"/var/snap/foo/1"}, allowExactMatch), Equals, true)
	// Path equal to candidate with trailing slash on path also matches.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1/", []string{"/var/snap/foo/1"}, allowExactMatch), Equals, true)
	// Path equal to candidate with trailing slash on candidate also matches.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1", []string{"/var/snap/foo/1/"}, allowExactMatch), Equals, true)
	// Unrelated path still does not match.
	c.Check(backend.IsUnderAnyDir("/var/snap/other/1/bar", []string{"/var/snap/foo/1"}, allowExactMatch), Equals, false)
}

type listNonMountControlMountsFn func(*snap.Info, *dirs.SnapDirOptions) ([]string, error)

type scope int

const (
	scopeRev scope = iota // tests ListNonMountControlMountsInSnapRevDataDirs
	scopeAll              // tests ListNonMountControlMountsInSnapAllDataDirs
)

func (v scope) fn() listNonMountControlMountsFn {
	b := backend.Backend{}
	switch v {
	case scopeRev:
		return b.ListNonMountControlMountsInSnapRevDataDirs
	case scopeAll:
		return b.ListNonMountControlMountsInSnapAllDataDirs
	}
	return nil
}

func (s *mountunitSuite) testListNonMountControlMountsNoMounts(c *C, variant scope) {
	var sysd *systemdtest.FakeSystemd
	restore := systemd.MockNewSystemd(func(be systemd.Backend, rootDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &systemdtest.FakeSystemd{}
		sysd.ListMountUnitsResult.MountPoints = nil
		return sysd
	})
	defer restore()

	restoreMI := osutil.MockMountInfo("")
	defer restoreMI()

	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "foo", Revision: snap.R(3)}}
	fn := variant.fn()
	mounts, err := fn(info, nil)
	c.Assert(err, IsNil)
	c.Check(mounts, HasLen, 0)
	c.Check(sysd.ListMountUnitsCalls, DeepEquals, []systemdtest.ParamsForListMountUnits{
		{SnapName: "foo", Origin: "mount-control"},
	})
}

func (s *mountunitSuite) TestListNonMountControlMountsRevNoMounts(c *C) {
	s.testListNonMountControlMountsNoMounts(c, scopeRev)
}

func (s *mountunitSuite) TestListNonMountControlMountsAllNoMounts(c *C) {
	s.testListNonMountControlMountsNoMounts(c, scopeAll)
}

func (s *mountunitSuite) testListNonMountControlMountsAllMountControl(c *C, variant scope) {
	// The mount-control mount is placed under current revision dir
	// which lies within the scan scope of both the Rev and All variants.
	snapRevDataDir := fmt.Sprintf("%s/var/snap/foo/3", dirs.GlobalRootDir)
	mcMount := snapRevDataDir + "/mc-mount"

	var sysd *systemdtest.FakeSystemd
	restore := systemd.MockNewSystemd(func(be systemd.Backend, rootDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &systemdtest.FakeSystemd{}
		sysd.ListMountUnitsResult.MountPoints = []string{mcMount}
		return sysd
	})
	defer restore()

	mountInfoContent := fmt.Sprintf("36 1 8:1 / %s rw - ext4 /dev/sda1 rw\n", mcMount)
	restoreMI := osutil.MockMountInfo(mountInfoContent)
	defer restoreMI()

	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "foo", Revision: snap.R(3)}}
	fn := variant.fn()
	mounts, err := fn(info, nil)
	c.Assert(err, IsNil)
	// All mounts are mount-control mounts, so nothing is returned.
	c.Check(mounts, HasLen, 0)
	c.Check(sysd.ListMountUnitsCalls, DeepEquals, []systemdtest.ParamsForListMountUnits{
		{SnapName: "foo", Origin: "mount-control"},
	})
}

func (s *mountunitSuite) TestListNonMountControlMountsRevAllMountControl(c *C) {
	s.testListNonMountControlMountsAllMountControl(c, scopeRev)
}

func (s *mountunitSuite) TestListNonMountControlMountsAllMountControl(c *C) {
	s.testListNonMountControlMountsAllMountControl(c, scopeAll)
}

func (s *mountunitSuite) testListNonMountControlMountsReturnsNonMountControl(c *C, variant scope) {
	// The mount-control and user mounts are placed under current revision dir
	// which lie within the scan scope of both the Rev and All variants.
	snapRevDataDir := fmt.Sprintf("%s/var/snap/foo/3", dirs.GlobalRootDir)
	mcMount := snapRevDataDir + "/mc-mount"
	userMount := snapRevDataDir + "/user-mount"
	unrelatedMount := "/tmp/unrelated"

	var sysd *systemdtest.FakeSystemd
	restore := systemd.MockNewSystemd(func(be systemd.Backend, rootDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &systemdtest.FakeSystemd{}
		sysd.ListMountUnitsResult.MountPoints = []string{mcMount}
		return sysd
	})
	defer restore()

	mountInfoContent := fmt.Sprintf(
		"36 1 8:1 / %s rw - ext4 /dev/sda1 rw\n"+
			"37 1 8:2 / %s rw - ext4 /dev/sda2 rw\n"+
			"38 1 8:3 / %s rw - tmpfs tmpfs rw\n",
		mcMount, userMount, unrelatedMount)
	restoreMI := osutil.MockMountInfo(mountInfoContent)
	defer restoreMI()

	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "foo", Revision: snap.R(3)}}
	fn := variant.fn()
	mounts, err := fn(info, nil)
	c.Assert(err, IsNil)
	// Only the non-mount-control mount is returned.
	c.Check(mounts, DeepEquals, []string{userMount})
	c.Check(sysd.ListMountUnitsCalls, DeepEquals, []systemdtest.ParamsForListMountUnits{
		{SnapName: "foo", Origin: "mount-control"},
	})
}

func (s *mountunitSuite) TestListNonMountControlMountsRevReturnsNonMountControl(c *C) {
	s.testListNonMountControlMountsReturnsNonMountControl(c, scopeRev)
}

func (s *mountunitSuite) TestListNonMountControlMountsAllReturnsNonMountControl(c *C) {
	s.testListNonMountControlMountsReturnsNonMountControl(c, scopeAll)
}

func (s *mountunitSuite) testListNonMountControlMountsCrossRevision(c *C, variant scope) {
	otherRevMount := fmt.Sprintf("%s/var/snap/foo/1/data", dirs.GlobalRootDir)
	currentRevMount := fmt.Sprintf("%s/var/snap/foo/3/data", dirs.GlobalRootDir)

	var expectedMounts []string
	switch variant {
	case scopeRev:
		// Rev only scans the current revision dir, so only currentRevMount is visible.
		expectedMounts = []string{currentRevMount}
	case scopeAll:
		// All scans the snap base dir, so mounts from all revisions are visible.
		expectedMounts = []string{otherRevMount, currentRevMount}
	}

	restore := systemd.MockNewSystemd(func(be systemd.Backend, rootDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		return &systemdtest.FakeSystemd{}
	})
	defer restore()

	mountInfoContent := fmt.Sprintf(
		"36 1 8:1 / %s rw - ext4 /dev/sda1 rw\n"+
			"37 1 8:2 / %s rw - ext4 /dev/sda2 rw\n",
		otherRevMount, currentRevMount)
	restoreMI := osutil.MockMountInfo(mountInfoContent)
	defer restoreMI()

	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "foo", Revision: snap.R(3)}}
	fn := variant.fn()
	mounts, err := fn(info, nil)
	c.Assert(err, IsNil)
	c.Check(mounts, DeepEquals, expectedMounts)
}

func (s *mountunitSuite) TestListNonMountControlMountsRevIgnoresMountsInOtherRevisions(c *C) {
	s.testListNonMountControlMountsCrossRevision(c, scopeRev)
}

func (s *mountunitSuite) TestListNonMountControlMountsAllDetectsAllRevisionMounts(c *C) {
	s.testListNonMountControlMountsCrossRevision(c, scopeAll)
}

func (s *mountunitSuite) testListNonMountControlMountsAtExactDir(c *C, variant scope) {
	var mountPath string
	switch variant {
	case scopeRev:
		mountPath = fmt.Sprintf("%s/var/snap/foo/3", dirs.GlobalRootDir)
	case scopeAll:
		mountPath = fmt.Sprintf("%s/var/snap/foo", dirs.GlobalRootDir)
	}

	restore := systemd.MockNewSystemd(func(be systemd.Backend, rootDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		return &systemdtest.FakeSystemd{}
	})
	defer restore()

	mountInfoContent := fmt.Sprintf("36 1 8:1 / %s rw - ext4 /dev/sda1 rw\n", mountPath)
	restoreMI := osutil.MockMountInfo(mountInfoContent)
	defer restoreMI()

	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "foo", Revision: snap.R(3)}}
	fn := variant.fn()
	mounts, err := fn(info, nil)
	c.Assert(err, IsNil)
	c.Check(mounts, DeepEquals, []string{mountPath})
}

func (s *mountunitSuite) TestListNonMountControlMountsRevAtRevisionDir(c *C) {
	s.testListNonMountControlMountsAtExactDir(c, scopeRev)
}

func (s *mountunitSuite) TestListNonMountControlMountsAllAtBaseDir(c *C) {
	s.testListNonMountControlMountsAtExactDir(c, scopeAll)
}

func (s *mountunitSuite) testListNonMountControlMountsInRootUserDir(c *C, variant scope) {
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}

	var mountPath string
	switch variant {
	case scopeRev:
		mountPath = fmt.Sprintf("%s/root/.snap/data/foo/3/data", dirs.GlobalRootDir)
	case scopeAll:
		mountPath = fmt.Sprintf("%s/root/.snap/data/foo/data", dirs.GlobalRootDir)
	}

	restore := systemd.MockNewSystemd(func(be systemd.Backend, rootDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		return &systemdtest.FakeSystemd{}
	})
	defer restore()

	mountInfoContent := fmt.Sprintf("36 1 8:1 / %s rw - ext4 /dev/sda1 rw\n", mountPath)
	restoreMI := osutil.MockMountInfo(mountInfoContent)
	defer restoreMI()

	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "foo", Revision: snap.R(3)}}
	fn := variant.fn()
	mounts, err := fn(info, opts)
	c.Assert(err, IsNil)
	c.Check(mounts, DeepEquals, []string{mountPath})
}

func (s *mountunitSuite) TestListNonMountControlMountsRevInRootUserDir(c *C) {
	s.testListNonMountControlMountsInRootUserDir(c, scopeRev)
}

func (s *mountunitSuite) TestListNonMountControlMountsAllInRootUserDir(c *C) {
	s.testListNonMountControlMountsInRootUserDir(c, scopeAll)
}

func (s *mountunitSuite) testListNonMountControlMountsInHomeUserDir(c *C, variant scope) {
	var userSnapDir, mountPath string
	switch variant {
	case scopeRev:
		userSnapDir = fmt.Sprintf("%s/home/user1/snap/foo/3", dirs.GlobalRootDir)
		mountPath = userSnapDir + "/data"
	case scopeAll:
		userSnapDir = fmt.Sprintf("%s/home/user1/snap/foo", dirs.GlobalRootDir)
		mountPath = userSnapDir + "/data"
	}
	// Create the user's snap directory so that the glob in snapDataDirs /
	// snapBaseDataDirs expands to it.
	c.Assert(os.MkdirAll(userSnapDir, 0755), IsNil)
	defer os.RemoveAll(userSnapDir)

	restore := systemd.MockNewSystemd(func(be systemd.Backend, rootDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		return &systemdtest.FakeSystemd{}
	})
	defer restore()

	mountInfoContent := fmt.Sprintf("36 1 8:1 / %s rw - ext4 /dev/sda1 rw\n", mountPath)
	restoreMI := osutil.MockMountInfo(mountInfoContent)
	defer restoreMI()

	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "foo", Revision: snap.R(3)}}
	fn := variant.fn()
	mounts, err := fn(info, nil)
	c.Assert(err, IsNil)
	c.Check(mounts, DeepEquals, []string{mountPath})
}

func (s *mountunitSuite) TestListNonMountControlMountsRevInHomeUserDir(c *C) {
	s.testListNonMountControlMountsInHomeUserDir(c, scopeRev)
}

func (s *mountunitSuite) TestListNonMountControlMountsAllInHomeUserDir(c *C) {
	s.testListNonMountControlMountsInHomeUserDir(c, scopeAll)
}

func (s *mountunitSuite) testListNonMountControlMountsErrorOnListMountUnits(c *C, variant scope) {
	expectedErr := errors.New("mock ListMountUnits error")

	restore := systemd.MockNewSystemd(func(be systemd.Backend, rootDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd := &systemdtest.FakeSystemd{}
		sysd.ListMountUnitsResult.Err = expectedErr
		return sysd
	})
	defer restore()

	restoreMI := osutil.MockMountInfo("")
	defer restoreMI()

	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "foo", Revision: snap.R(3)}}
	fn := variant.fn()
	_, err := fn(info, nil)
	c.Check(err, Equals, expectedErr)
}

func (s *mountunitSuite) TestListNonMountControlMountsRevErrorOnListMountUnits(c *C) {
	s.testListNonMountControlMountsErrorOnListMountUnits(c, scopeRev)
}

func (s *mountunitSuite) TestListNonMountControlMountsAllErrorOnListMountUnits(c *C) {
	s.testListNonMountControlMountsErrorOnListMountUnits(c, scopeAll)
}

func (s *mountunitSuite) testListNonMountControlMountsErrorOnLoadMountInfo(c *C, variant scope) {
	restore := systemd.MockNewSystemd(func(be systemd.Backend, rootDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		return &systemdtest.FakeSystemd{}
	})
	defer restore()

	// Inject a mountinfo line with less fields; the parser requires at least 10.
	restoreMI := osutil.MockMountInfo("col1 col2 col3\n")
	defer restoreMI()

	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "foo", Revision: snap.R(3)}}
	fn := variant.fn()
	_, err := fn(info, nil)
	c.Check(err, ErrorMatches, "incorrect number of fields, expected at least 10 but found 3")
}

func (s *mountunitSuite) TestListNonMountControlMountsRevErrorOnLoadMountInfo(c *C) {
	s.testListNonMountControlMountsErrorOnLoadMountInfo(c, scopeRev)
}

func (s *mountunitSuite) TestListNonMountControlMountsAllErrorOnLoadMountInfo(c *C) {
	s.testListNonMountControlMountsErrorOnLoadMountInfo(c, scopeAll)
}
