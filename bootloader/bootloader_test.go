// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package bootloader_test

import (
	"errors"
	"io/ioutil"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

const packageKernel = `
name: ubuntu-kernel
version: 4.0-1
type: kernel
vendor: Someone
`

type baseBootenvTestSuite struct {
	testutil.BaseTest
}

func (s *baseBootenvTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

type bootenvTestSuite struct {
	baseBootenvTestSuite

	b *bootloadertest.MockBootloader
}

var _ = Suite(&bootenvTestSuite{})

func (s *bootenvTestSuite) SetUpTest(c *C) {
	s.baseBootenvTestSuite.SetUpTest(c)

	s.b = bootloadertest.Mock("mocky", c.MkDir())
}

func (s *bootenvTestSuite) TestForceBootloader(c *C) {
	bootloader.Force(s.b)
	defer bootloader.Force(nil)

	got, err := bootloader.Find()
	c.Assert(err, IsNil)
	c.Check(got, Equals, s.b)
}

func (s *bootenvTestSuite) TestForceBootloaderError(c *C) {
	myErr := errors.New("zap")
	bootloader.ForceError(myErr)
	defer bootloader.ForceError(nil)

	got, err := bootloader.Find()
	c.Assert(err, Equals, myErr)
	c.Check(got, IsNil)
}

func (s *bootenvTestSuite) TestMarkBootSuccessfulAllSnap(c *C) {
	s.b.BootVars["snap_mode"] = "trying"
	s.b.BootVars["snap_try_core"] = "os1"
	s.b.BootVars["snap_try_kernel"] = "k1"
	err := bootloader.MarkBootSuccessful(s.b)
	c.Assert(err, IsNil)

	expected := map[string]string{
		// cleared
		"snap_mode":       "",
		"snap_try_kernel": "",
		"snap_try_core":   "",
		// updated
		"snap_kernel": "k1",
		"snap_core":   "os1",
	}
	c.Assert(s.b.BootVars, DeepEquals, expected)

	// do it again, verify its still valid
	err = bootloader.MarkBootSuccessful(s.b)
	c.Assert(err, IsNil)
	c.Assert(s.b.BootVars, DeepEquals, expected)
}

func (s *bootenvTestSuite) TestMarkBootSuccessfulKKernelUpdate(c *C) {
	s.b.BootVars["snap_mode"] = "trying"
	s.b.BootVars["snap_core"] = "os1"
	s.b.BootVars["snap_kernel"] = "k1"
	s.b.BootVars["snap_try_core"] = ""
	s.b.BootVars["snap_try_kernel"] = "k2"
	err := bootloader.MarkBootSuccessful(s.b)
	c.Assert(err, IsNil)
	c.Assert(s.b.BootVars, DeepEquals, map[string]string{
		// cleared
		"snap_mode":       "",
		"snap_try_kernel": "",
		"snap_try_core":   "",
		// unchanged
		"snap_core": "os1",
		// updated
		"snap_kernel": "k2",
	})
}

func (s *bootenvTestSuite) TestInstallBootloaderConfigNoConfig(c *C) {
	err := bootloader.InstallBootConfig(c.MkDir())
	c.Assert(err, ErrorMatches, `cannot find boot config in.*`)
}

func (s *bootenvTestSuite) TestInstallBootloaderConfig(c *C) {
	for _, t := range []struct{ gadgetFile, systemFile string }{
		{"grub.conf", "/boot/grub/grub.cfg"},
		{"uboot.conf", "/boot/uboot/uboot.env"},
		{"androidboot.conf", "/boot/androidboot/androidboot.env"},
	} {
		mockGadgetDir := c.MkDir()
		err := ioutil.WriteFile(filepath.Join(mockGadgetDir, t.gadgetFile), nil, 0644)
		c.Assert(err, IsNil)
		err = bootloader.InstallBootConfig(mockGadgetDir)
		c.Assert(err, IsNil)
		fn := filepath.Join(dirs.GlobalRootDir, t.systemFile)
		c.Assert(osutil.FileExists(fn), Equals, true)
	}
}

func (s *bootenvTestSuite) TestSetNextBootError(c *C) {
	bootloader.Force(s.b)
	defer bootloader.Force(nil)

	s.b.GetErr = errors.New("zap")
	err := bootloader.SetNextBoot(&snap.Info{}, snap.TypeApp)
	c.Check(err, ErrorMatches, `cannot set next boot: zap`)

	bootloader.ForceError(errors.New("brkn"))
	err = bootloader.SetNextBoot(&snap.Info{}, snap.TypeApp)
	c.Check(err, ErrorMatches, `cannot set next boot: brkn`)
}

func (s *bootenvTestSuite) TestSetNextBootForCore(c *C) {
	bootloader.Force(s.b)
	defer bootloader.Force(nil)

	info := &snap.Info{}
	info.SnapType = snap.TypeOS
	info.RealName = "core"
	info.Revision = snap.R(100)

	err := bootloader.SetNextBoot(info, info.GetType())
	c.Assert(err, IsNil)

	v, err := s.b.GetBootVars("snap_try_core", "snap_mode")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_try_core": "core_100.snap",
		"snap_mode":     "try",
	})

	reqd, err := bootloader.ChangeRequiresReboot(info, info.GetType())
	c.Check(err, IsNil)
	c.Check(reqd, Equals, true)
}

