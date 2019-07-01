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
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type mountedfilesystemTestSuite struct {
	dir    string
	backup string
}

var _ = Suite(&mountedfilesystemTestSuite{})

func (s *mountedfilesystemTestSuite) SetUpTest(c *C) {
	s.dir = c.MkDir()
	s.backup = c.MkDir()
}

type gadgetData struct {
	name, target, content string
}

func makeGadgetData(c *C, where string, data []gadgetData) {
	for _, en := range data {
		makeSizedFile(c, filepath.Join(where, en.name), 0, []byte(en.content))
	}
}

func verifyDeployedGadgetData(c *C, where string, data []gadgetData) {
	for _, en := range data {
		target := filepath.Join(where, en.target)
		c.Check(target, testutil.FileContains, en.content)
	}
}

func (s *mountedfilesystemTestSuite) TestWriteFile(c *C) {
	makeSizedFile(c, filepath.Join(s.dir, "foo"), 0, []byte("foo foo foo"))

	outDir := c.MkDir()

	// foo -> /foo
	err := gadget.WriteFile(filepath.Join(s.dir, "foo"), filepath.Join(outDir, "foo"), nil)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(outDir, "foo"), testutil.FileEquals, []byte("foo foo foo"))

	// foo -> bar/foo
	err = gadget.WriteFile(filepath.Join(s.dir, "foo"), filepath.Join(outDir, "bar/foo"), nil)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(outDir, "bar/foo"), testutil.FileEquals, []byte("foo foo foo"))

	// deploy overwrites
	makeSizedFile(c, filepath.Join(outDir, "overwrite"), 0, []byte("disappear"))
	err = gadget.WriteFile(filepath.Join(s.dir, "foo"), filepath.Join(outDir, "overwrite"), nil)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(outDir, "overwrite"), testutil.FileEquals, []byte("foo foo foo"))

	// unless told to preserve
	keepName := filepath.Join(outDir, "keep")
	makeSizedFile(c, keepName, 0, []byte("can't touch this"))
	err = gadget.WriteFile(filepath.Join(s.dir, "foo"), filepath.Join(outDir, "keep"), []string{keepName})
	c.Assert(err, IsNil)
	c.Check(filepath.Join(outDir, "keep"), testutil.FileEquals, []byte("can't touch this"))

	err = gadget.WriteFile(filepath.Join(s.dir, "not-found"), filepath.Join(outDir, "foo"), nil)
	c.Assert(err, ErrorMatches, "cannot copy .*: unable to open .*/not-found: .* no such file or directory")
}

