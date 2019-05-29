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

func (r *mountedfilesystemTestSuite) SetUpTest(c *C) {
	r.dir = c.MkDir()
	r.backup = c.MkDir()
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

func (r *mountedfilesystemTestSuite) TestDeployFile(c *C) {
	makeSizedFile(c, filepath.Join(r.dir, "foo"), 0, []byte("foo foo foo"))

	outDir := c.MkDir()

	// foo -> /foo
	err := gadget.DeployFile(filepath.Join(r.dir, "foo"), filepath.Join(outDir, "foo"), nil)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(outDir, "foo"), testutil.FileEquals, []byte("foo foo foo"))

	// foo -> bar/foo
	err = gadget.DeployFile(filepath.Join(r.dir, "foo"), filepath.Join(outDir, "bar/foo"), nil)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(outDir, "bar/foo"), testutil.FileEquals, []byte("foo foo foo"))

	// deploy overwrites
	makeSizedFile(c, filepath.Join(outDir, "overwrite"), 0, []byte("disappear"))
	err = gadget.DeployFile(filepath.Join(r.dir, "foo"), filepath.Join(outDir, "overwrite"), nil)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(outDir, "overwrite"), testutil.FileEquals, []byte("foo foo foo"))

	// unless told to preserve
	keepName := filepath.Join(outDir, "keep")
	makeSizedFile(c, keepName, 0, []byte("can't touch this"))
	err = gadget.DeployFile(filepath.Join(r.dir, "foo"), filepath.Join(outDir, "keep"), []string{keepName})
	c.Assert(err, IsNil)
	c.Check(filepath.Join(outDir, "keep"), testutil.FileEquals, []byte("can't touch this"))

	err = gadget.DeployFile(filepath.Join(r.dir, "not-found"), filepath.Join(outDir, "foo"), nil)
	c.Assert(err, ErrorMatches, "cannot copy .*: unable to open .*/not-found: .* no such file or directory")
}

func (r *mountedfilesystemTestSuite) TestDeployDirectoryContents(c *C) {
	gd := []gadgetData{
		{"boot-assets/splash", "splash", "splash"},
		{"boot-assets/some-dir/data", "some-dir/data", "data"},
		{"boot-assets/some-dir/empty-file", "some-dir/empty-file", ""},
		{"boot-assets/nested-dir/nested", "/nested-dir/nested", "nested"},
		{"boot-assets/nested-dir/more-nested/more", "/nested-dir/more-nested/more", "more"},
	}
	makeGadgetData(c, r.dir, gd)

	outDir := c.MkDir()
	// boot-assets/ -> / (contents of boot assets under /)
	err := gadget.DeployDirectory(filepath.Join(r.dir, "boot-assets")+"/", outDir+"/", nil)
	c.Assert(err, IsNil)

	verifyDeployedGadgetData(c, outDir, gd)
}

func (r *mountedfilesystemTestSuite) TestDeployDirectoryWhole(c *C) {
	gd := []gadgetData{
		{"boot-assets/splash", "boot-assets/splash", "splash"},
		{"boot-assets/some-dir/data", "boot-assets/some-dir/data", "data"},
		{"boot-assets/some-dir/empty-file", "boot-assets/some-dir/empty-file", ""},
		{"boot-assets/nested-dir/nested", "boot-assets/nested-dir/nested", "nested"},
		{"boot-assets/nested-dir/more-nested/more", "boot-assets//nested-dir/more-nested/more", "more"},
	}
	makeGadgetData(c, r.dir, gd)

	outDir := c.MkDir()
	// boot-assets -> / (boot-assets and children under /)
	err := gadget.DeployDirectory(filepath.Join(r.dir, "boot-assets"), outDir+"/", nil)
	c.Assert(err, IsNil)

	verifyDeployedGadgetData(c, outDir, gd)
}

func (r *mountedfilesystemTestSuite) TestMountedWriterHappy(c *C) {
	rw := gadget.NewMountedFilesystemWriter(r.dir)

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
	makeGadgetData(c, r.dir, gd)
	err := os.MkdirAll(filepath.Join(r.dir, "boot-assets/empty-dir"), 0755)
	c.Assert(err, IsNil)

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
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

	err = rw.Deploy(outDir, ps, nil)
	c.Assert(err, IsNil)

	verifyDeployedGadgetData(c, outDir, gd)
	c.Assert(osutil.IsDirectory(filepath.Join(outDir, "empty-dir")), Equals, true)
}