func (s *bootenvTestSuite) TestSetNextBootWithBaseForCore(c *C) {
	bootloader.Force(s.b)
	defer bootloader.Force(nil)

	info := &snap.Info{}
	info.SnapType = snap.TypeBase
	info.RealName = "core18"
	info.Revision = snap.R(1818)

	err := bootloader.SetNextBoot(info, info.GetType())
	c.Assert(err, IsNil)

	v, err := s.b.GetBootVars("snap_try_core", "snap_mode")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_try_core": "core18_1818.snap",
		"snap_mode":     "try",
	})

	reqd, err := bootloader.ChangeRequiresReboot(info, info.GetType())
	c.Check(err, IsNil)
	c.Check(reqd, Equals, true)
}

func (s *bootenvTestSuite) TestSetNextBootForKernel(c *C) {
	bootloader.Force(s.b)
	defer bootloader.Force(nil)

	info := &snap.Info{}
	info.SnapType = snap.TypeKernel
	info.RealName = "krnl"
	info.Revision = snap.R(42)

	err := bootloader.SetNextBoot(info, info.GetType())
	c.Assert(err, IsNil)

	v, err := s.b.GetBootVars("snap_try_kernel", "snap_mode")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_try_kernel": "krnl_42.snap",
		"snap_mode":       "try",
	})

	bootVars := map[string]string{
		"snap_kernel":     "krnl_40.snap",
		"snap_try_kernel": "krnl_42.snap"}
	s.b.SetBootVars(bootVars)
	reqd, err := bootloader.ChangeRequiresReboot(info, info.GetType())
	c.Check(err, IsNil)
	c.Check(reqd, Equals, true)

	// simulate good boot
	bootVars = map[string]string{"snap_kernel": "krnl_42.snap"}
	s.b.SetBootVars(bootVars)
	reqd, err = bootloader.ChangeRequiresReboot(info, info.GetType())
	c.Check(err, IsNil)
	c.Check(reqd, Equals, false)
}

func (s *bootenvTestSuite) TestSetNextBootForKernelForTheSameKernel(c *C) {
	bootloader.Force(s.b)
	defer bootloader.Force(nil)

	info := &snap.Info{}
	info.SnapType = snap.TypeKernel
	info.RealName = "krnl"
	info.Revision = snap.R(40)

	bootVars := map[string]string{"snap_kernel": "krnl_40.snap"}
	s.b.SetBootVars(bootVars)

	err := bootloader.SetNextBoot(info, info.GetType())
	c.Assert(err, IsNil)

	v, err := s.b.GetBootVars("snap_kernel")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_kernel": "krnl_40.snap",
	})
}

func (s *bootenvTestSuite) TestSetNextBootForKernelForTheSameKernelTryMode(c *C) {
	bootloader.Force(s.b)
	defer bootloader.Force(nil)

	info := &snap.Info{}
	info.SnapType = snap.TypeKernel
	info.RealName = "krnl"
	info.Revision = snap.R(40)

	bootVars := map[string]string{
		"snap_kernel":     "krnl_40.snap",
		"snap_try_kernel": "krnl_99.snap",
		"snap_mode":       "try"}
	s.b.SetBootVars(bootVars)

	err := bootloader.SetNextBoot(info, info.GetType())
	c.Assert(err, IsNil)

	v, err := s.b.GetBootVars("snap_kernel", "snap_try_kernel", "snap_mode")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_kernel":     "krnl_40.snap",
		"snap_try_kernel": "",
		"snap_mode":       "",
	})
}

func (s *bootenvTestSuite) TestNameAndRevnoFromSnapValid(c *C) {
	info, err := bootloader.NameAndRevnoFromSnap("foo_2.snap")
	c.Assert(err, IsNil)
	c.Assert(info.Name, Equals, "foo")
	c.Assert(info.Revision, Equals, snap.R(2))
}

func (s *bootenvTestSuite) TestNameAndRevnoFromSnapInvalidFormat(c *C) {
	_, err := bootloader.NameAndRevnoFromSnap("invalid")
	c.Assert(err, ErrorMatches, `input "invalid" has invalid format \(not enough '_'\)`)
	_, err = bootloader.NameAndRevnoFromSnap("invalid_xxx.snap")
	c.Assert(err, ErrorMatches, `invalid snap revision: "xxx"`)
}