func (s *mountedfilesystemTestSuite) TestWriteDirectoryContents(c *C) {
	gd := []gadgetData{
		{"boot-assets/splash", "splash", "splash"},
		{"boot-assets/some-dir/data", "some-dir/data", "data"},
		{"boot-assets/some-dir/empty-file", "some-dir/empty-file", ""},
		{"boot-assets/nested-dir/nested", "/nested-dir/nested", "nested"},
		{"boot-assets/nested-dir/more-nested/more", "/nested-dir/more-nested/more", "more"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := c.MkDir()
	// boot-assets/ -> / (contents of boot assets under /)
	err := gadget.WriteDirectory(filepath.Join(s.dir, "boot-assets")+"/", outDir+"/", nil)
	c.Assert(err, IsNil)

	verifyDeployedGadgetData(c, outDir, gd)
}

func (s *mountedfilesystemTestSuite) TestWriteDirectoryWhole(c *C) {
	gd := []gadgetData{
		{"boot-assets/splash", "boot-assets/splash", "splash"},
		{"boot-assets/some-dir/data", "boot-assets/some-dir/data", "data"},
		{"boot-assets/some-dir/empty-file", "boot-assets/some-dir/empty-file", ""},
		{"boot-assets/nested-dir/nested", "boot-assets/nested-dir/nested", "nested"},
		{"boot-assets/nested-dir/more-nested/more", "boot-assets//nested-dir/more-nested/more", "more"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := c.MkDir()
	// boot-assets -> / (boot-assets and children under /)
	err := gadget.WriteDirectory(filepath.Join(s.dir, "boot-assets"), outDir+"/", nil)
	c.Assert(err, IsNil)

	verifyDeployedGadgetData(c, outDir, gd)
}

func (s *mountedfilesystemTestSuite) TestWriteNonDirectory(c *C) {
	gd := []gadgetData{
		{name: "foo", content: "nested"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := c.MkDir()

	err := gadget.WriteDirectory(filepath.Join(s.dir, "foo")+"/", outDir, nil)
	c.Assert(err, ErrorMatches, `cannot specify trailing / for a source which is not a directory`)

	err = gadget.WriteDirectory(filepath.Join(s.dir, "foo"), outDir, nil)
	c.Assert(err, ErrorMatches, `source is not a directory`)
}

func (s *mountedfilesystemTestSuite) TestMountedWriterHappy(c *C) {
	gd := []gadgetData{
		{"foo", "foo-dir/foo", "foo foo foo"},
		{"bar", "bar-name", "bar bar bar"},
		{"boot-assets/splash", "splash", "splash"},
		{"boot-assets/some-dir/data", "some-dir/data", "data"},
		{"boot-assets/some-dir/data", "data-copy", "data"},
		{"boot-assets/some-dir/empty-file", "some-dir/empty-file", ""},
		{"boot-assets/nested-dir/nested", "/nested-copy/nested", "nested"},
		{"boot-assets/nested-dir/more-nested/more", "/nested-copy/more-nested/more", "more"},
	}
	makeGadgetData(c, s.dir, gd)
	err := os.MkdirAll(filepath.Join(s.dir, "boot-assets/empty-dir"), 0755)
	c.Assert(err, IsNil)

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					// single file in target directory
					Source: "foo",
					Target: "/foo-dir/",
				}, {
					// single file under different name
					Source: "bar",
					Target: "/bar-name",
				}, {
					// whole directory contents
					Source: "boot-assets/",
					Target: "/",
				}, {
					// single file from nested directory
					Source: "boot-assets/some-dir/data",
					Target: "/data-copy",
				}, {
					// contents of nested directory under new target directory
					Source: "boot-assets/nested-dir/",
					Target: "/nested-copy/",
				},
			},
		},
	}

	outDir := c.MkDir()

	rw, err := gadget.NewMountedFilesystemWriter(s.dir, ps)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, nil)
	c.Assert(err, IsNil)

	verifyDeployedGadgetData(c, outDir, gd)
	c.Assert(osutil.IsDirectory(filepath.Join(outDir, "empty-dir")), Equals, true)
}

func (s *mountedfilesystemTestSuite) TestMountedWriterNonDirectory(c *C) {
	gd := []gadgetData{
		{name: "foo", content: "nested"},
	}
	makeGadgetData(c, s.dir, gd)

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					// contents of nested directory under new target directory
					Source: "foo/",
					Target: "/nested-copy/",
				},
			},
		},
	}

	outDir := c.MkDir()

	rw, err := gadget.NewMountedFilesystemWriter(s.dir, ps)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, nil)
	c.Assert(err, ErrorMatches, `cannot write filesystem content of source:foo/: cannot specify trailing / for a source which is not a directory`)
}

func (s *mountedfilesystemTestSuite) TestMountedWriterErrorMissingSource(c *C) {
	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					// single file in target directory
					Source: "foo",
					Target: "/foo-dir/",
				},
			},
		},
	}

	outDir := c.MkDir()

	rw, err := gadget.NewMountedFilesystemWriter(s.dir, ps)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, nil)
	c.Assert(err, ErrorMatches, "cannot write filesystem content of source:foo: .*unable to open.*: no such file or directory")
}

