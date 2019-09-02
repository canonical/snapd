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

	loader *bootloadertest.MockBootloader
}

var _ = Suite(&bootSetSuite{})

func (s *bootSetSuite) SetUpTest(c *C) {
	s.baseBootSetSuite.SetUpTest(c)

	s.loader = bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(s.loader)
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
		s.loader.BootVars[t.bootVarKey] = t.bootVarValue
		c.Assert(boot.InUse(t.snapName, t.snapRev), Equals, t.inUse, Commentf("unexpected result: %s %s %v", t.snapName, t.snapRev, t.inUse))
	}
}

func (s *bootSetSuite) TestInUseUnhapy(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()
	s.loader.BootVars["snap_kernel"] = "kernel_41.snap"

	// sanity check
	c.Check(boot.InUse("kernel", snap.R(41)), Equals, true)

	// make GetVars fail
	s.loader.GetErr = errors.New("zap")
	c.Check(boot.InUse("kernel", snap.R(41)), Equals, false)
	c.Check(logbuf.String(), testutil.Contains, "cannot get boot vars: zap")
	s.loader.GetErr = nil

	// make bootloader.Find fail
	bootloader.ForceError(errors.New("broken bootloader"))
	c.Check(boot.InUse("kernel", snap.R(41)), Equals, false)
	c.Check(logbuf.String(), testutil.Contains, "cannot get boot settings: broken bootloader")
}

func (s *bootSetSuite) TestCurrentBootNameAndRevision(c *C) {
	s.loader.BootVars["snap_core"] = "core_2.snap"
	s.loader.BootVars["snap_kernel"] = "canonical-pc-linux_2.snap"

	current, err := boot.GetCurrentBoot(snap.TypeOS)
	c.Check(err, IsNil)
	c.Check(current.Name, Equals, "core")
	c.Check(current.Revision, Equals, snap.R(2))

	current, err = boot.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, IsNil)
	c.Check(current.Name, Equals, "canonical-pc-linux")
	c.Check(current.Revision, Equals, snap.R(2))

	s.loader.BootVars["snap_mode"] = "trying"
	_, err = boot.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, Equals, boot.ErrBootNameAndRevisionNotReady)
}

func (s *bootSetSuite) TestCurrentBootNameAndRevisionUnhappy(c *C) {
	_, err := boot.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, ErrorMatches, "cannot get name and revision of boot kernel: unset")

	_, err = boot.GetCurrentBoot(snap.TypeOS)
	c.Check(err, ErrorMatches, "cannot get name and revision of boot base: unset")

	_, err = boot.GetCurrentBoot(snap.TypeBase)
	c.Check(err, ErrorMatches, "cannot get name and revision of boot base: unset")

	_, err = boot.GetCurrentBoot(snap.TypeApp)
	c.Check(err, ErrorMatches, "internal error: cannot find boot revision for snap type \"app\"")

	// sanity check
	s.loader.BootVars["snap_kernel"] = "kernel_41.snap"
	current, err := boot.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, IsNil)
	c.Check(current.Name, Equals, "kernel")
	c.Check(current.Revision, Equals, snap.R(41))

	// make GetVars fail
	s.loader.GetErr = errors.New("zap")
	_, err = boot.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, ErrorMatches, "cannot get boot variables: zap")
	s.loader.GetErr = nil

	// make bootloader.Find fail
	bootloader.ForceError(errors.New("broken bootloader"))
	_, err = boot.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, ErrorMatches, "cannot get boot settings: broken bootloader")
}

func (s *bootSetSuite) TestLookup(c *C) {
	info := &snap.Info{}
	info.RealName = "some-snap"

	bp, applicable := boot.Lookup(info, snap.TypeApp, nil, false)
	c.Check(bp, IsNil)
	c.Check(applicable, Equals, false)

	for _, typ := range []snap.Type{
		snap.TypeKernel,
		snap.TypeOS,
		snap.TypeBase,
	} {
		bp, applicable = boot.Lookup(info, typ, nil, true)
		c.Check(bp, IsNil)
		c.Check(applicable, Equals, false)

		bp, applicable = boot.Lookup(info, typ, nil, false)
		c.Check(applicable, Equals, true)

		if typ == snap.TypeKernel {
			c.Check(bp, DeepEquals, boot.NewCoreKernel(info))
		} else {
			c.Check(bp, DeepEquals, boot.NewCoreBootParticipant(info, typ))
		}
	}
}