func BenchmarkNameAndRevno(b *testing.B) {
	for n := 0; n < b.N; n++ {
		for _, sn := range []string{
			"core_21.snap",
			"kernel_41.snap",
			"some-long-kernel-name-kernel_82.snap",
			"what-is-this-core_111.snap",
		} {
			bootloader.NameAndRevnoFromSnap(sn)
		}
	}
}

func (s *bootenvTestSuite) TestInUse(c *C) {
	bootloader.Force(s.b)
	defer bootloader.Force(nil)

	for i, t := range []struct {
		bootVarKey   string
		bootVarValue string

		snapName string
		snapRev  snap.Revision

		inUse bool
	}{
		// in use
		{"snap_kernel", "kernel_41.snap", "kernel", snap.R(41), true},
		{"snap_try_kernel", "kernel_82.snap", "kernel", snap.R(82), true},
		{"snap_core", "core_21.snap", "core", snap.R(21), true},
		{"snap_try_core", "core_42.snap", "core", snap.R(42), true},
		// not in use
		{"snap_core", "core_111.snap", "core", snap.R(21), false},
		{"snap_try_core", "core_111.snap", "core", snap.R(21), false},
		{"snap_kernel", "kernel_111.snap", "kernel", snap.R(1), false},
		{"snap_try_kernel", "kernel_111.snap", "kernel", snap.R(1), false},
	} {
		s.b.BootVars[t.bootVarKey] = t.bootVarValue
		inUse, err := bootloader.InUse(t.snapName, t.snapRev)
		comment := Commentf("%d: unexpected result: %s %s %v", i, t.snapName, t.snapRev, t.inUse)
		c.Assert(err, IsNil, comment)
		c.Assert(inUse, Equals, t.inUse, comment)
	}
}

func (s *bootenvTestSuite) TestInUseUnhapy(c *C) {
	bootloader.Force(s.b)
	defer bootloader.Force(nil)

	s.b.BootVars["snap_kernel"] = "kernel_41.snap"

	// sanity check
	inUse, err := bootloader.InUse("kernel", snap.R(41))
	c.Assert(err, IsNil)
	c.Check(inUse, Equals, true)

	// make GetVars fail
	s.b.GetErr = errors.New("zap")
	inUse, err = bootloader.InUse("kernel", snap.R(41))
	c.Check(inUse, Equals, false)
	c.Check(err, ErrorMatches, "cannot get boot vars: zap")
	s.b.GetErr = nil

	// make bootloader.Find fail
	bootloader.ForceError(errors.New("broken bootloader"))
	inUse, err = bootloader.InUse("kernel", snap.R(41))
	c.Check(inUse, Equals, false)
	c.Check(err, ErrorMatches, "cannot get boot settings: broken bootloader")
}

func (s *bootenvTestSuite) TestCurrentBootNameAndRevision(c *C) {
	bootloader.Force(s.b)
	defer bootloader.Force(nil)

	s.b.BootVars["snap_core"] = "core_2.snap"
	s.b.BootVars["snap_kernel"] = "canonical-pc-linux_2.snap"

	current, err := bootloader.GetCurrentBoot(snap.TypeOS)
	c.Check(err, IsNil)
	c.Check(current.Name, Equals, "core")
	c.Check(current.Revision, Equals, snap.R(2))

	current, err = bootloader.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, IsNil)
	c.Check(current.Name, Equals, "canonical-pc-linux")
	c.Check(current.Revision, Equals, snap.R(2))

	s.b.BootVars["snap_mode"] = "trying"
	_, err = bootloader.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, Equals, bootloader.ErrBootNameAndRevisionNotReady)
}

func (s *bootenvTestSuite) TestCurrentBootNameAndRevisionUnhappy(c *C) {
	bootloader.Force(s.b)
	defer bootloader.Force(nil)

	_, err := bootloader.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, ErrorMatches, "cannot get name and revision of boot kernel: unset")

	_, err = bootloader.GetCurrentBoot(snap.TypeOS)
	c.Check(err, ErrorMatches, "cannot get name and revision of boot base: unset")

	_, err = bootloader.GetCurrentBoot(snap.TypeBase)
	c.Check(err, ErrorMatches, "cannot get name and revision of boot base: unset")

	_, err = bootloader.GetCurrentBoot(snap.TypeApp)
	c.Check(err, ErrorMatches, "internal error: cannot find boot revision for snap type \"app\"")

	// sanity check
	s.b.BootVars["snap_kernel"] = "kernel_41.snap"
	current, err := bootloader.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, IsNil)
	c.Check(current.Name, Equals, "kernel")
	c.Check(current.Revision, Equals, snap.R(41))

	// make GetVars fail
	s.b.GetErr = errors.New("zap")
	_, err = bootloader.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, ErrorMatches, "cannot get boot variables: zap")
	s.b.GetErr = nil

	// make bootloader.Find fail
	bootloader.ForceError(errors.New("broken bootloader"))
	_, err = bootloader.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, ErrorMatches, "cannot get boot settings: broken bootloader")
}