func (s *mountedfilesystemTestSuite) TestMountedWriterErrorBadDestination(c *C) {
	makeSizedFile(c, filepath.Join(s.dir, "foo"), 0, []byte("foo foo foo"))

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "vfat",
			Content: []gadget.VolumeContent{
				{
					// single file in target directory
					Source: "foo",
					Target: "/foo-dir/",
				},
			},
		},
	}

	outDir := c.MkDir()

	err := os.Chmod(outDir, 0000)
	c.Assert(err, IsNil)

	rw, err := gadget.NewMountedFilesystemWriter(s.dir, ps)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, nil)
	c.Assert(err, ErrorMatches, "cannot write filesystem content of source:foo: cannot create .*: mkdir .* permission denied")
}

func (s *mountedfilesystemTestSuite) TestMountedWriterConflictingDestinationDirectoryErrors(c *C) {
	makeGadgetData(c, s.dir, []gadgetData{
		{name: "foo", content: "foo foo foo"},
		{name: "foo-dir", content: "bar bar bar"},
	})

	psOverwritesDirectoryWithFile := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					// single file in target directory
					Source: "foo",
					Target: "/foo-dir/",
				}, {
					// conflicts with /foo-dir directory
					Source: "foo-dir",
					Target: "/",
				},
			},
		},
	}

	outDir := c.MkDir()

	rw, err := gadget.NewMountedFilesystemWriter(s.dir, psOverwritesDirectoryWithFile)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	// can't overwrite a directory with a file
	err = rw.Write(outDir, nil)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot write filesystem content of source:foo-dir: cannot copy .*: unable to create %s/foo-dir: .* is a directory", outDir))

}

func (s *mountedfilesystemTestSuite) TestMountedWriterConflictingDestinationFileOk(c *C) {
	makeGadgetData(c, s.dir, []gadgetData{
		{name: "foo", content: "foo foo foo"},
		{name: "bar", content: "bar bar bar"},
	})
	psOverwritesFile := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					Source: "bar",
					Target: "/",
				}, {
					// overwrites data from preceding entry
					Source: "foo",
					Target: "/bar",
				},
			},
		},
	}

	outDir := c.MkDir()

	rw, err := gadget.NewMountedFilesystemWriter(s.dir, psOverwritesFile)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, nil)
	c.Assert(err, IsNil)

	c.Check(osutil.FileExists(filepath.Join(outDir, "foo")), Equals, false)
	// overwritten
	c.Check(filepath.Join(outDir, "bar"), testutil.FileEquals, "foo foo foo")
}

func (s *mountedfilesystemTestSuite) TestMountedWriterErrorNested(c *C) {
	makeGadgetData(c, s.dir, []gadgetData{
		{name: "foo/foo-dir", content: "data"},
		{name: "foo/bar/baz", content: "data"},
	})

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					// single file in target directory
					Source: "/",
					Target: "/foo-dir/",
				},
			},
		},
	}

	outDir := c.MkDir()

	makeSizedFile(c, filepath.Join(outDir, "/foo-dir/foo/bar"), 0, nil)

	rw, err := gadget.NewMountedFilesystemWriter(s.dir, ps)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, nil)
	c.Assert(err, ErrorMatches, "cannot write filesystem content of source:/: .* not a directory")
}

