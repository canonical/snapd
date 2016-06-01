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

package snappy

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

type undoTestSuite struct {
	meter progress.NullProgress
}

var _ = Suite(&undoTestSuite{})

func (s *undoTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc", "systemd", "system", "multi-user.target.wants"), 0755)
	c.Assert(err, IsNil)

	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}
}

func (s *undoTestSuite) TearDownTest(c *C) {
	findBootloader = partition.FindBootloader
}

var helloSnap = `name: hello-snap
version: 1.0
`

func (s *undoTestSuite) TestUndoForSetupSnapSimple(c *C) {
	snapPath := makeTestSnapPackage(c, helloSnap)

	si := snap.SideInfo{
		OfficialName: "hello-snap",
		Revision:     snap.R(14),
	}

	minInfo, err := SetupSnap(snapPath, &si, 0, &s.meter)
	c.Assert(err, IsNil)
	c.Assert(minInfo.MountDir(), Equals, filepath.Join(dirs.SnapSnapsDir, "hello-snap/14"))
	l, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.mount"))
	c.Assert(l, HasLen, 1)

	// undo undoes the mount unit and the instdir creation
	UndoSetupSnap(minInfo, &s.meter)
	l, _ = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.mount"))
	c.Assert(l, HasLen, 0)
	c.Assert(osutil.FileExists(minInfo.MountDir()), Equals, false)
}

func (s *undoTestSuite) TestUndoForSetupSnapKernelUboot(c *C) {
	bootloader := newMockBootloader(c.MkDir())
	findBootloader = func() (partition.Bootloader, error) {
		return bootloader, nil
	}

	testFiles := [][]string{
		{"kernel.img", "kernel"},
		{"initrd.img", "initrd"},
		{"modules/4.4.0-14-generic/foo.ko", "a module"},
		{"firmware/bar.bin", "some firmware"},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	snapPath := makeTestSnapPackageWithFiles(c, `name: kernel-snap
version: 1.0
type: kernel
`, testFiles)

	si := snap.SideInfo{
		OfficialName: "kernel-snap",
		Revision:     snap.R(140),
	}

	instDir, err := SetupSnap(snapPath, &si, 0, &s.meter)
	c.Assert(err, IsNil)
	l, _ := filepath.Glob(filepath.Join(bootloader.Dir(), "*"))
	c.Assert(l, HasLen, 1)

	// undo deletes the kernel assets again
	UndoSetupSnap(instDir, &s.meter)
	l, _ = filepath.Glob(filepath.Join(bootloader.Dir(), "*"))
	c.Assert(l, HasLen, 0)
}
