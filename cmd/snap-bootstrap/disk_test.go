// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package main_test

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap-bootstrap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

type diskSuite struct {
	testutil.BaseTest
}

var _ = Suite(&diskSuite{})

func (s *diskSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
}

type fileInfo struct {
	rdev    uint64
	failSys bool
}

func (*fileInfo) Name() string       { return "" }
func (*fileInfo) Size() int64        { return 0 }
func (*fileInfo) Mode() os.FileMode  { return 0 }
func (*fileInfo) ModTime() time.Time { return time.Time{} }
func (*fileInfo) IsDir() bool        { return false }
func (fi *fileInfo) Sys() any {
	if fi.failSys {
		return nil
	}
	return &syscall.Stat_t{Rdev: fi.rdev}
}

var _ = os.FileInfo(&fileInfo{})

func (s *diskSuite) TestDiskModelHappy(c *C) {
	restore := main.MockOsutilDeviceMajorAndMinor(func(devPath string) (uint32, uint32, error) {
		c.Check(devPath, Equals, "/dev/sda")
		return 4095, 238, nil
	})
	s.AddCleanup(restore)

	modelDir := filepath.Join(dirs.GlobalRootDir, "/sys/dev/block/4095:238/device")
	c.Assert(os.MkdirAll(modelDir, 0755), IsNil)
	modelPath := filepath.Join(modelDir, "model")
	c.Assert(os.WriteFile(modelPath, []byte("  disk model  "), 0644), IsNil)

	sbDisk := &main.SecbootDisk{&main.Disk{Node: "/dev/sda", Parts: []*main.Partition{}}}
	c.Assert(sbDisk.DiskModel(), Equals, "disk model")
}

func (s *diskSuite) TestDiskModelStatError(c *C) {
	restore := main.MockOsutilDeviceMajorAndMinor(func(devPath string) (uint32, uint32, error) {
		c.Check(devPath, Equals, "/dev/sda")
		return 0, 0, errors.New("fail")
	})
	s.AddCleanup(restore)

	sbDisk := &main.SecbootDisk{&main.Disk{Node: "/dev/sda", Parts: []*main.Partition{}}}
	c.Assert(sbDisk.DiskModel(), Equals, "unknown")
}

func (s *diskSuite) TestDiskModelNoModel(c *C) {
	restore := main.MockOsutilDeviceMajorAndMinor(func(devPath string) (uint32, uint32, error) {
		c.Check(devPath, Equals, "/dev/sda")
		return 4095, 238, nil
	})
	s.AddCleanup(restore)

	sbDisk := &main.SecbootDisk{&main.Disk{Node: "/dev/sda", Parts: []*main.Partition{}}}
	c.Assert(sbDisk.DiskModel(), Equals, "unknown")
}

func (s *diskSuite) TestDiskModelEmpty(c *C) {
	restore := main.MockOsutilDeviceMajorAndMinor(func(devPath string) (uint32, uint32, error) {
		c.Check(devPath, Equals, "/dev/sda")
		return 4095, 238, nil
	})
	s.AddCleanup(restore)

	modelDir := filepath.Join(dirs.GlobalRootDir, "/sys/dev/block/4095:238/device")
	c.Assert(os.MkdirAll(modelDir, 0755), IsNil)
	modelPath := filepath.Join(modelDir, "model")
	c.Assert(os.WriteFile(modelPath, []byte("   "), 0644), IsNil)

	sbDisk := &main.SecbootDisk{&main.Disk{Node: "/dev/sda", Parts: []*main.Partition{}}}
	c.Assert(sbDisk.DiskModel(), Equals, "unknown")
}