func (s *mountedfilesystemTestSuite) TestMountedWriterPreserve(c *C) {
	// some data for the gadget
	gdDeployed := []gadgetData{
		{"foo", "foo-dir/foo", "data"},
		{"bar", "bar-name", "data"},
		{"boot-assets/splash", "splash", "data"},
		{"boot-assets/some-dir/data", "some-dir/data", "data"},
		{"boot-assets/some-dir/empty-file", "some-dir/empty-file", "data"},
		{"boot-assets/nested-dir/more-nested/more", "/nested-copy/more-nested/more", "data"},
	}
	gdNotDeployed := []gadgetData{
		{"foo", "/foo", "data"},
		{"boot-assets/some-dir/data", "data-copy", "data"},
		{"boot-assets/nested-dir/nested", "/nested-copy/nested", "data"},
	}
	makeGadgetData(c, s.dir, append(gdDeployed, gdNotDeployed...))

	// these exist in the root directory and are preserved
	preserve := []string{
		// mix entries with leading / and without
		"/foo",
		"/data-copy",
		"nested-copy/nested",
		"not-listed", // not present in 'gadget' contents
	}
	// these are preserved, but don't exist in the root, so data from gadget
	// will be deployed
	preserveButNotPresent := []string{
		"/bar-name",
		"some-dir/data",
	}
	outDir := filepath.Join(c.MkDir(), "out-dir")

	for _, en := range preserve {
		p := filepath.Join(outDir, en)
		makeSizedFile(c, p, 0, []byte("can't touch this"))
	}

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					Source: "foo",
					Target: "/foo-dir/",
				}, {
					// would overwrite /foo
					Source: "foo",
					Target: "/",
				}, {
					// preserved, but not present, will be
					// deployed
					Source: "bar",
					Target: "/bar-name",
				}, {
					// some-dir/data is preserved, but not
					// preset, hence will be deployed
					Source: "boot-assets/",
					Target: "/",
				}, {
					// would overwrite /data-copy
					Source: "boot-assets/some-dir/data",
					Target: "/data-copy",
				}, {
					// would overwrite /nested-copy/nested
					Source: "boot-assets/nested-dir/",
					Target: "/nested-copy/",
				},
			},
		},
	}

	rw, err := gadget.NewMountedFilesystemWriter(s.dir, ps)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, append(preserve, preserveButNotPresent...))
	c.Assert(err, IsNil)

	// files that existed were preserved
	for _, en := range preserve {
		p := filepath.Join(outDir, en)
		c.Check(p, testutil.FileEquals, "can't touch this")
	}
	// everything else was deployed
	verifyDeployedGadgetData(c, outDir, gdDeployed)
}

func (s *mountedfilesystemTestSuite) TestMountedWriterImplicitDir(c *C) {
	gd := []gadgetData{
		{"boot-assets/nested-dir/nested", "/nested-copy/nested-dir/nested", "nested"},
		{"boot-assets/nested-dir/more-nested/more", "/nested-copy/nested-dir/more-nested/more", "more"},
	}
	makeGadgetData(c, s.dir, gd)

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					// contents of nested directory under new target directory
					Source: "boot-assets/nested-dir",
					Target: "/nested-copy/",
				},
			},
		},
	}

	outDir := c.MkDir()

	rw, err := gadget.NewMountedFilesystemWriter(s.dir, ps)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, nil)
	c.Assert(err, IsNil)

	verifyDeployedGadgetData(c, outDir, gd)
}

func (s *mountedfilesystemTestSuite) TestMountedWriterNoFs(c *C) {
	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			// no filesystem
			Content: []gadget.VolumeContent{
				{
					// single file in target directory
					Source: "foo",
					Target: "/foo-dir/",
				},
			},
		},
	}

	rw, err := gadget.NewMountedFilesystemWriter(s.dir, ps)
	c.Assert(err, ErrorMatches, "structure #0 has no filesystem")
	c.Assert(rw, IsNil)
}

func (s *mountedfilesystemTestSuite) TestMountedWriterTrivialValidation(c *C) {
	rw, err := gadget.NewMountedFilesystemWriter(s.dir, nil)
	c.Assert(err, ErrorMatches, `internal error: \*PositionedStructure.*`)
	c.Assert(rw, IsNil)

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			// no filesystem
			Content: []gadget.VolumeContent{
				{
					Source: "",
					Target: "",
				},
			},
		},
	}

	rw, err = gadget.NewMountedFilesystemWriter("", ps)
	c.Assert(err, ErrorMatches, `internal error: gadget content directory cannot be unset`)
	c.Assert(rw, IsNil)

	rw, err = gadget.NewMountedFilesystemWriter(s.dir, ps)
	c.Assert(err, IsNil)

	err = rw.Write("", nil)
	c.Assert(err, ErrorMatches, "internal error: destination directory cannot be unset")

	d := c.MkDir()
	err = rw.Write(d, nil)
	c.Assert(err, ErrorMatches, "cannot write filesystem content .* source cannot be unset")

	ps.Content[0].Source = "/"
	err = rw.Write(d, nil)
	c.Assert(err, ErrorMatches, "cannot write filesystem content .* target cannot be unset")
}
