// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package gadget_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type filesystemImageTestSuite struct {
	testutil.BaseTest

	dir       string
	work      string
	content   string
	psTrivial *gadget.LaidOutStructure
}

var _ = Suite(&filesystemImageTestSuite{})

func (s *filesystemImageTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.dir = c.MkDir()
	// work directory
	s.work = filepath.Join(s.dir, "work")
	err := os.MkdirAll(s.work, 0755)
	c.Assert(err, IsNil)

	// gadget content directory
	s.content = filepath.Join(s.dir, "content")

	s.psTrivial = &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "happyfs",
			Size:       2 * gadget.SizeMiB,
			Content:    []gadget.VolumeContent{},
		},
		Index: 1,
	}
}

func (s *filesystemImageTestSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *filesystemImageTestSuite) imgForPs(c *C, ps *gadget.LaidOutStructure) string {
	c.Assert(ps, NotNil)
	img := filepath.Join(s.dir, "img")
	makeSizedFile(c, img, ps.Size, nil)
	return img
}

type filesystemImageMockedTestSuite struct {
	filesystemImageTestSuite
}

var _ = Suite(&filesystemImageMockedTestSuite{})

func (s *filesystemImageMockedTestSuite) SetUpTest(c *C) {
	s.filesystemImageTestSuite.SetUpTest(c)

	unexpectedMkfs := func(imgFile, label, contentsRootDir string) error {
		return errors.New("unexpected mkfs call")
	}
	s.AddCleanup(gadget.MockMkfsHandlers(map[string]gadget.MkfsFunc{
		"happyfs": unexpectedMkfs,
	}))
}

func (s *filesystemImageMockedTestSuite) TearDownTest(c *C) {
	s.filesystemImageTestSuite.TearDownTest(c)
}

func (s *filesystemImageMockedTestSuite) TestSimpleErrors(c *C) {
	psValid := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "ext4",
			Size:       2 * gadget.SizeMiB,
		},
	}

	fiw, err := gadget.NewFilesystemImageWriter("", psValid, "")
	c.Assert(err, ErrorMatches, "internal error: gadget content directory cannot be unset")
	c.Assert(fiw, IsNil)

	fiw, err = gadget.NewFilesystemImageWriter(s.dir, nil, "")
	c.Assert(err, ErrorMatches, `internal error: \*LaidOutStructure is nil`)
	c.Assert(fiw, IsNil)

	psNoFs := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "none",
			Size:       2 * gadget.SizeMiB,
		},
	}

	fiw, err = gadget.NewFilesystemImageWriter(s.dir, psNoFs, "")
	c.Assert(err, ErrorMatches, "internal error: structure has no filesystem")
	c.Assert(fiw, IsNil)

	psInvalidFs := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "xfs",
			Size:       2 * gadget.SizeMiB,
		},
	}
	fiw, err = gadget.NewFilesystemImageWriter(s.dir, psInvalidFs, "")
	c.Assert(err, ErrorMatches, `internal error: filesystem "xfs" has no handler`)
	c.Assert(fiw, IsNil)
}

func (s *filesystemImageMockedTestSuite) TestHappyFull(c *C) {
	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "happyfs",
			Label:      "so-happy",
			Size:       2 * gadget.SizeMiB,
			Content: []gadget.VolumeContent{
				{Source: "/foo", Target: "/"},
			},
		},
		Index: 2,
	}

	// image file
	img := s.imgForPs(c, ps)

	// mock gadget data
	gd := []gadgetData{
		{name: "foo", target: "foo", content: "hello foo"},
	}
	makeGadgetData(c, s.content, gd)

	var cbCalled bool
	var mkfsCalled bool

	cb := func(rootDir string, cbPs *gadget.LaidOutStructure) error {
		c.Assert(cbPs, DeepEquals, ps)
		c.Assert(rootDir, Equals, filepath.Join(s.work, "snap-stage-content-part-0002"))
		verifyWrittenGadgetData(c, rootDir, gd)

		cbCalled = true
		return nil
	}

	mkfsHappyFs := func(imgFile, label, contentsRootDir string) error {
		c.Assert(imgFile, Equals, img)
		c.Assert(label, Equals, "so-happy")
		c.Assert(contentsRootDir, Equals, filepath.Join(s.work, "snap-stage-content-part-0002"))
		mkfsCalled = true
		return nil
	}

	restore := gadget.MockMkfsHandlers(map[string]gadget.MkfsFunc{
		"happyfs": mkfsHappyFs,
	})
	defer restore()

	fiw, err := gadget.NewFilesystemImageWriter(s.content, ps, s.work)
	c.Assert(err, IsNil)

	err = fiw.Write(img, cb)
	c.Assert(err, IsNil)
	c.Assert(cbCalled, Equals, true)
	c.Assert(mkfsCalled, Equals, true)
	// nothing left in temporary staging location
	matches, err := filepath.Glob(s.work + "/*")
	c.Assert(err, IsNil)
	c.Assert(matches, HasLen, 0)
}

