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

func makeExistingDeployedData(c *C, where string, data []gadgetData) {
	for _, en := range data {
		makeSizedFile(c, filepath.Join(where, en.target), 0, []byte(en.content))
	}
}

func verifyExistingDeployedData(c *C, where string, data []gadgetData) {
	for _, en := range data {
		target := filepath.Join(where, en.target)
		c.Check(target, testutil.FileContains, en.content)
	}
}

type contentType int

const (
	typeFile contentType = iota
	typeDir
)

func verifyDirContents(c *C, where string, expected map[string]contentType) {
	cleanWhere := filepath.Clean(where)

	got := make(map[string]contentType)
	filepath.Walk(where, func(name string, fi os.FileInfo, err error) error {
		if name == where {
			return nil
		}
		suffixName := name[len(cleanWhere)+1:]
		t := typeFile
		if fi.IsDir() {
			t = typeDir
		}
		got[suffixName] = t

		for prefix := filepath.Dir(name); prefix != where; prefix = filepath.Dir(prefix) {
			delete(got, prefix[len(cleanWhere)+1:])
		}

		return nil
	})

	c.Assert(got, DeepEquals, expected)
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
	c.Assert(err, ErrorMatches, "cannot deploy filesystem content of data:foo: .*unable to open.*: no such file or directory")
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
	c.Assert(err, ErrorMatches, "cannot deploy filesystem content of data:foo: cannot create .*: mkdir .* permission denied")
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
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot deploy filesystem content of data:foo-dir: cannot copy .*: unable to create %s/foo-dir: .* is a directory", outDir))

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
	c.Assert(err, ErrorMatches, "cannot deploy filesystem content of data:/: .* not a directory")
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

func (r *mountedfilesystemTestSuite) TestMountedUpdaterBackupSimple(c *C) {
	// some data for the gadget
	gdDeployed := []gadgetData{
		{"bar", "bar-name", "data"},
		{"foo", "foo", "data"},
		{"zed", "zed", "data"},
		{"same-data", "same", "same"},
		// not included in volume contents
		{"not-deployed", "not-deployed", "data"},
	}
	makeGadgetData(c, r.dir, gdDeployed)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	// these exist in the destination directory and will be backed up
	backedUp := []gadgetData{
		{target: "foo", content: "can't touch this"},
		{target: "nested/foo", content: "can't touch this"},
		// listed in preserve
		{target: "zed", content: "preserved"},
		// same content as the update
		{target: "same", content: "same"},
	}
	makeExistingDeployedData(c, outDir, backedUp)

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					Source: "bar",
					Target: "/bar-name",
				}, {
					Source: "foo",
					Target: "/",
				}, {
					Source: "foo",
					Target: "/nested/",
				}, {
					Source: "zed",
					Target: "/",
				}, {
					Source: "same-data",
					Target: "/same",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition:  1,
				Preserve: []string{"/zed"},
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	err := rw.Backup(nil, ps)
	c.Assert(err, IsNil)

	// files that existed were backed up
	for _, en := range backedUp {
		backup := filepath.Join(r.backup, "struct-0", en.target+".backup")
		same := filepath.Join(r.backup, "struct-0", en.target+".same")
		switch en.content {
		case "preserved":
			c.Check(osutil.FileExists(backup), Equals, false, Commentf("file: %v", backup))
			c.Check(osutil.FileExists(same), Equals, false, Commentf("file: %v", same))
		case "same":
			c.Check(osutil.FileExists(same), Equals, true, Commentf("file: %v", same))
		default:
			c.Check(backup, testutil.FileEquals, "can't touch this")
		}
	}

	// running backup again does not error out
	err = rw.Backup(nil, ps)
	c.Assert(err, IsNil)
}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterBackupWithDirectories(c *C) {
	// some data for the gadget
	gdDeployed := []gadgetData{
		{name: "bar", content: "data"},
		{name: "some-dir/foo", content: "data"},
	}
	makeGadgetData(c, r.dir, gdDeployed)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	// these exist in the destination directory and will be backed up
	backedUp := []gadgetData{
		// overwritten by "bar" -> "/foo"
		{target: "foo", content: "can't touch this"},
		// overwritten by some-dir/ -> /nested/
		{target: "nested/foo", content: "can't touch this"},
	}
	makeExistingDeployedData(c, outDir, backedUp)
	// and a directory
	err := os.MkdirAll(filepath.Join(outDir, "this/is/some"), 0755)
	c.Assert(err, IsNil)

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					Source: "bar",
					Target: "/foo",
				}, {
					Source: "bar",
					Target: "/this/is/some/nested/",
				}, {
					Source: "some-dir/",
					Target: "/nested/",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition:  1,
				Preserve: []string{"/zed"},
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	err = rw.Backup(nil, ps)
	c.Assert(err, IsNil)

	verifyDirContents(c, filepath.Join(r.backup, "struct-0"), map[string]contentType{
		"this/is/some.backup": typeFile,
		"this/is.backup":      typeFile,
		"this.backup":         typeFile,

		"nested/foo.backup": typeFile,
		"nested.backup":     typeFile,

		"foo.backup": typeFile,
	})
}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterBackupNonexistent(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{"bar", "foo", "data"},
		{"bar", "some-dir/foo", "data"},
	}
	makeGadgetData(c, r.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					Source: "bar",
					Target: "/foo",
				}, {
					Source: "bar",
					Target: "/some-dir/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
				// bar not in preserved files
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	err := rw.Backup(nil, ps)
	c.Assert(err, IsNil)

	backupRoot := filepath.Join(r.backup, "struct-0")
	err = filepath.Walk(backupRoot, func(name string, _ os.FileInfo, _ error) error {
		c.Check(name, Equals, backupRoot)
		return nil
	})
}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterBackupFailsOnBackupDirErrors(c *C) {
	outDir := filepath.Join(c.MkDir(), "out-dir")

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					Source: "bar",
					Target: "/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	err := os.Chmod(r.backup, 0555)
	defer os.Chmod(r.backup, 0755)

	err = rw.Backup(nil, ps)
	c.Assert(err, ErrorMatches, "cannot create backup directory: .*/struct-0: permission denied")
}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterBackupFailsOnDestinationErrors(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{name: "bar", content: "data"},
	}
	makeGadgetData(c, r.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingDeployedData(c, outDir, []gadgetData{
		{target: "foo", content: "same"},
	})

	err := os.Chmod(filepath.Join(outDir, "foo"), 0000)
	c.Assert(err, IsNil)

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					Source: "bar",
					Target: "/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	err = rw.Backup(nil, ps)
	c.Assert(err, ErrorMatches, "cannot create a backup structure: cannot open destination file: open .*/out-dir/foo: permission denied")
}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterBackupFailsOnBadSrcComparison(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{name: "bar", content: "data"},
	}
	makeGadgetData(c, r.dir, gd)
	err := os.Chmod(filepath.Join(r.dir, "bar"), 0000)
	c.Assert(err, IsNil)

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingDeployedData(c, outDir, []gadgetData{
		{target: "foo", content: "same"},
	})

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					Source: "bar",
					Target: "/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	err = rw.Backup(nil, ps)
	c.Assert(err, ErrorMatches, "cannot create a backup structure: cannot checksum update file: open .*/bar: permission denied")
}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterUpdate(c *C) {
	// some data for the gadget
	gdDeployed := []gadgetData{
		{"foo", "foo-dir/foo", "data"},
		{"bar", "bar-name", "data"},
		{"boot-assets/splash", "splash", "data"},
		{"boot-assets/some-dir/data", "some-dir/data", "data"},
		{"boot-assets/some-dir/empty-file", "some-dir/empty-file", ""},
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
					// present, hence will be deployed
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
				}, {
					Source: "boot-assets",
					Target: "/boot-assets-copy/",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition:  1,
				Preserve: append(preserve, preserveButNotPresent...),
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	err := rw.Backup(nil, ps)
	c.Assert(err, IsNil)

	err = rw.Update(nil, ps)
	c.Assert(err, IsNil)

	// files that existed were preserved
	for _, en := range preserve {
		p := filepath.Join(outDir, en)
		c.Check(p, testutil.FileEquals, "can't touch this")
	}
	// everything else was deployed
	verifyDeployedGadgetData(c, outDir, gdDeployed)
}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterUpdateLookupFails(c *C) {
	makeGadgetData(c, r.dir, []gadgetData{
		{"canary", "canary", "data"},
	})

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					Source: "/",
					Target: "/",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return "", errors.New("failed")
	})

	err := rw.Update(nil, ps)
	c.Assert(err, ErrorMatches, "cannot find mount location of structure #0: failed")
}
func (r *mountedfilesystemTestSuite) TestMountedUpdaterDirContents(c *C) {
	// some data for the gadget
	gdDeployed := []gadgetData{
		{"bar/foo", "/bar-name/foo", "data"},
		{"bar/nested/foo", "/bar-name/nested/foo", "data"},
		{"bar/foo", "/bar-copy/bar/foo", "data"},
		{"bar/nested/foo", "/bar-copy/bar/nested/foo", "data"},
		{"deep-nested", "/this/is/some/deep/nesting/deep-nested", "data"},
	}
	makeGadgetData(c, r.dir, gdDeployed)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					// contents of bar under /bar-name/
					Source: "bar/",
					Target: "/bar-name",
				}, {
					// whole bar under /bar-copy/
					Source: "bar",
					Target: "/bar-copy/",
				}, {
					// deep prefix
					Source: "deep-nested",
					Target: "/this/is/some/deep/nesting/",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	err := rw.Backup(nil, ps)
	c.Assert(err, IsNil)

	err = rw.Update(nil, ps)
	c.Assert(err, IsNil)

	verifyDeployedGadgetData(c, outDir, gdDeployed)
}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterExpectsBackup(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{"bar", "foo", "update"},
		{"bar", "some-dir/foo", "update"},
	}
	makeGadgetData(c, r.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingDeployedData(c, outDir, []gadgetData{
		{target: "foo", content: "content"},
		{target: "some-dir/foo", content: "content"},
		{target: "/preserved", content: "preserve"},
	})

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					Source: "bar",
					Target: "/foo",
				}, {
					Source: "bar",
					Target: "/some-dir/foo",
				}, {
					Source: "bar",
					Target: "/preserved",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
				// bar not in preserved files
				Preserve: []string{"preserved"},
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	err := rw.Update(nil, ps)
	c.Assert(err, ErrorMatches, "missing backup file for .*/out-dir/foo")
	// create a mock backup of first file
	makeSizedFile(c, filepath.Join(r.backup, "struct-0/foo.backup"), 0, nil)
	// try again
	err = rw.Update(nil, ps)
	c.Assert(err, ErrorMatches, "missing backup file for .*/out-dir/some-dir/foo")
	// create a mock backup of second entry
	makeSizedFile(c, filepath.Join(r.backup, "struct-0/some-dir/foo.backup"), 0, nil)
	// try again (preserved files need no backup)
	err = rw.Update(nil, ps)
	c.Assert(err, IsNil)

	verifyDeployedGadgetData(c, outDir, []gadgetData{
		{target: "foo", content: "update"},
		{target: "some-dir/foo", content: "update"},
		{target: "/preserved", content: "preserve"},
	})
}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterEmptyDir(c *C) {
	// some data for the gadget
	err := os.MkdirAll(filepath.Join(r.dir, "empty-dir"), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(r.dir, "non-empty/empty-dir"), 0755)
	c.Assert(err, IsNil)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					Source: "/",
					Target: "/",
				}, {
					Source: "/",
					Target: "/foo",
				}, {
					Source: "/non-empty/empty-dir/",
					Target: "/contents-of-empty/",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	err = rw.Update(nil, ps)

	verifyDirContents(c, outDir, map[string]contentType{
		// / -> /
		"empty-dir":           typeDir,
		"non-empty/empty-dir": typeDir,

		// / -> /foo
		"foo/empty-dir":           typeDir,
		"foo/non-empty/empty-dir": typeDir,
	})
}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterSameFileSkipped(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{"bar", "foo", "data"},
		{"bar", "some-dir/foo", "data"},
	}
	makeGadgetData(c, r.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingDeployedData(c, outDir, []gadgetData{
		{target: "foo", content: "same"},
		{target: "some-dir/foo", content: "same"},
	})

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					Source: "bar",
					Target: "/foo",
				}, {
					Source: "bar",
					Target: "/some-dir/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	// pretend a backup pass ran and found the files identical
	makeSizedFile(c, filepath.Join(r.backup, "struct-0/foo.same"), 0, nil)
	makeSizedFile(c, filepath.Join(r.backup, "struct-0/some-dir/foo.same"), 0, nil)

	err := rw.Update(nil, ps)
	c.Assert(err, IsNil)
	// files were not modified
	verifyDeployedGadgetData(c, outDir, []gadgetData{
		{target: "foo", content: "same"},
		{target: "some-dir/foo", content: "same"},
	})
}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterRollbackFromBackup(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{"bar", "foo", "data"},
		{"bar", "some-dir/foo", "data"},
	}
	makeGadgetData(c, r.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingDeployedData(c, outDir, []gadgetData{
		{target: "foo", content: "deployed"},
		{target: "some-dir/foo", content: "deployed"},
	})

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					Source: "bar",
					Target: "/foo",
				}, {
					Source: "bar",
					Target: "/some-dir/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	// pretend a backup pass ran and created a backup
	makeSizedFile(c, filepath.Join(r.backup, "struct-0/foo.backup"), 0, []byte("backup"))
	makeSizedFile(c, filepath.Join(r.backup, "struct-0/some-dir/foo.backup"), 0, []byte("backup"))

	err := rw.Rollback(nil, ps)
	c.Assert(err, IsNil)
	// files were restored from backup
	verifyDeployedGadgetData(c, outDir, []gadgetData{
		{target: "foo", content: "backup"},
		{target: "some-dir/foo", content: "backup"},
	})
}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterRollbackSkipSame(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{name: "bar", content: "data"},
	}
	makeGadgetData(c, r.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingDeployedData(c, outDir, []gadgetData{
		{target: "foo", content: "same"},
	})

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					Source: "bar",
					Target: "/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	// pretend a backup pass ran and created a backup
	makeSizedFile(c, filepath.Join(r.backup, "struct-0/foo.same"), 0, nil)

	err := rw.Rollback(nil, ps)
	c.Assert(err, IsNil)
	// files were not modified
	verifyDeployedGadgetData(c, outDir, []gadgetData{
		{target: "foo", content: "same"},
	})
}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterRollbackSkipPreserved(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{name: "bar", content: "data"},
	}
	makeGadgetData(c, r.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingDeployedData(c, outDir, []gadgetData{
		{target: "foo", content: "preserved"},
	})

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					Source: "bar",
					Target: "/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition:  1,
				Preserve: []string{"foo"},
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	// preserved files get no backup, but gets a stamp instead
	makeSizedFile(c, filepath.Join(r.backup, "struct-0/foo.preserve"), 0, nil)

	err := rw.Rollback(nil, ps)
	c.Assert(err, IsNil)
	// files were not modified
	verifyDeployedGadgetData(c, outDir, []gadgetData{
		{target: "foo", content: "preserved"},
	})
}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterRollbackNewFiles(c *C) {
	makeGadgetData(c, r.dir, []gadgetData{
		{name: "bar", content: "data"},
	})

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingDeployedData(c, outDir, []gadgetData{
		{target: "foo", content: "deployed"},
		{target: "some-dir/bar", content: "deployed"},
		{target: "this/is/some/deep/nesting/bar", content: "deployed"},
	})

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					Source: "bar",
					Target: "/foo",
				}, {
					Source: "bar",
					Target: "some-dir/",
				}, {
					Source: "bar",
					Target: "/this/is/some/deep/nesting/",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	// none of the marker files exists, files are new, will be removed
	err := rw.Rollback(nil, ps)
	c.Assert(err, IsNil)
	// everything was removed
	verifyDirContents(c, outDir, map[string]contentType{
		// totally empty
	})
}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterRollbackRestoreFails(c *C) {
	makeGadgetData(c, r.dir, []gadgetData{
		{name: "bar", content: "data"},
	})

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingDeployedData(c, outDir, []gadgetData{
		{target: "foo", content: "deployed"},
		{target: "some-dir/foo", content: "deployed"},
	})
	// make rollback fail when restoring
	err := os.Chmod(filepath.Join(outDir, "foo"), 0000)
	c.Assert(err, IsNil)

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					Source: "bar",
					Target: "/foo",
				}, {
					Source: "bar",
					Target: "/some-dir/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	// one file backed up, the other is new
	makeSizedFile(c, filepath.Join(r.backup, "struct-0/foo.backup"), 0, []byte("backup"))

	err = rw.Rollback(nil, ps)
	c.Assert(err, ErrorMatches, "cannot copy .*: unable to create .*/out-dir/foo: permission denied")

	// remove offending file
	c.Assert(os.Remove(filepath.Join(outDir, "foo")), IsNil)

	// make destination dir non-writable
	err = os.Chmod(filepath.Join(outDir, "some-dir"), 0555)
	c.Assert(err, IsNil)
	// restore permissions later, otherwise test suite cleanup complains
	defer os.Chmod(filepath.Join(outDir, "some-dir"), 0755)

	err = rw.Rollback(nil, ps)
	c.Assert(err, ErrorMatches, "cannot remove deployed update: remove .*/out-dir/some-dir/foo: permission denied")
}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterRollbackNotDeployed(c *C) {
	makeGadgetData(c, r.dir, []gadgetData{
		{name: "bar", content: "data"},
	})

	outDir := filepath.Join(c.MkDir(), "out-dir")

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					Source: "bar",
					Target: "/foo",
				}, {
					Source: "bar",
					Target: "/some-dir/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	// rollback does not error out if files were not deployed
	err := rw.Rollback(nil, ps)
	c.Assert(err, IsNil)
}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterRollbackDirectory(c *C) {
	makeGadgetData(c, r.dir, []gadgetData{
		{name: "some-dir/bar", content: "data"},
		{name: "some-dir/foo", content: "data"},
		{name: "some-dir/nested/nested-foo", content: "data"},
	})

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingDeployedData(c, outDir, []gadgetData{
		// some-dir/ -> /
		{target: "foo", content: "deployed"},
		{target: "bar", content: "deployed"},
		{target: "nested/nested-foo", content: "deployed"},
		// some-dir/ -> /other-dir/
		{target: "other-dir/foo", content: "deployed"},
		{target: "other-dir/bar", content: "deployed"},
		{target: "other-dir/nested/nested-foo", content: "deployed"},
		// some-dir/nested -> /other-dir/nested/
		{target: "other-dir/nested/nested/nested-foo", content: "deployed"},
		// bar -> /this/is/some/deep/nesting/
		{target: "this/is/some/deep/nesting/bar", content: "deployed"},
	})

	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			Content: []gadget.VolumeContent{
				{
					Source: "some-dir/",
					Target: "/",
				}, {
					Source: "some-dir/",
					Target: "/other-dir/",
				}, {
					Source: "some-dir/nested",
					Target: "/other-dir/nested/",
				}, {
					Source: "bar",
					Target: "/this/is/some/deep/nesting/",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	// one file backed up
	makeSizedFile(c, filepath.Join(r.backup, "struct-0/foo.backup"), 0, []byte("backup"))
	// pretend part of the directory structure existed before
	makeSizedFile(c, filepath.Join(r.backup, "struct-0/this/is/some.backup"), 0, nil)
	makeSizedFile(c, filepath.Join(r.backup, "struct-0/this/is.backup"), 0, nil)
	makeSizedFile(c, filepath.Join(r.backup, "struct-0/this.backup"), 0, nil)

	// files without a marker are new, will be removed
	err := rw.Rollback(nil, ps)
	c.Assert(err, IsNil)

	verifyDirContents(c, outDir, map[string]contentType{
		"this/is/some": typeDir,
		"foo":          typeFile,
	})
	// this one got restored
	c.Check(filepath.Join(outDir, "foo"), testutil.FileEquals, "backup")

}

func (r *mountedfilesystemTestSuite) TestMountedUpdaterEndToEndOne(c *C) {
	// some data for the gadget
	gdDeployed := []gadgetData{
		{"foo", "foo-dir/foo", "data"},
		{"bar", "bar-name", "data"},
		{"boot-assets/splash", "splash", "data"},
		{"boot-assets/some-dir/data", "some-dir/data", "data"},
		{"boot-assets/some-dir/empty-file", "some-dir/empty-file", ""},
		{"boot-assets/nested-dir/more-nested/more", "/nested-copy/more-nested/more", "data"},
	}
	gdNotDeployed := []gadgetData{
		{"foo", "/foo", "data"},
		{"boot-assets/some-dir/data", "data-copy", "data"},
		{"boot-assets/nested-dir/nested", "/nested-copy/nested", "data"},
	}
	makeGadgetData(c, r.dir, append(gdDeployed, gdNotDeployed...))
	err := os.MkdirAll(filepath.Join(r.dir, "boot-assets/empty-dir"), 0755)
	c.Assert(err, IsNil)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	makeExistingDeployedData(c, outDir, []gadgetData{
		{target: "foo", content: "can't touch this"},
		{target: "data-copy-preserved", content: "can't touch this"},
		{target: "data-copy", content: "can't touch this"},
		{target: "nested-copy/nested", content: "can't touch this"},
		{target: "not-listed", content: "can't touch this"},
	})
	// these exist in the root directory and are preserved
	preserve := []string{
		// mix entries with leading / and without
		"/foo",
		"/data-copy-preserved",
		"nested-copy/nested",
		"not-listed", // not present in 'gadget' contents
	}
	// these are preserved, but don't exist in the root, so data from gadget
	// will be deployed
	preserveButNotPresent := []string{
		"/bar-name",
		"some-dir/data",
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
					// present, hence will be deployed
					Source: "boot-assets/",
					Target: "/",
				}, {
					// would overwrite /data-copy
					Source: "boot-assets/some-dir/data",
					Target: "/data-copy-preserved",
				}, {
					Source: "boot-assets/some-dir/data",
					Target: "/data-copy",
				}, {
					// would overwrite /nested-copy/nested
					Source: "boot-assets/nested-dir/",
					Target: "/nested-copy/",
				}, {
					Source: "boot-assets",
					Target: "/boot-assets-copy/",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition:  1,
				Preserve: append(preserve, preserveButNotPresent...),
			},
		},
	}

	rw := gadget.NewMountedFilesystemUpdater(r.dir, r.backup, func(to *gadget.PositionedStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	})

	originalState := map[string]contentType{
		"foo":                 typeFile,
		"data-copy":           typeFile,
		"not-listed":          typeFile,
		"data-copy-preserved": typeFile,
		"nested-copy/nested":  typeFile,
	}
	verifyDirContents(c, outDir, originalState)

	// run the backup phase
	err = rw.Backup(nil, ps)
	c.Assert(err, IsNil)

	verifyDirContents(c, filepath.Join(r.backup, "struct-0"), map[string]contentType{
		"nested-copy.backup":           typeFile,
		"nested-copy/nested.preserve":  typeFile,
		"foo.preserve":                 typeFile,
		"data-copy-preserved.preserve": typeFile,
		"data-copy.backup":             typeFile,
	})

	// run the update phase
	err = rw.Update(nil, ps)
	c.Assert(err, IsNil)

	verifyDirContents(c, outDir, map[string]contentType{
		"foo":        typeFile,
		"not-listed": typeFile,

		// boot-assets/some-dir/data -> /data-copy
		"data-copy": typeFile,

		// boot-assets/some-dir/data -> /data-copy-preserved
		"data-copy-preserved": typeFile,

		// foo -> /foo-dir/
		"foo-dir/foo": typeFile,

		// bar -> /bar-name
		"bar-name": typeFile,

		// boot-assets/ -> /
		"splash":                      typeFile,
		"some-dir/data":               typeFile,
		"some-dir/empty-file":         typeFile,
		"nested-dir/nested":           typeFile,
		"nested-dir/more-nested/more": typeFile,
		"empty-dir":                   typeDir,

		// boot-assets -> /boot-assets-copy/
		"boot-assets-copy/boot-assets/splash":                      typeFile,
		"boot-assets-copy/boot-assets/some-dir/data":               typeFile,
		"boot-assets-copy/boot-assets/some-dir/empty-file":         typeFile,
		"boot-assets-copy/boot-assets/nested-dir/nested":           typeFile,
		"boot-assets-copy/boot-assets/nested-dir/more-nested/more": typeFile,
		"boot-assets-copy/boot-assets/empty-dir":                   typeDir,

		// boot-assets/nested-dir/ -> /nested-copy/
		"nested-copy/nested":           typeFile,
		"nested-copy/more-nested/more": typeFile,
	})

	// files that existed were preserved
	for _, en := range preserve {
		p := filepath.Join(outDir, en)
		c.Check(p, testutil.FileEquals, "can't touch this")
	}
	// everything else was deployed
	verifyDeployedGadgetData(c, outDir, gdDeployed)

	err = rw.Rollback(nil, ps)
	c.Assert(err, IsNil)
	// back to square one
	verifyDirContents(c, outDir, originalState)
}