type mockModel string

func (s mockModel) Kernel() string { return string(s) }
func (s mockModel) Base() string   { return string(s) }

func (s *bootSetSuite) TestLookupBaseWithModel(c *C) {
	core := &snap.Info{SideInfo: snap.SideInfo{RealName: "core"}, SnapType: snap.TypeOS}
	core18 := &snap.Info{SideInfo: snap.SideInfo{RealName: "core18"}, SnapType: snap.TypeBase}

	type tableT struct {
		with       *snap.Info
		model      mockModel
		applicable bool
	}

	table := []tableT{
		{
			with:       core,
			model:      "",
			applicable: true,
		}, {
			with:       core,
			model:      "core",
			applicable: true,
		}, {
			with:       core,
			model:      "core18",
			applicable: false,
		},
		{
			with:       core18,
			model:      "",
			applicable: false,
		},
		{
			with:       core18,
			model:      "core",
			applicable: false,
		},
		{
			with:       core18,
			model:      "core18",
			applicable: true,
		},
	}

	for i, t := range table {
		bp, applicable := boot.Lookup(t.with, t.with.GetType(), t.model, true)
		c.Check(applicable, Equals, false)
		c.Check(bp, IsNil)

		bp, applicable = boot.Lookup(t.with, t.with.GetType(), t.model, false)
		c.Check(applicable, Equals, t.applicable, Commentf("%d", i))
		if t.applicable {
			c.Check(bp, DeepEquals, boot.NewCoreBootParticipant(t.with, t.with.GetType()))
		} else {
			c.Check(bp, IsNil)
		}
	}
}

func (s *bootSetSuite) TestLookupKernelWithModel(c *C) {
	info := &snap.Info{}
	info.RealName = "kernel"
	expectedbp := boot.NewCoreKernel(info)

	type tableT struct {
		model      mockModel
		applicable bool
		bp         boot.BootParticipant
	}

	table := []tableT{
		{
			model:      "other-kernel",
			applicable: false,
			bp:         nil,
		}, {
			model:      "kernel",
			applicable: true,
			bp:         expectedbp,
		}, {
			model:      "",
			applicable: false,
			bp:         nil,
		},
	}

	for _, t := range table {
		bp, applicable := boot.Lookup(info, snap.TypeKernel, t.model, true)
		c.Check(applicable, Equals, false)
		c.Check(bp, IsNil)

		bp, applicable = boot.Lookup(info, snap.TypeKernel, t.model, false)
		c.Check(applicable, Equals, t.applicable)
		c.Check(bp, DeepEquals, t.bp)
	}
}

func (s *bootSetSuite) TestMarkBootSuccessfulAllSnap(c *C) {
	s.loader.BootVars["snap_mode"] = "trying"
	s.loader.BootVars["snap_try_core"] = "os1"
	s.loader.BootVars["snap_try_kernel"] = "k1"
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
	c.Assert(s.loader.BootVars, DeepEquals, expected)

	// do it again, verify its still valid
	err = boot.MarkBootSuccessful()
	c.Assert(err, IsNil)
	c.Assert(s.loader.BootVars, DeepEquals, expected)
}

func (s *bootSetSuite) TestMarkBootSuccessfulKKernelUpdate(c *C) {
	s.loader.BootVars["snap_mode"] = "trying"
	s.loader.BootVars["snap_core"] = "os1"
	s.loader.BootVars["snap_kernel"] = "k1"
	s.loader.BootVars["snap_try_core"] = ""
	s.loader.BootVars["snap_try_kernel"] = "k2"
	err := boot.MarkBootSuccessful()
	c.Assert(err, IsNil)
	c.Assert(s.loader.BootVars, DeepEquals, map[string]string{
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