func (s *filesystemImageMockedTestSuite) TestPostStageOptional(c *C) {
	var mkfsCalled bool
	mkfsHappyFs := func(imgFile, label, contentsRootDir string) error {
		mkfsCalled = true
		return nil
	}

	restore := gadget.MockMkfsHandlers(map[string]gadget.MkfsFunc{
		"happyfs": mkfsHappyFs,
	})
	defer restore()

	fiw, err := gadget.NewFilesystemImageWriter(s.content, s.psTrivial, s.work)
	c.Assert(err, IsNil)

	img := s.imgForPs(c, s.psTrivial)

	err = fiw.Write(img, nil)
	c.Assert(err, IsNil)
	c.Assert(mkfsCalled, Equals, true)
}

func (s *filesystemImageMockedTestSuite) TestChecksImage(c *C) {
	cb := func(rootDir string, cbPs *gadget.LaidOutStructure) error {
		return errors.New("unexpected call")
	}

	fiw, err := gadget.NewFilesystemImageWriter(s.content, s.psTrivial, s.work)
	c.Assert(err, IsNil)

	img := filepath.Join(s.dir, "img")

	// no image file
	err = fiw.Write(img, cb)
	c.Assert(err, ErrorMatches, "cannot stat image file: .*/img: no such file or directory")

	// image file smaller than expected
	makeSizedFile(c, img, s.psTrivial.Size/2, nil)

	err = fiw.Write(img, cb)
	c.Assert(err, ErrorMatches, fmt.Sprintf("size of image file %v is different from declared structure size %v", s.psTrivial.Size/2, s.psTrivial.Size))
}

func (s *filesystemImageMockedTestSuite) TestPostStageError(c *C) {
	cb := func(rootDir string, cbPs *gadget.LaidOutStructure) error {
		return errors.New("post stage exploded")
	}

	fiw, err := gadget.NewFilesystemImageWriter(s.content, s.psTrivial, s.work)
	c.Assert(err, IsNil)

	img := s.imgForPs(c, s.psTrivial)

	err = fiw.Write(img, cb)
	c.Assert(err, ErrorMatches, "post stage callback failed: post stage exploded")
}

func (s *filesystemImageMockedTestSuite) TestMkfsError(c *C) {
	mkfsHappyFs := func(imgFile, label, contentsRootDir string) error {
		return errors.New("will not mkfs")
	}
	restore := gadget.MockMkfsHandlers(map[string]gadget.MkfsFunc{
		"happyfs": mkfsHappyFs,
	})
	defer restore()

	fiw, err := gadget.NewFilesystemImageWriter(s.content, s.psTrivial, s.work)
	c.Assert(err, IsNil)

	img := s.imgForPs(c, s.psTrivial)

	err = fiw.Write(img, nil)
	c.Assert(err, ErrorMatches, `cannot create "happyfs" filesystem: will not mkfs`)
}

func (s *filesystemImageMockedTestSuite) TestFilesystemExtraCheckError(c *C) {
	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "happyfs",
			Size:       2 * gadget.SizeMiB,
			Content:    []gadget.VolumeContent{},
		},
	}

	fiw, err := gadget.NewFilesystemImageWriter(s.content, ps, s.work)
	c.Assert(err, IsNil)

	img := s.imgForPs(c, ps)

	// modify filesystem
	ps.Filesystem = "foofs"

	err = fiw.Write(img, nil)
	c.Assert(err, ErrorMatches, `internal error: filesystem "foofs" has no handler`)
}

func (s *filesystemImageMockedTestSuite) TestMountedWriterError(c *C) {
	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "happyfs",
			Size:       2 * gadget.SizeMiB,
			Content: []gadget.VolumeContent{
				{Source: "/foo", Target: "/"},
			},
		},
	}

	cb := func(rootDir string, cbPs *gadget.LaidOutStructure) error {
		return errors.New("unexpected call")
	}

	fiw, err := gadget.NewFilesystemImageWriter(s.content, ps, s.work)
	c.Assert(err, IsNil)

	img := s.imgForPs(c, ps)

	// declared content does not exist in the content directory
	err = fiw.Write(img, cb)
	c.Assert(err, ErrorMatches, `cannot prepare filesystem content: cannot write filesystem content of source:/foo: .* no such file or directory`)
}

