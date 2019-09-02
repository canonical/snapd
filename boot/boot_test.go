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
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
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

	bl *bootloadertest.MockBootloader
}

var _ = Suite(&bootSetSuite{})

func (s *bootSetSuite) SetUpTest(c *C) {
	s.baseBootSetSuite.SetUpTest(c)

	s.bl = bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(s.bl)
	s.AddCleanup(func() { bootloader.Force(nil) })
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
