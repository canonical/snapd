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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/partition"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/systemd"
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
		Revision:     14,
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
		Revision:     140,
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

func (s *undoTestSuite) TestUndoForCopyData(c *C) {
	v1, err := makeInstalledMockSnap(`name: hello
version: 1.0`, 11)

	c.Assert(err, IsNil)
	makeSnapActive(v1)
	// add some data
	datadir := filepath.Join(dirs.SnapDataDir, "hello/11")
	subdir := filepath.Join(datadir, "random-subdir")
	err = os.MkdirAll(subdir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(subdir, "random-file"), nil, 0644)
	c.Assert(err, IsNil)

	// pretend we install a new version
	v2, err := makeInstalledMockSnap(`name: hello
version: 2.0`, 12)

	c.Assert(err, IsNil)

	sn1, err := NewInstalledSnap(v1)
	c.Assert(err, IsNil)

	sn, err := NewInstalledSnap(v2)
	c.Assert(err, IsNil)

	// copy data
	err = CopyData(sn.Info(), sn1.Info(), 0, &s.meter)
	c.Assert(err, IsNil)
	v2data := filepath.Join(dirs.SnapDataDir, "hello/12")
	l, _ := filepath.Glob(filepath.Join(v2data, "*"))
	c.Assert(l, HasLen, 1)

	UndoCopyData(sn.Info(), 0, &s.meter)
	l, _ = filepath.Glob(filepath.Join(v2data, "*"))
	c.Assert(l, HasLen, 0)

}

func (s *undoTestSuite) TestUndoForGenerateWrappers(c *C) {
	yaml, err := makeInstalledMockSnap(`name: hello
version: 1.0
apps:
 bin:
   command: bin
 svc:
   command: svc
   daemon: simple
`, 11)

	c.Assert(err, IsNil)

	sn, err := NewInstalledSnap(yaml)
	c.Assert(err, IsNil)

	err = GenerateWrappers(sn.Info(), &s.meter)
	c.Assert(err, IsNil)

	l, err := filepath.Glob(filepath.Join(dirs.SnapBinariesDir, "*"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)
	l, err = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.service"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)

	// undo via remove
	err = RemoveGeneratedWrappers(sn.Info(), &s.meter)
	l, err = filepath.Glob(filepath.Join(dirs.SnapBinariesDir, "*"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 0)
	l, err = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.service"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 0)
}

func (s *undoTestSuite) TestUndoForUpdateCurrentSymlink(c *C) {
	v1yaml, err := makeInstalledMockSnap(`name: hello
version: 1.0
`, 11)

	c.Assert(err, IsNil)
	makeSnapActive(v1yaml)

	v2yaml, err := makeInstalledMockSnap(`name: hello
version: 2.0
`, 22)

	c.Assert(err, IsNil)

	v1, err := NewInstalledSnap(v1yaml)
	c.Assert(err, IsNil)
	v2, err := NewInstalledSnap(v2yaml)
	c.Assert(err, IsNil)

	err = UpdateCurrentSymlink(v2.Info(), &s.meter)
	c.Assert(err, IsNil)

	v1MountDir := v1.Info().MountDir()
	v2MountDir := v2.Info().MountDir()
	v2DataDir := v2.Info().DataDir()
	currentActiveSymlink := filepath.Join(v2MountDir, "..", "current")
	currentActiveDir, err := filepath.EvalSymlinks(currentActiveSymlink)
	c.Assert(err, IsNil)
	c.Assert(currentActiveDir, Equals, v2MountDir)

	currentDataSymlink := filepath.Join(filepath.Dir(v2DataDir), "current")
	currentDataDir, err := filepath.EvalSymlinks(currentDataSymlink)
	c.Assert(err, IsNil)
	c.Assert(currentDataDir, Matches, `.*/22`)

	// undo is just update again
	err = UpdateCurrentSymlink(v1.Info(), &s.meter)
	currentActiveDir, err = filepath.EvalSymlinks(currentActiveSymlink)
	c.Assert(err, IsNil)
	c.Assert(currentActiveDir, Equals, v1MountDir)

	currentDataDir, err = filepath.EvalSymlinks(currentDataSymlink)
	c.Assert(err, IsNil)
	c.Assert(currentDataDir, Matches, `.*/11`)

}
