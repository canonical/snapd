// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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
	"errors"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

// set up gocheck
func TestBoot(t *testing.T) { TestingT(t) }

// baseBootSuite is used to setup the common test environment
type baseBootSetSuite struct {
	testutil.BaseTest

	bootdir string
}

func (s *baseBootSetSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
	restore := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	s.AddCleanup(restore)

	s.bootdir = filepath.Join(dirs.GlobalRootDir, "boot")
}

// bootSetSuite tests the abstract BootSet interface, and tools that
// don't depend on a specific BootSet implementation
type bootSetSuite struct {
	baseBootSetSuite

	bootloader *bootloadertest.MockBootloader
}

var _ = Suite(&bootSetSuite{})

func (s *bootSetSuite) SetUpTest(c *C) {
	s.baseBootSetSuite.SetUpTest(c)

	s.bootloader = bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(s.bootloader)
	s.AddCleanup(func() { bootloader.Force(nil) })
}

func (s *bootSetSuite) TestNameAndRevnoFromSnapValid(c *C) {
	info, err := boot.NameAndRevnoFromSnap("foo_2.snap")
	c.Assert(err, IsNil)
	c.Assert(info.Name, Equals, "foo")
	c.Assert(info.Revision, Equals, snap.R(2))
}

func (s *bootSetSuite) TestNameAndRevnoFromSnapInvalidFormat(c *C) {
	_, err := boot.NameAndRevnoFromSnap("invalid")
	c.Assert(err, ErrorMatches, `input "invalid" has invalid format \(not enough '_'\)`)
	_, err = boot.NameAndRevnoFromSnap("invalid_xxx.snap")
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
			boot.NameAndRevnoFromSnap(sn)
		}
	}
}

func (s *bootSetSuite) TestInUse(c *C) {
	for _, t := range []struct {
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
		s.bootloader.BootVars[t.bootVarKey] = t.bootVarValue
		c.Assert(boot.InUse(t.snapName, t.snapRev), Equals, t.inUse, Commentf("unexpected result: %s %s %v", t.snapName, t.snapRev, t.inUse))
	}
}

func (s *bootSetSuite) TestInUseUnhapy(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()
	s.bootloader.BootVars["snap_kernel"] = "kernel_41.snap"

	// sanity check
	c.Check(boot.InUse("kernel", snap.R(41)), Equals, true)

	// make GetVars fail
	s.bootloader.GetErr = errors.New("zap")
	c.Check(boot.InUse("kernel", snap.R(41)), Equals, false)
	c.Check(logbuf.String(), testutil.Contains, "cannot get boot vars: zap")
	s.bootloader.GetErr = nil

	// make bootloader.Find fail
	bootloader.ForceError(errors.New("broken bootloader"))
	c.Check(boot.InUse("kernel", snap.R(41)), Equals, false)
	c.Check(logbuf.String(), testutil.Contains, "cannot get boot settings: broken bootloader")
}

func (s *bootSetSuite) TestCurrentBootNameAndRevision(c *C) {
	s.bootloader.BootVars["snap_core"] = "core_2.snap"
	s.bootloader.BootVars["snap_kernel"] = "canonical-pc-linux_2.snap"

	current, err := boot.GetCurrentBoot(snap.TypeOS)
	c.Check(err, IsNil)
	c.Check(current.Name, Equals, "core")
	c.Check(current.Revision, Equals, snap.R(2))

	current, err = boot.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, IsNil)
	c.Check(current.Name, Equals, "canonical-pc-linux")
	c.Check(current.Revision, Equals, snap.R(2))

	s.bootloader.BootVars["snap_mode"] = "trying"
	_, err = boot.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, Equals, boot.ErrBootNameAndRevisionNotReady)
}

func (s *bootSetSuite) TestCurrentBootNameAndRevisionUnhappy(c *C) {
	_, err := boot.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, ErrorMatches, "cannot get name and revision of boot kernel: boot variable unset")

	_, err = boot.GetCurrentBoot(snap.TypeOS)
	c.Check(err, ErrorMatches, "cannot get name and revision of boot base: boot variable unset")

	_, err = boot.GetCurrentBoot(snap.TypeBase)
	c.Check(err, ErrorMatches, "cannot get name and revision of boot base: boot variable unset")

	_, err = boot.GetCurrentBoot(snap.TypeApp)
	c.Check(err, ErrorMatches, "internal error: cannot find boot revision for snap type \"app\"")

	// sanity check
	s.bootloader.BootVars["snap_kernel"] = "kernel_41.snap"
	current, err := boot.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, IsNil)
	c.Check(current.Name, Equals, "kernel")
	c.Check(current.Revision, Equals, snap.R(41))

	// make GetVars fail
	s.bootloader.GetErr = errors.New("zap")
	_, err = boot.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, ErrorMatches, "cannot get boot variables: zap")
	s.bootloader.GetErr = nil

	// make bootloader.Find fail
	bootloader.ForceError(errors.New("broken bootloader"))
	_, err = boot.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, ErrorMatches, "cannot get boot settings: broken bootloader")
}