func (s *filesystemImageMockedTestSuite) TestBadWorkDirError(c *C) {
	cb := func(rootDir string, cbPs *gadget.LaidOutStructure) error {
		return errors.New("unexpected call")
	}

	badWork := filepath.Join(s.dir, "bad-work")
	fiw, err := gadget.NewFilesystemImageWriter(s.content, s.psTrivial, badWork)
	c.Assert(err, IsNil)

	img := s.imgForPs(c, s.psTrivial)

	err = fiw.Write(img, cb)
	c.Assert(err, ErrorMatches, `cannot prepare staging directory: mkdir .*/bad-work/snap-stage-content-part-0001: no such file or directory`)

	err = os.MkdirAll(filepath.Join(badWork, "snap-stage-content-part-0001"), 0755)
	c.Assert(err, IsNil)

	err = fiw.Write(img, cb)
	c.Assert(err, ErrorMatches, `cannot prepare staging directory .*/bad-work/snap-stage-content-part-0001: path exists`)
}

func (s *filesystemImageMockedTestSuite) TestKeepsStagingDir(c *C) {
	cb := func(rootDir string, cbPs *gadget.LaidOutStructure) error {
		return nil
	}
	mkfsHappyFs := func(imgFile, label, contentsRootDir string) error {
		return nil
	}
	restore := gadget.MockMkfsHandlers(map[string]gadget.MkfsFunc{
		"happyfs": mkfsHappyFs,
	})
	defer restore()

	fiw, err := gadget.NewFilesystemImageWriter(s.content, s.psTrivial, s.work)
	c.Assert(err, IsNil)

	img := s.imgForPs(c, s.psTrivial)

	os.Setenv("SNAP_DEBUG_IMAGE_NO_CLEANUP", "1")
	defer os.Unsetenv("SNAP_DEBUG_IMAGE_NO_CLEANUP")
	err = fiw.Write(img, cb)
	c.Assert(err, IsNil)

	matches, err := filepath.Glob(s.work + "/*")
	c.Assert(err, IsNil)
	c.Assert(matches, HasLen, 1)
	c.Assert(osutil.IsDirectory(filepath.Join(s.work, "snap-stage-content-part-0001")), Equals, true)
}

type filesystemImageMkfsTestSuite struct {
	filesystemImageTestSuite

	cmdFakeroot *testutil.MockCmd
	cmdMkfsVfat *testutil.MockCmd
	cmdMcopy    *testutil.MockCmd
}

func (s *filesystemImageMkfsTestSuite) SetUpTest(c *C) {
	s.filesystemImageTestSuite.SetUpTest(c)

	// mkfs.ext4 is called via fakeroot wrapper
	s.cmdFakeroot = testutil.MockCommand(c, "fakeroot", "")
	s.AddCleanup(s.cmdFakeroot.Restore)

	s.cmdMkfsVfat = testutil.MockCommand(c, "mkfs.vfat", "")
	s.AddCleanup(s.cmdMkfsVfat.Restore)

	s.cmdMcopy = testutil.MockCommand(c, "mcopy", "")
	s.AddCleanup(s.cmdMcopy.Restore)
}

func (s *filesystemImageMkfsTestSuite) TearDownTest(c *C) {
	s.filesystemImageTestSuite.TearDownTest(c)
}

var _ = Suite(&filesystemImageMkfsTestSuite{})

func (s *filesystemImageMkfsTestSuite) TestExt4(c *C) {
	psExt4 := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "ext4",
			Size:       2 * gadget.SizeMiB,
			Content:    []gadget.VolumeContent{{Source: "/", Target: "/"}},
		},
	}

	makeSizedFile(c, filepath.Join(s.content, "foo"), 1024, nil)

	fiw, err := gadget.NewFilesystemImageWriter(s.content, psExt4, s.work)
	c.Assert(err, IsNil)

	img := s.imgForPs(c, psExt4)
	err = fiw.Write(img, nil)
	c.Assert(err, IsNil)

	c.Check(s.cmdFakeroot.Calls(), HasLen, 1)
	c.Check(s.cmdMkfsVfat.Calls(), HasLen, 0)
	c.Check(s.cmdMcopy.Calls(), HasLen, 0)
}

func (s *filesystemImageMkfsTestSuite) TestVfat(c *C) {
	psVfat := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "vfat",
			Size:       2 * gadget.SizeMiB,
			Content:    []gadget.VolumeContent{{Source: "/", Target: "/"}},
		},
	}

	makeSizedFile(c, filepath.Join(s.content, "foo"), 1024, nil)

	fiw, err := gadget.NewFilesystemImageWriter(s.content, psVfat, s.work)
	c.Assert(err, IsNil)

	img := s.imgForPs(c, psVfat)
	err = fiw.Write(img, nil)
	c.Assert(err, IsNil)

	c.Check(s.cmdFakeroot.Calls(), HasLen, 0)
	c.Check(s.cmdMkfsVfat.Calls(), HasLen, 1)
	c.Check(s.cmdMcopy.Calls(), HasLen, 1)
}
