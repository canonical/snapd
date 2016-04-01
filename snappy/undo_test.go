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

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/partition"
	"github.com/ubuntu-core/snappy/progress"

	. "gopkg.in/check.v1"
)

type undoTestSuite struct {
	meter progress.NullProgress
}

var _ = Suite(&undoTestSuite{})

func (s *undoTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc", "systemd", "system", "multi-user.target.wants"), 0755)
	c.Assert(err, IsNil)
}

func (s *undoTestSuite) TearDownTest(c *C) {
	findBootloader = partition.FindBootloader
}

var helloSnap = `name: hello-snap
version: 1.0
`

func (s *undoTestSuite) TestUndoForSetupSnapSimple(c *C) {
	snapFile := makeTestSnapPackage(c, helloSnap)

	instDir, err := SetupSnap(snapFile, 0, &s.meter)
	c.Assert(err, IsNil)
	c.Assert(instDir, Equals, filepath.Join(dirs.SnapSnapsDir, "hello-snap/1.0"))
	l, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.mount"))
	c.Assert(l, HasLen, 1)

	// undo undoes the mount unit and the instdir creation
	UndoSetupSnap(instDir, &s.meter)
	l, _ = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.mount"))
	c.Assert(l, HasLen, 0)
	c.Assert(osutil.FileExists(instDir), Equals, false)
}

func (s *undoTestSuite) TestUndoForSetupSnapKernelUboot(c *C) {
	bootloader := newMockBootloader(c.MkDir())
	findBootloader = func() (partition.Bootloader, error) {
		return bootloader, nil
	}

	testFiles := [][]string{
		{"vmlinuz-4.4.0-14-generic.efi.signed", "kernel"},
		{"initrd.img-4.4.0-14-generic", "initrd"},
		{"lib/modules/4.4.0-14-generic/foo.ko", "a module"},
		{"lib/firmware/bar.bin", "some firmware"},
	}
	snapFile := makeTestSnapPackageWithFiles(c, `name: kernel-snap
version: 1.0
type: kernel

kernel: vmlinuz-4.4.0-14-generic.efi.signed
initrd: initrd.img-4.4.0-14-generic
modules: lib/modules/4.4.0-14-generic
firmware: lib/firmware
`, testFiles)

	instDir, err := SetupSnap(snapFile, 0, &s.meter)
	c.Assert(err, IsNil)
	l, _ := filepath.Glob(filepath.Join(bootloader.Dir(), "*"))
	c.Assert(l, HasLen, 1)

	// undo deletes the kernel assets again
	UndoSetupSnap(instDir, &s.meter)
	l, _ = filepath.Glob(filepath.Join(bootloader.Dir(), "*"))
	c.Assert(l, HasLen, 0)
}

func (s *undoTestSuite) TestUndoForCopyData(c *C) {
	v1, err := makeInstalledMockSnap(dirs.SnapSnapsDir, `name: hello
version: 1.0`)
	c.Assert(err, IsNil)
	makeSnapActive(v1)
	// add some data
	datadir := filepath.Join(dirs.SnapDataDir, "hello/1.0")
	subdir := filepath.Join(datadir, "random-subdir")
	err = os.MkdirAll(subdir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(subdir, "random-file"), nil, 0644)
	c.Assert(err, IsNil)

	// pretend we install a new version
	v2, err := makeInstalledMockSnap(dirs.SnapSnapsDir, `name: hello
version: 2.0`)
	c.Assert(err, IsNil)

	sn, err := NewInstalledSnap(v2)
	c.Assert(err, IsNil)

	// copy data
	err = CopyData(sn, 0, &s.meter)
	c.Assert(err, IsNil)
	v2data := filepath.Join(dirs.SnapDataDir, "hello/2.0")
	l, _ := filepath.Glob(filepath.Join(v2data, "*"))
	c.Assert(l, HasLen, 1)

	UndoCopyData(sn, 0, &s.meter)
	l, _ = filepath.Glob(filepath.Join(v2data, "*"))
	c.Assert(l, HasLen, 0)

}

func (s *undoTestSuite) TestUndoForFinalize(c *C) {
	v1yaml, err := makeInstalledMockSnap(dirs.SnapSnapsDir, `name: hello
version: 1.0`)
	c.Assert(err, IsNil)
	makeSnapActive(v1yaml)

	v2yaml, err := makeInstalledMockSnap(dirs.SnapSnapsDir, `name: hello
version: 2.0`)
	c.Assert(err, IsNil)

	v1sn, err := NewInstalledSnap(v1yaml)
	c.Assert(err, IsNil)
	v2sn, err := NewInstalledSnap(v2yaml)
	c.Assert(err, IsNil)

	err = FinalizeSnap(v2sn, 0, &s.meter)
	c.Assert(err, IsNil)
	currentActiveDir, _ := filepath.EvalSymlinks(filepath.Join(v2sn.basedir, "..", "current"))
	c.Assert(currentActiveDir, Matches, ".*/2.0")

	UndoFinalizeSnap(v1sn, v2sn, 0, &s.meter)
	currentActiveDir, _ = filepath.EvalSymlinks(filepath.Join(v2sn.basedir, "..", "current"))
	c.Assert(currentActiveDir, Matches, ".*/1.0")
}