func (r *mountedfilesystemTestSuite) TestMountedWriterErrorMissingSource(c *C) {
	rw := gadget.NewMountedFilesystemWriter(r.dir)

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
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

	err := rw.Deploy(outDir, ps, nil)
	c.Assert(err, ErrorMatches, "cannot deploy filesystem content of source:foo: .*unable to open.*: no such file or directory")
}

func (r *mountedfilesystemTestSuite) TestMountedWriterErrorBadDestination(c *C) {
	rw := gadget.NewMountedFilesystemWriter(r.dir)

	makeSizedFile(c, filepath.Join(r.dir, "foo"), 0, []byte("foo foo foo"))

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
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

	err = rw.Deploy(outDir, ps, nil)
	c.Assert(err, ErrorMatches, "cannot deploy filesystem content of source:foo: cannot create .*: mkdir .* permission denied")
}

func (r *mountedfilesystemTestSuite) TestMountedWriterConflictingDestinationDirectoryErrors(c *C) {
	rw := gadget.NewMountedFilesystemWriter(r.dir)

	makeGadgetData(c, r.dir, []gadgetData{
		{name: "foo", content: "foo foo foo"},
		{name: "foo-dir", content: "bar bar bar"},
	})

	psOverwritesDirectoryWithFile := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
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

	// can't overwrite a directory with a file
	err := rw.Deploy(outDir, psOverwritesDirectoryWithFile, nil)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot deploy filesystem content of source:foo-dir: cannot copy .*: unable to create %s/foo-dir: .* is a directory", outDir))

}

func (r *mountedfilesystemTestSuite) TestMountedWriterConflictingDestinationFileOk(c *C) {
	rw := gadget.NewMountedFilesystemWriter(r.dir)

	makeGadgetData(c, r.dir, []gadgetData{
		{name: "foo", content: "foo foo foo"},
		{name: "bar", content: "bar bar bar"},
	})
	psOverwritesFile := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
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

	err := rw.Deploy(outDir, psOverwritesFile, nil)
	c.Assert(err, IsNil)

	c.Check(osutil.FileExists(filepath.Join(outDir, "foo")), Equals, false)
	// overwritten
	c.Check(filepath.Join(outDir, "bar"), testutil.FileEquals, "foo foo foo")
}

func (r *mountedfilesystemTestSuite) TestMountedWriterErrorNested(c *C) {
	rw := gadget.NewMountedFilesystemWriter(r.dir)

	makeGadgetData(c, r.dir, []gadgetData{
		{name: "foo/foo-dir", content: "data"},
		{name: "foo/bar/baz", content: "data"},
	})

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
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

	err := rw.Deploy(outDir, ps, nil)
	c.Assert(err, ErrorMatches, "cannot deploy filesystem content of source:/: .* not a directory")
}

func (r *mountedfilesystemTestSuite) TestMountedWriterPreserve(c *C) {
	rw := gadget.NewMountedFilesystemWriter(r.dir)

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
	makeGadgetData(c, r.dir, append(gdDeployed, gdNotDeployed...))

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
			Size: 2048,
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

	err := rw.Deploy(outDir, ps, append(preserve, preserveButNotPresent...))
	c.Assert(err, IsNil)

	// files that existed were preserved
	for _, en := range preserve {
		p := filepath.Join(outDir, en)
		c.Check(p, testutil.FileEquals, "can't touch this")
	}
	// everything else was deployed
	verifyDeployedGadgetData(c, outDir, gdDeployed)
}

func (r *mountedfilesystemTestSuite) TestMountedWriterImplicitDir(c *C) {
	rw := gadget.NewMountedFilesystemWriter(r.dir)

	gd := []gadgetData{
		{"boot-assets/nested-dir/nested", "/nested-copy/nested-dir/nested", "nested"},
		{"boot-assets/nested-dir/more-nested/more", "/nested-copy/nested-dir/more-nested/more", "more"},
	}
	makeGadgetData(c, r.dir, gd)

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
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

	err := rw.Deploy(outDir, ps, nil)
	c.Assert(err, IsNil)

	verifyDeployedGadgetData(c, outDir, gd)
}