func (s *bootSetSuite) TestParticipant(c *C) {
	info := &snap.Info{}
	info.RealName = "some-snap"

	bp := boot.Participant(info, snap.TypeApp, nil, false)
	c.Check(bp.IsTrivial(), Equals, true)

	for _, typ := range []snap.Type{
		snap.TypeKernel,
		snap.TypeOS,
		snap.TypeBase,
	} {
		bp = boot.Participant(info, typ, nil, true)
		c.Check(bp.IsTrivial(), Equals, true)

		bp = boot.Participant(info, typ, nil, false)
		c.Check(bp.IsTrivial(), Equals, false)

		c.Check(bp, DeepEquals, boot.NewCoreBootParticipant(info, typ))
	}
}

type mockModel string

func (s mockModel) Kernel() string { return string(s) }
func (s mockModel) Base() string   { return string(s) }
func (s mockModel) Classic() bool  { return s == "" }

func (s *bootSetSuite) TestParticipantBaseWithModel(c *C) {
	core := &snap.Info{SideInfo: snap.SideInfo{RealName: "core"}, SnapType: snap.TypeOS}
	core18 := &snap.Info{SideInfo: snap.SideInfo{RealName: "core18"}, SnapType: snap.TypeBase}

	type tableT struct {
		with  *snap.Info
		model mockModel
		nop   bool
	}

	table := []tableT{
		{
			with:  core,
			model: "",
			nop:   false,
		}, {
			with:  core,
			model: "core",
			nop:   false,
		}, {
			with:  core,
			model: "core18",
			nop:   true,
		},
		{
			with:  core18,
			model: "",
			nop:   true,
		},
		{
			with:  core18,
			model: "core",
			nop:   true,
		},
		{
			with:  core18,
			model: "core18",
			nop:   false,
		},
	}

	for i, t := range table {
		bp := boot.Participant(t.with, t.with.GetType(), t.model, true)
		c.Check(bp.IsTrivial(), Equals, true)

		bp = boot.Participant(t.with, t.with.GetType(), t.model, false)
		c.Check(bp.IsTrivial(), Equals, t.nop, Commentf("%d", i))
		if !t.nop {
			c.Check(bp, DeepEquals, boot.NewCoreBootParticipant(t.with, t.with.GetType()))
		}
	}
}

func (s *bootSetSuite) TestKernelWithModel(c *C) {
	info := &snap.Info{}
	info.RealName = "kernel"
	expected := boot.NewCoreKernel(info)

	type tableT struct {
		model mockModel
		nop   bool
		krn   boot.BootKernel
	}

	table := []tableT{
		{
			model: "other-kernel",
			nop:   true,
			krn:   boot.Trivial{},
		}, {
			model: "kernel",
			nop:   false,
			krn:   expected,
		}, {
			model: "",
			nop:   true,
			krn:   boot.Trivial{},
		},
	}

	for _, t := range table {
		krn := boot.Kernel(info, snap.TypeKernel, t.model, true)
		c.Check(krn.IsTrivial(), Equals, true)

		krn = boot.Kernel(info, snap.TypeKernel, t.model, false)
		c.Check(krn.IsTrivial(), Equals, t.nop)
		c.Check(krn, DeepEquals, t.krn)
	}
}

func (s *bootSetSuite) TestMarkBootSuccessfulAllSnap(c *C) {
	s.bootloader.BootVars["snap_mode"] = "trying"
	s.bootloader.BootVars["snap_try_core"] = "os1"
	s.bootloader.BootVars["snap_try_kernel"] = "k1"
	err := boot.MarkBootSuccessful()
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
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// do it again, verify its still valid
	err = boot.MarkBootSuccessful()
	c.Assert(err, IsNil)
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)
}

func (s *bootSetSuite) TestMarkBootSuccessfulKKernelUpdate(c *C) {
	s.bootloader.BootVars["snap_mode"] = "trying"
	s.bootloader.BootVars["snap_core"] = "os1"
	s.bootloader.BootVars["snap_kernel"] = "k1"
	s.bootloader.BootVars["snap_try_core"] = ""
	s.bootloader.BootVars["snap_try_kernel"] = "k2"
	err := boot.MarkBootSuccessful()
	c.Assert(err, IsNil)
	c.Assert(s.bootloader.BootVars, DeepEquals, map[string]string{
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
