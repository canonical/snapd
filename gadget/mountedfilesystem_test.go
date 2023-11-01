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
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
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
	name, target, symlinkTo, content string
}

func makeGadgetData(c *C, where string, data []gadgetData) {
	for _, en := range data {
		if en.name == "" {
			continue
		}
		if strings.HasSuffix(en.name, "/") {
			err := os.MkdirAll(filepath.Join(where, en.name), 0755)
			c.Check(en.content, HasLen, 0)
			c.Assert(err, IsNil)
			continue
		}
		if en.symlinkTo != "" {
			err := os.Symlink(en.symlinkTo, filepath.Join(where, en.name))
			c.Assert(err, IsNil)
			continue
		}
		makeSizedFile(c, filepath.Join(where, en.name), 0, []byte(en.content))
	}
}

func verifyWrittenGadgetData(c *C, where string, data []gadgetData) {
	for _, en := range data {
		if en.target == "" {
			continue
		}
		if en.symlinkTo != "" {
			symlinkTarget, err := os.Readlink(filepath.Join(where, en.target))
			c.Assert(err, IsNil)
			c.Check(symlinkTarget, Equals, en.symlinkTo)
			continue
		}
		target := filepath.Join(where, en.target)
		c.Check(target, testutil.FileContains, en.content)
	}
}

func makeExistingData(c *C, where string, data []gadgetData) {
	for _, en := range data {
		if en.target == "" {
			continue
		}
		if strings.HasSuffix(en.target, "/") {
			err := os.MkdirAll(filepath.Join(where, en.target), 0755)
			c.Check(en.content, HasLen, 0)
			c.Assert(err, IsNil)
			continue
		}
		if en.symlinkTo != "" {
			err := os.Symlink(en.symlinkTo, filepath.Join(where, en.target))
			c.Assert(err, IsNil)
			continue
		}
		makeSizedFile(c, filepath.Join(where, en.target), 0, []byte(en.content))
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
	err := filepath.Walk(where, func(name string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
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
	c.Assert(err, IsNil)
	if len(expected) > 0 {
		c.Assert(got, DeepEquals, expected)
	} else {
		c.Assert(got, HasLen, 0)
	}
}

func (s *mountedfilesystemTestSuite) mustResolveVolumeContent(c *C, ps *gadget.LaidOutStructure) {
	rc, err := gadget.ResolveVolumeContent(s.dir, "", nil, ps.VolumeStructure, nil)
	c.Assert(err, IsNil)
	ps.ResolvedContent = rc
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

	// overwrites
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
		{name: "boot-assets/splash", target: "splash", content: "splash"},
		{name: "boot-assets/some-dir/data", target: "some-dir/data", content: "data"},
		{name: "boot-assets/some-dir/empty-file", target: "some-dir/empty-file"},
		{name: "boot-assets/nested-dir/nested", target: "/nested-dir/nested", content: "nested"},
		{name: "boot-assets/nested-dir/more-nested/more", target: "/nested-dir/more-nested/more", content: "more"},
	}
	makeGadgetData(c, s.dir, gd)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "ext4",
		},
	}
	rw, err := gadget.NewMountedFilesystemWriter(ps, nil)
	c.Assert(err, IsNil)

	outDir := c.MkDir()
	// boot-assets/ -> / (contents of boot assets under /)
	err = rw.WriteDirectory(outDir, filepath.Join(s.dir, "boot-assets")+"/", outDir+"/", nil)
	c.Assert(err, IsNil)

	verifyWrittenGadgetData(c, outDir, gd)
}

func (s *mountedfilesystemTestSuite) TestWriteDirectoryWhole(c *C) {
	gd := []gadgetData{
		{name: "boot-assets/splash", target: "boot-assets/splash", content: "splash"},
		{name: "boot-assets/some-dir/data", target: "boot-assets/some-dir/data", content: "data"},
		{name: "boot-assets/some-dir/empty-file", target: "boot-assets/some-dir/empty-file"},
		{name: "boot-assets/nested-dir/nested", target: "boot-assets/nested-dir/nested", content: "nested"},
		{name: "boot-assets/nested-dir/more-nested/more", target: "boot-assets//nested-dir/more-nested/more", content: "more"},
	}
	makeGadgetData(c, s.dir, gd)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "ext4",
		},
	}
	rw, err := gadget.NewMountedFilesystemWriter(ps, nil)
	c.Assert(err, IsNil)

	outDir := c.MkDir()
	// boot-assets -> / (boot-assets and children under /)
	err = rw.WriteDirectory(outDir, filepath.Join(s.dir, "boot-assets"), outDir+"/", nil)
	c.Assert(err, IsNil)

	verifyWrittenGadgetData(c, outDir, gd)
}

func (s *mountedfilesystemTestSuite) TestWriteNonDirectory(c *C) {
	gd := []gadgetData{
		{name: "foo", content: "nested"},
	}
	makeGadgetData(c, s.dir, gd)
	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "ext4",
		},
	}
	rw, err := gadget.NewMountedFilesystemWriter(ps, nil)
	c.Assert(err, IsNil)

	outDir := c.MkDir()

	err = rw.WriteDirectory(outDir, filepath.Join(s.dir, "foo")+"/", outDir, nil)
	c.Assert(err, ErrorMatches, `cannot specify trailing / for a source which is not a directory`)

	err = rw.WriteDirectory(outDir, filepath.Join(s.dir, "foo"), outDir, nil)
	c.Assert(err, ErrorMatches, `source is not a directory`)
}

type mockContentChange struct {
	path   string
	change *gadget.ContentChange
}

type mockWriteObserver struct {
	content         map[string][]*mockContentChange
	preserveTargets []string
	observeErr      error
	expectedRole    string
	c               *C
}

func (m *mockWriteObserver) Observe(op gadget.ContentOperation, partRole,
	targetRootDir, relativeTargetPath string, data *gadget.ContentChange) (gadget.ContentChangeAction, error) {
	if m.c == nil {
		panic("c is unset")
	}
	m.c.Assert(data, NotNil)
	m.c.Assert(op, Equals, gadget.ContentWrite, Commentf("unexpected operation %v", op))
	if m.content == nil {
		m.content = make(map[string][]*mockContentChange)
	}
	// the file with content that will be written must exist
	m.c.Check(osutil.FileExists(data.After) && !osutil.IsDirectory(data.After), Equals, true,
		Commentf("path %q does not exist or is a directory", data.After))
	// all files are treated as new by the writer
	m.c.Check(data.Before, Equals, "")
	m.c.Check(filepath.IsAbs(relativeTargetPath), Equals, false,
		Commentf("target path %q is absolute", relativeTargetPath))

	m.content[targetRootDir] = append(m.content[targetRootDir],
		&mockContentChange{path: relativeTargetPath, change: data})

	m.c.Check(m.expectedRole, Equals, partRole)

	if strutil.ListContains(m.preserveTargets, relativeTargetPath) {
		return gadget.ChangeIgnore, nil
	}
	return gadget.ChangeApply, m.observeErr
}

func (s *mountedfilesystemTestSuite) TestMountedWriterHappy(c *C) {
	gd := []gadgetData{
		{name: "foo", target: "foo-dir/foo", content: "foo foo foo"},
		{name: "bar", target: "bar-name", content: "bar bar bar"},
		{name: "boot-assets/splash", target: "splash", content: "splash"},
		{name: "boot-assets/some-dir/data", target: "some-dir/data", content: "data"},
		{name: "boot-assets/some-dir/data", target: "data-copy", content: "data"},
		{name: "boot-assets/some-dir/empty-file", target: "some-dir/empty-file"},
		{name: "boot-assets/nested-dir/nested", target: "/nested-copy/nested", content: "nested"},
		{name: "boot-assets/nested-dir/more-nested/more", target: "/nested-copy/more-nested/more", content: "more"},
		{name: "baz", target: "/baz", content: "baz"},
	}
	makeGadgetData(c, s.dir, gd)
	err := os.MkdirAll(filepath.Join(s.dir, "boot-assets/empty-dir"), 0755)
	c.Assert(err, IsNil)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:       "hello",
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					// single file in target directory
					UnresolvedSource: "foo",
					Target:           "/foo-dir/",
				}, {
					// single file under different name
					UnresolvedSource: "bar",
					Target:           "/bar-name",
				}, {
					// whole directory contents
					UnresolvedSource: "boot-assets/",
					Target:           "/",
				}, {
					// single file from nested directory
					UnresolvedSource: "boot-assets/some-dir/data",
					Target:           "/data-copy",
				}, {
					// contents of nested directory under new target directory
					UnresolvedSource: "boot-assets/nested-dir/",
					Target:           "/nested-copy/",
				}, {
					// contents of nested directory under new target directory
					UnresolvedSource: "baz",
					Target:           "baz",
				},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	outDir := c.MkDir()

	obs := &mockWriteObserver{
		c:            c,
		expectedRole: ps.Role(),
	}
	rw, err := gadget.NewMountedFilesystemWriter(ps, obs)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, nil)
	c.Assert(err, IsNil)

	verifyWrittenGadgetData(c, outDir, gd)
	c.Assert(osutil.IsDirectory(filepath.Join(outDir, "empty-dir")), Equals, true)

	// verify observer was notified of writes for files only
	c.Assert(obs.content, DeepEquals, map[string][]*mockContentChange{
		outDir: {
			{"foo-dir/foo", &gadget.ContentChange{After: filepath.Join(s.dir, "foo")}},
			{"bar-name", &gadget.ContentChange{After: filepath.Join(s.dir, "bar")}},

			{"nested-dir/more-nested/more", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/more-nested/more"),
			}},
			{"nested-dir/nested", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/nested"),
			}},
			{"some-dir/data", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/some-dir/data"),
			}},
			{"some-dir/empty-file", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/some-dir/empty-file"),
			}},
			{"splash", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/splash"),
			}},

			{"data-copy", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/some-dir/data"),
			}},

			{"nested-copy/more-nested/more", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/more-nested/more"),
			}},
			{"nested-copy/nested", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/nested"),
			}},

			{"baz", &gadget.ContentChange{After: filepath.Join(s.dir, "baz")}},
		},
	})
}

func (s *mountedfilesystemTestSuite) TestMountedWriterNonDirectory(c *C) {
	gd := []gadgetData{
		{name: "foo", content: "nested"},
	}
	makeGadgetData(c, s.dir, gd)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					// contents of nested directory under new target directory
					UnresolvedSource: "foo/",
					Target:           "/nested-copy/",
				},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	outDir := c.MkDir()

	rw, err := gadget.NewMountedFilesystemWriter(ps, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, nil)
	c.Assert(err, ErrorMatches, `cannot write filesystem content of source:foo/: cannot specify trailing / for a source which is not a directory`)
}

func (s *mountedfilesystemTestSuite) TestMountedWriterErrorMissingSource(c *C) {
	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					// single file in target directory
					UnresolvedSource: "foo",
					Target:           "/foo-dir/",
				},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	outDir := c.MkDir()

	rw, err := gadget.NewMountedFilesystemWriter(ps, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, nil)
	c.Assert(err, ErrorMatches, "cannot write filesystem content of source:foo: .*unable to open.*: no such file or directory")
}

func (s *mountedfilesystemTestSuite) TestMountedWriterErrorBadDestination(c *C) {
	if os.Geteuid() == 0 {
		c.Skip("the test cannot be run by the root user")
	}

	makeSizedFile(c, filepath.Join(s.dir, "foo"), 0, []byte("foo foo foo"))

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "vfat",
			Content: []gadget.VolumeContent{
				{
					// single file in target directory
					UnresolvedSource: "foo",
					Target:           "/foo-dir/",
				},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	outDir := c.MkDir()

	err := os.Chmod(outDir, 0000)
	c.Assert(err, IsNil)

	rw, err := gadget.NewMountedFilesystemWriter(ps, nil)
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

	psOverwritesDirectoryWithFile := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					// single file in target directory
					UnresolvedSource: "foo",
					Target:           "/foo-dir/",
				}, {
					// conflicts with /foo-dir directory
					UnresolvedSource: "foo-dir",
					Target:           "/",
				},
			},
		},
	}
	s.mustResolveVolumeContent(c, psOverwritesDirectoryWithFile)

	outDir := c.MkDir()

	rw, err := gadget.NewMountedFilesystemWriter(psOverwritesDirectoryWithFile, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	// can't overwrite a directory with a file
	err = rw.Write(outDir, nil)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot write filesystem content of source:foo-dir: cannot copy .*: cannot commit atomic file copy: rename %[1]s/foo-dir\.[a-zA-Z0-9]+~ %[1]s/foo-dir: file exists`, outDir))

}

func (s *mountedfilesystemTestSuite) TestMountedWriterConflictingDestinationFileOk(c *C) {
	makeGadgetData(c, s.dir, []gadgetData{
		{name: "foo", content: "foo foo foo"},
		{name: "bar", content: "bar bar bar"},
	})
	psOverwritesFile := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "bar",
					Target:           "/",
				}, {
					// overwrites data from preceding entry
					UnresolvedSource: "foo",
					Target:           "/bar",
				},
			},
		},
	}
	s.mustResolveVolumeContent(c, psOverwritesFile)

	outDir := c.MkDir()

	rw, err := gadget.NewMountedFilesystemWriter(psOverwritesFile, nil)
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

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					// single file in target directory
					UnresolvedSource: "/",
					Target:           "/foo-dir/",
				},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	outDir := c.MkDir()

	makeSizedFile(c, filepath.Join(outDir, "/foo-dir/foo/bar"), 0, nil)

	rw, err := gadget.NewMountedFilesystemWriter(ps, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, nil)
	c.Assert(err, ErrorMatches, "cannot write filesystem content of source:/: .* not a directory")
}

func (s *mountedfilesystemTestSuite) TestMountedWriterPreserve(c *C) {
	// some data for the gadget
	gdWritten := []gadgetData{
		{name: "foo", target: "foo-dir/foo", content: "data"},
		{name: "bar", target: "bar-name", content: "data"},
		{name: "boot-assets/splash", target: "splash", content: "data"},
		{name: "boot-assets/some-dir/data", target: "some-dir/data", content: "data"},
		{name: "boot-assets/some-dir/empty-file", target: "some-dir/empty-file", content: "data"},
		{name: "boot-assets/nested-dir/more-nested/more", target: "/nested-copy/more-nested/more", content: "data"},
	}
	gdNotWritten := []gadgetData{
		{name: "foo", target: "/foo", content: "data"},
		{name: "boot-assets/some-dir/data", target: "data-copy", content: "data"},
		{name: "boot-assets/nested-dir/nested", target: "/nested-copy/nested", content: "data"},
	}
	makeGadgetData(c, s.dir, append(gdWritten, gdNotWritten...))

	// these exist in the root directory and are preserved
	preserve := []string{
		// mix entries with leading / and without
		"/foo",
		"/data-copy",
		"nested-copy/nested",
		"not-listed", // not present in 'gadget' contents
	}
	// these are preserved, but don't exist in the root, so data from gadget
	// will be written
	preserveButNotPresent := []string{
		"/bar-name",
		"some-dir/data",
	}
	outDir := filepath.Join(c.MkDir(), "out-dir")

	for _, en := range preserve {
		p := filepath.Join(outDir, en)
		makeSizedFile(c, p, 0, []byte("can't touch this"))
	}

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "foo",
					Target:           "/foo-dir/",
				}, {
					// would overwrite /foo
					UnresolvedSource: "foo",
					Target:           "/",
				}, {
					// preserved, but not present, will be
					// written
					UnresolvedSource: "bar",
					Target:           "/bar-name",
				}, {
					// some-dir/data is preserved, but not
					// preset, hence will be written
					UnresolvedSource: "boot-assets/",
					Target:           "/",
				}, {
					// would overwrite /data-copy
					UnresolvedSource: "boot-assets/some-dir/data",
					Target:           "/data-copy",
				}, {
					// would overwrite /nested-copy/nested
					UnresolvedSource: "boot-assets/nested-dir/",
					Target:           "/nested-copy/",
				},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemWriter(ps, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, append(preserve, preserveButNotPresent...))
	c.Assert(err, IsNil)

	// files that existed were preserved
	for _, en := range preserve {
		p := filepath.Join(outDir, en)
		c.Check(p, testutil.FileEquals, "can't touch this")
	}
	// everything else was written
	verifyWrittenGadgetData(c, outDir, gdWritten)
}

func (s *mountedfilesystemTestSuite) TestMountedWriterPreserveWithObserver(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{name: "foo", target: "foo-dir/foo", content: "foo from gadget"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeSizedFile(c, filepath.Join(outDir, "foo"), 0, []byte("foo from disk"))

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "foo",
					// would overwrite existing foo
					Target: "foo",
				}, {
					UnresolvedSource: "foo",
					// does not exist
					Target: "foo-new",
				},
			},
		},
	}

	obs := &mockWriteObserver{
		c:            c,
		expectedRole: ps.Role(),
		preserveTargets: []string{
			"foo",
			"foo-new",
		},
	}
	rw, err := gadget.NewMountedFilesystemWriter(ps, obs)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, nil)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(outDir, "foo"), testutil.FileEquals, "foo from disk")
	c.Check(filepath.Join(outDir, "foo-new"), testutil.FileAbsent)
}

func (s *mountedfilesystemTestSuite) TestMountedWriterNonFilePreserveError(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{name: "foo", content: "data"},
	}
	makeGadgetData(c, s.dir, gd)

	preserve := []string{
		// this will be a directory
		"foo",
	}
	outDir := filepath.Join(c.MkDir(), "out-dir")
	// will conflict with preserve entry
	err := os.MkdirAll(filepath.Join(outDir, "foo"), 0755)
	c.Assert(err, IsNil)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "/",
					Target:           "/",
				},
			},
		},
	}

	rw, err := gadget.NewMountedFilesystemWriter(ps, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, preserve)
	c.Assert(err, ErrorMatches, `cannot map preserve entries for destination ".*/out-dir": preserved entry "foo" cannot be a directory`)
}

func (s *mountedfilesystemTestSuite) TestMountedWriterImplicitDir(c *C) {
	gd := []gadgetData{
		{name: "boot-assets/nested-dir/nested", target: "/nested-copy/nested-dir/nested", content: "nested"},
		{name: "boot-assets/nested-dir/more-nested/more", target: "/nested-copy/nested-dir/more-nested/more", content: "more"},
	}
	makeGadgetData(c, s.dir, gd)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					// contents of nested directory under new target directory
					UnresolvedSource: "boot-assets/nested-dir",
					Target:           "/nested-copy/",
				},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	outDir := c.MkDir()

	rw, err := gadget.NewMountedFilesystemWriter(ps, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, nil)
	c.Assert(err, IsNil)

	verifyWrittenGadgetData(c, outDir, gd)
}

func (s *mountedfilesystemTestSuite) TestMountedWriterNoFs(c *C) {
	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			// no filesystem
			Content: []gadget.VolumeContent{
				{
					// single file in target directory
					UnresolvedSource: "foo",
					Target:           "/foo-dir/",
				},
			},
			EnclosingVolume: &gadget.Volume{},
		},
	}

	rw, err := gadget.NewMountedFilesystemWriter(ps, nil)
	c.Assert(err, ErrorMatches, "structure #0 has no filesystem")
	c.Assert(rw, IsNil)
}

func (s *mountedfilesystemTestSuite) TestMountedWriterTrivialValidation(c *C) {
	rw, err := gadget.NewMountedFilesystemWriter(nil, nil)
	c.Assert(err, ErrorMatches, `internal error: \*LaidOutStructure.*`)
	c.Assert(rw, IsNil)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "/",
					Target:           "/",
				},
			},
			EnclosingVolume: &gadget.Volume{},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err = gadget.NewMountedFilesystemWriter(ps, nil)
	c.Assert(err, IsNil)

	err = rw.Write("", nil)
	c.Assert(err, ErrorMatches, "internal error: destination directory cannot be unset")

	d := c.MkDir()
	ps.ResolvedContent[0].ResolvedSource = ""
	err = rw.Write(d, nil)
	c.Assert(err, ErrorMatches, "cannot write filesystem content .* source cannot be unset")

	ps.ResolvedContent[0].ResolvedSource = "/"
	ps.ResolvedContent[0].Target = ""
	err = rw.Write(d, nil)
	c.Assert(err, ErrorMatches, "cannot write filesystem content .* target cannot be unset")
}

func (s *mountedfilesystemTestSuite) TestMountedWriterSymlinks(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{name: "foo", target: "foo", content: "data"},
		{name: "nested/foo", target: "nested/foo", content: "nested-data"},
		{name: "link", symlinkTo: "foo"},
		{name: "nested-link", symlinkTo: "nested"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{UnresolvedSource: "/", Target: "/"},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemWriter(ps, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, nil)
	c.Assert(err, IsNil)

	// everything else was written
	verifyWrittenGadgetData(c, outDir, []gadgetData{
		{target: "foo", content: "data"},
		{target: "link", symlinkTo: "foo"},
		{target: "nested/foo", content: "nested-data"},
		{target: "nested-link", symlinkTo: "nested"},
		// when read via symlink
		{target: "nested-link/foo", content: "nested-data"},
	})
}

type mockContentUpdateObserver struct {
	contentUpdate   map[string][]*mockContentChange
	contentRollback map[string][]*mockContentChange
	preserveTargets []string
	observeErr      error
	expectedRole    string
	c               *C
}

func (m *mockContentUpdateObserver) reset() {
	m.contentUpdate = nil
	m.contentRollback = nil
}

func (m *mockContentUpdateObserver) Observe(op gadget.ContentOperation, partRole,
	targetRootDir, relativeTargetPath string, data *gadget.ContentChange) (gadget.ContentChangeAction, error) {
	if m.c == nil {
		panic("c is unset")
	}
	if m.contentUpdate == nil {
		m.contentUpdate = make(map[string][]*mockContentChange)
	}
	if m.contentRollback == nil {
		m.contentRollback = make(map[string][]*mockContentChange)
	}
	m.c.Assert(data, NotNil)

	// the after content must always be set
	m.c.Check(osutil.FileExists(data.After) && !osutil.IsDirectory(data.After), Equals, true,
		Commentf("after reference path %q does not exist or is a directory", data.After))
	// they may be no before content for new files
	if data.Before != "" {
		m.c.Check(osutil.FileExists(data.Before) && !osutil.IsDirectory(data.Before), Equals, true,
			Commentf("before reference path %q does not exist or is a directory", data.Before))
	}
	m.c.Check(filepath.IsAbs(relativeTargetPath), Equals, false,
		Commentf("target path %q is absolute", relativeTargetPath))

	opData := &mockContentChange{path: relativeTargetPath, change: data}
	switch op {
	case gadget.ContentUpdate:
		m.contentUpdate[targetRootDir] = append(m.contentUpdate[targetRootDir], opData)
	case gadget.ContentRollback:
		m.contentRollback[targetRootDir] = append(m.contentRollback[targetRootDir], opData)
	default:
		m.c.Fatalf("unexpected observe operation %v", op)
	}

	m.c.Check(m.expectedRole, Equals, partRole)

	if m.observeErr != nil {
		return gadget.ChangeAbort, m.observeErr
	}
	if strutil.ListContains(m.preserveTargets, relativeTargetPath) {
		return gadget.ChangeIgnore, nil
	}
	return gadget.ChangeApply, nil
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterBackupSimple(c *C) {
	// some data for the gadget
	gdWritten := []gadgetData{
		{name: "bar", target: "bar-name", content: "data"},
		{name: "foo", target: "foo", content: "data"},
		{name: "zed", target: "zed", content: "data"},
		{name: "same-data", target: "same", content: "same"},
		// not included in volume contents
		{name: "not-written", target: "not-written", content: "data"},
	}
	makeGadgetData(c, s.dir, gdWritten)

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
	makeExistingData(c, outDir, backedUp)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "bar",
					Target:           "/bar-name",
				}, {
					UnresolvedSource: "foo",
					Target:           "/",
				}, {
					UnresolvedSource: "foo",
					Target:           "/nested/",
				}, {
					UnresolvedSource: "zed",
					Target:           "/",
				}, {
					UnresolvedSource: "same-data",
					Target:           "/same",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition:  1,
				Preserve: []string{"/zed"},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	muo := &mockContentUpdateObserver{
		c:            c,
		expectedRole: ps.Role(),
	}
	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, muo)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Backup()
	c.Assert(err, IsNil)

	// files that existed were backed up
	for _, en := range backedUp {
		backup := filepath.Join(s.backup, "struct-0", en.target+".backup")
		same := filepath.Join(s.backup, "struct-0", en.target+".same")
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
	// notified for both updated and new content
	c.Check(muo.contentUpdate, DeepEquals, map[string][]*mockContentChange{
		outDir: {
			// bar-name is a new file
			{"bar-name", &gadget.ContentChange{
				After: filepath.Join(s.dir, "bar"),
			}},
			// updates
			{"foo", &gadget.ContentChange{
				After:  filepath.Join(s.dir, "foo"),
				Before: filepath.Join(s.backup, "struct-0/foo.backup"),
			}},
			{"nested/foo", &gadget.ContentChange{
				After:  filepath.Join(s.dir, "foo"),
				Before: filepath.Join(s.backup, "struct-0/nested/foo.backup"),
			}},
		},
	})

	// running backup again (eg. after a reboot) does not error out
	err = rw.Backup()
	c.Assert(err, IsNil)
	// we are notified of all files again
	c.Check(muo.contentUpdate, DeepEquals, map[string][]*mockContentChange{
		outDir: {
			// bar-name is a new file
			{"bar-name", &gadget.ContentChange{
				After: filepath.Join(s.dir, "bar"),
			}},
			{"foo", &gadget.ContentChange{
				After:  filepath.Join(s.dir, "foo"),
				Before: filepath.Join(s.backup, "struct-0/foo.backup"),
			}},
			{"nested/foo", &gadget.ContentChange{
				After:  filepath.Join(s.dir, "foo"),
				Before: filepath.Join(s.backup, "struct-0/nested/foo.backup"),
			}},
			// same set of calls once more
			{"bar-name", &gadget.ContentChange{
				After: filepath.Join(s.dir, "bar"),
			}},
			{"foo", &gadget.ContentChange{
				After:  filepath.Join(s.dir, "foo"),
				Before: filepath.Join(s.backup, "struct-0/foo.backup"),
			}},
			{"nested/foo", &gadget.ContentChange{
				After:  filepath.Join(s.dir, "foo"),
				Before: filepath.Join(s.backup, "struct-0/nested/foo.backup"),
			}},
		},
	})
}

func (s *mountedfilesystemTestSuite) TestMountedWriterObserverErr(c *C) {
	makeGadgetData(c, s.dir, []gadgetData{
		{name: "foo", content: "data"},
	})

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{UnresolvedSource: "/", Target: "/"},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	outDir := c.MkDir()
	obs := &mockWriteObserver{
		c:            c,
		observeErr:   errors.New("observe fail"),
		expectedRole: ps.Role(),
	}
	rw, err := gadget.NewMountedFilesystemWriter(ps, obs)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Write(outDir, nil)
	c.Assert(err, ErrorMatches, "cannot write filesystem content of source:/: cannot observe file write: observe fail")
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterBackupWithDirectories(c *C) {
	// some data for the gadget
	gdWritten := []gadgetData{
		{name: "bar", content: "data"},
		{name: "some-dir/foo", content: "data"},
		{name: "empty-dir/"},
	}
	makeGadgetData(c, s.dir, gdWritten)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	// these exist in the destination directory and will be backed up
	backedUp := []gadgetData{
		// overwritten by "bar" -> "/foo"
		{target: "foo", content: "can't touch this"},
		// overwritten by some-dir/ -> /nested/
		{target: "nested/foo", content: "can't touch this"},
		// written to by bar -> /this/is/some/nested/
		{target: "this/is/some/"},
		{target: "lone-dir/"},
	}
	makeExistingData(c, outDir, backedUp)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "bar",
					Target:           "/foo",
				}, {
					UnresolvedSource: "bar",
					Target:           "/this/is/some/nested/",
				}, {
					UnresolvedSource: "some-dir/",
					Target:           "/nested/",
				}, {
					UnresolvedSource: "empty-dir/",
					Target:           "/lone-dir/",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition:  1,
				Preserve: []string{"/zed"},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Backup()
	c.Assert(err, IsNil)

	verifyDirContents(c, filepath.Join(s.backup, "struct-0"), map[string]contentType{
		"this/is/some.backup": typeFile,
		"this/is.backup":      typeFile,
		"this.backup":         typeFile,

		"nested/foo.backup": typeFile,
		"nested.backup":     typeFile,

		"foo.backup": typeFile,

		"lone-dir.backup": typeFile,
	})
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterBackupNonexistent(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{name: "bar", target: "foo", content: "data"},
		{name: "bar", target: "some-dir/foo", content: "data"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "bar",
					Target:           "/foo",
				}, {
					UnresolvedSource: "bar",
					Target:           "/some-dir/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
				// bar not in preserved files
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Backup()
	c.Assert(err, IsNil)

	backupRoot := filepath.Join(s.backup, "struct-0")
	// actually empty
	verifyDirContents(c, backupRoot, map[string]contentType{})
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterBackupFailsOnBackupDirErrors(c *C) {
	if os.Geteuid() == 0 {
		c.Skip("the test cannot be run by the root user")
	}

	outDir := filepath.Join(c.MkDir(), "out-dir")

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "bar",
					Target:           "/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = os.Chmod(s.backup, 0555)
	c.Assert(err, IsNil)
	defer os.Chmod(s.backup, 0755)

	err = rw.Backup()
	c.Assert(err, ErrorMatches, "cannot create backup directory: .*/struct-0: permission denied")
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterBackupFailsOnDestinationErrors(c *C) {
	if os.Geteuid() == 0 {
		c.Skip("the test cannot be run by the root user")
	}

	// some data for the gadget
	gd := []gadgetData{
		{name: "bar", content: "data"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingData(c, outDir, []gadgetData{
		{target: "foo", content: "same"},
	})

	err := os.Chmod(filepath.Join(outDir, "foo"), 0000)
	c.Assert(err, IsNil)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "bar",
					Target:           "/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Backup()
	c.Assert(err, ErrorMatches, "cannot backup content: cannot open destination file: open .*/out-dir/foo: permission denied")
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterBackupFailsOnBadSrcComparison(c *C) {
	if os.Geteuid() == 0 {
		c.Skip("the test cannot be run by the root user")
	}

	// some data for the gadget
	gd := []gadgetData{
		{name: "bar", content: "data"},
	}
	makeGadgetData(c, s.dir, gd)
	err := os.Chmod(filepath.Join(s.dir, "bar"), 0000)
	c.Assert(err, IsNil)

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingData(c, outDir, []gadgetData{
		{target: "foo", content: "same"},
	})

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "bar",
					Target:           "/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Backup()
	c.Assert(err, ErrorMatches, "cannot backup content: cannot checksum update file: open .*/bar: permission denied")
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterBackupFunnyNamesConflictBackup(c *C) {
	gdWritten := []gadgetData{
		{name: "bar.backup/foo", content: "data"},
		{name: "bar", content: "data"},
		{name: "foo.same/foo", content: "same-as-current"},
		{name: "foo", content: "same-as-current"},
	}
	makeGadgetData(c, s.dir, gdWritten)

	// backup stamps conflicts with bar.backup
	existingUpBar := []gadgetData{
		// will be listed first
		{target: "bar", content: "can't touch this"},
		{target: "bar.backup/foo", content: "can't touch this"},
	}
	// backup stamps conflicts with foo.same
	existingUpFoo := []gadgetData{
		// will be listed first
		{target: "foo", content: "same-as-current"},
		{target: "foo.same/foo", content: "can't touch this"},
	}

	outDirConflictsBar := filepath.Join(c.MkDir(), "out-dir-bar")
	makeExistingData(c, outDirConflictsBar, existingUpBar)

	outDirConflictsFoo := filepath.Join(c.MkDir(), "out-dir-foo")
	makeExistingData(c, outDirConflictsFoo, existingUpFoo)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{UnresolvedSource: "/", Target: "/"},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	backupBar := filepath.Join(s.backup, "backup-bar")
	backupFoo := filepath.Join(s.backup, "backup-foo")

	prefix := `cannot backup content: cannot create backup file: cannot create stamp file prefix: `
	for _, tc := range []struct {
		backupDir string
		outDir    string
		err       string
	}{
		{backupBar, outDirConflictsBar, prefix + `mkdir .*/bar.backup: not a directory`},
		{backupFoo, outDirConflictsFoo, prefix + `mkdir .*/foo.same: not a directory`},
	} {
		err := os.MkdirAll(tc.backupDir, 0755)
		c.Assert(err, IsNil)
		rw, err := gadget.NewMountedFilesystemUpdater(ps, tc.backupDir, func(to *gadget.LaidOutStructure) (string, error) {
			c.Check(to, DeepEquals, ps)
			return tc.outDir, nil
		}, nil)
		c.Assert(err, IsNil)
		c.Assert(rw, NotNil)

		err = rw.Backup()
		c.Assert(err, ErrorMatches, tc.err)
	}
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterBackupFunnyNamesOk(c *C) {
	gdWritten := []gadgetData{
		{name: "bar.backup/foo", target: "bar.backup/foo", content: "data"},
		{name: "foo.same/foo.same", target: "foo.same/foo.same", content: "same-as-current"},
		{name: "zed.preserve", target: "zed.preserve", content: "this-is-preserved"},
		{name: "new-file.same", target: "new-file.same", content: "this-is-new"},
	}
	makeGadgetData(c, s.dir, gdWritten)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	// these exist in the destination directory and will be backed up
	backedUp := []gadgetData{
		// will be listed first
		{target: "bar.backup/foo", content: "not-data"},
		{target: "foo.same/foo.same", content: "same-as-current"},
		{target: "zed.preserve", content: "to-be-preserved"},
	}
	makeExistingData(c, outDir, backedUp)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{UnresolvedSource: "/", Target: "/"},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
				Preserve: []string{
					"zed.preserve",
				},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Backup()
	c.Assert(err, IsNil)

	verifyDirContents(c, filepath.Join(s.backup, "struct-0"), map[string]contentType{
		"bar.backup.backup":     typeFile,
		"bar.backup/foo.backup": typeFile,

		"foo.same.backup":        typeFile,
		"foo.same/foo.same.same": typeFile,

		"zed.preserve.preserve": typeFile,
	})
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterBackupErrorOnSymlinkFile(c *C) {
	gd := []gadgetData{
		{name: "bar/data", target: "bar/data", content: "some data"},
		{name: "bar/foo", target: "bar/foo", content: "data"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	existing := []gadgetData{
		{target: "bar/data", content: "some data"},
		{target: "bar/foo", symlinkTo: "data"},
	}
	makeExistingData(c, outDir, existing)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{UnresolvedSource: "/", Target: "/"},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Backup()
	c.Assert(err, ErrorMatches, "cannot backup content: cannot backup file /bar/foo: symbolic links are not supported")
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterBackupErrorOnSymlinkInPrefixDir(c *C) {
	gd := []gadgetData{
		{name: "bar/nested/data", target: "bar/data", content: "some data"},
		{name: "baz/foo", target: "baz/foo", content: "data"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	existing := []gadgetData{
		{target: "bar/nested-target/data", content: "some data"},
	}
	makeExistingData(c, outDir, existing)
	// bar/nested-target -> nested
	os.Symlink("nested-target", filepath.Join(outDir, "bar/nested"))

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{UnresolvedSource: "/", Target: "/"},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Backup()
	c.Assert(err, ErrorMatches, "cannot backup content: cannot create a checkpoint for directory /bar/nested: symbolic links are not supported")
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterUpdate(c *C) {
	// some data for the gadget
	gdWritten := []gadgetData{
		{name: "foo", target: "foo-dir/foo", content: "data"},
		{name: "bar", target: "bar-name", content: "data"},
		{name: "boot-assets/splash", target: "splash", content: "data"},
		{name: "boot-assets/some-dir/data", target: "some-dir/data", content: "data"},
		{name: "boot-assets/some-dir/empty-file", target: "some-dir/empty-file", content: ""},
		{name: "boot-assets/nested-dir/more-nested/more", target: "/nested-copy/more-nested/more", content: "data"},
	}
	// data inside the gadget that will be skipped due to being part of
	// 'preserve' list
	gdNotWritten := []gadgetData{
		{name: "foo", target: "/foo", content: "data"},
		{name: "boot-assets/some-dir/data", target: "data-copy", content: "data"},
		{name: "boot-assets/nested-dir/nested", target: "/nested-copy/nested", content: "data"},
	}
	// data inside the gadget that is identical to what is already present in the target
	gdIdentical := []gadgetData{
		{name: "boot-assets/nested-dir/more-nested/identical", target: "/nested-copy/more-nested/identical", content: "same-as-target"},
		{name: "boot-assets/nested-dir/same-as-target-dir/identical", target: "/nested-copy/same-as-target-dir/identical", content: "same-as-target"},
	}

	gd := append(gdWritten, gdNotWritten...)
	gd = append(gd, gdIdentical...)
	makeGadgetData(c, s.dir, gd)

	// these exist in the root directory and are preserved
	preserve := []string{
		// mix entries with leading / and without
		"/foo",
		"/data-copy",
		"nested-copy/nested",
		"not-listed", // not present in 'gadget' contents
	}
	// these are preserved, but don't exist in the root, so data from gadget
	// will be written
	preserveButNotPresent := []string{
		"/bar-name",
		"some-dir/data",
	}
	outDir := filepath.Join(c.MkDir(), "out-dir")

	for _, en := range preserve {
		p := filepath.Join(outDir, en)
		makeSizedFile(c, p, 0, []byte("can't touch this"))
	}
	for _, en := range gdIdentical {
		makeSizedFile(c, filepath.Join(outDir, en.target), 0, []byte(en.content))
	}

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "foo",
					Target:           "/foo-dir/",
				}, {
					// would overwrite /foo
					UnresolvedSource: "foo",
					Target:           "/",
				}, {
					// preserved, but not present, will be
					// written
					UnresolvedSource: "bar",
					Target:           "/bar-name",
				}, {
					// some-dir/data is preserved, but not
					// present, hence will be written
					UnresolvedSource: "boot-assets/",
					Target:           "/",
				}, {
					// would overwrite /data-copy
					UnresolvedSource: "boot-assets/some-dir/data",
					Target:           "/data-copy",
				}, {
					// would overwrite /nested-copy/nested
					UnresolvedSource: "boot-assets/nested-dir/",
					Target:           "/nested-copy/",
				}, {
					UnresolvedSource: "boot-assets",
					Target:           "/boot-assets-copy/",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition:  1,
				Preserve: append(preserve, preserveButNotPresent...),
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	muo := &mockContentUpdateObserver{
		c:            c,
		expectedRole: ps.Role(),
	}
	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, muo)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Backup()
	c.Assert(err, IsNil)

	// identical files were identified as such
	for _, en := range gdIdentical {
		c.Check(filepath.Join(s.backup, "struct-0", en.target)+".same", testutil.FilePresent)
	}

	// only notified about content getting updated
	c.Check(muo.contentUpdate, DeepEquals, map[string][]*mockContentChange{
		outDir: {
			// the following files were not observed because they
			// are the same as the ones on disk:
			// - nested-copy/more-nested/identical
			// - nested-copy/same-as-target-dir/identical
			//
			// we still get notified about new files:
			{"foo-dir/foo", &gadget.ContentChange{
				After: filepath.Join(s.dir, "foo"),
			}},
			// in the preserve list but not present
			{"bar-name", &gadget.ContentChange{
				After: filepath.Join(s.dir, "bar"),
			}},
			// boot-assets/ -> /
			{"nested-dir/more-nested/identical", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/more-nested/identical"),
			}},
			{"nested-dir/more-nested/more", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/more-nested/more"),
			}},
			{"nested-dir/nested", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/nested"),
			}},
			{"nested-dir/same-as-target-dir/identical", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/same-as-target-dir/identical"),
			}},
			// in the preserve list but not present
			{"some-dir/data", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/some-dir/data"),
			}},
			{"some-dir/empty-file", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/some-dir/empty-file"),
			}},
			{"splash", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/splash"),
			}},
			// boot-assets/nested-dir/ -> /nested-copy/
			{"nested-copy/more-nested/more", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/more-nested/more"),
			}},
			// boot-assets -> /boot-assets-copy/
			{"boot-assets-copy/boot-assets/nested-dir/more-nested/identical", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/more-nested/identical")}},
			{"boot-assets-copy/boot-assets/nested-dir/more-nested/more", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/more-nested/more")}},
			{"boot-assets-copy/boot-assets/nested-dir/nested", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/nested"),
			}},
			{"boot-assets-copy/boot-assets/nested-dir/same-as-target-dir/identical", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/same-as-target-dir/identical"),
			}},
			{"boot-assets-copy/boot-assets/some-dir/data", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/some-dir/data"),
			}},
			{"boot-assets-copy/boot-assets/some-dir/empty-file", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/some-dir/empty-file"),
			}},
			{"boot-assets-copy/boot-assets/splash", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/splash"),
			}},
		},
	})

	err = rw.Update()
	c.Assert(err, IsNil)

	// files that existed were preserved
	for _, en := range preserve {
		p := filepath.Join(outDir, en)
		c.Check(p, testutil.FileEquals, "can't touch this")
	}
	// everything else was written
	verifyWrittenGadgetData(c, outDir, append(gdWritten, gdIdentical...))
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterDirContents(c *C) {
	// some data for the gadget
	gdWritten := []gadgetData{
		{name: "bar/foo", target: "/bar-name/foo", content: "data"},
		{name: "bar/nested/foo", target: "/bar-name/nested/foo", content: "data"},
		{name: "bar/foo", target: "/bar-copy/bar/foo", content: "data"},
		{name: "bar/nested/foo", target: "/bar-copy/bar/nested/foo", content: "data"},
		{name: "deep-nested", target: "/this/is/some/deep/nesting/deep-nested", content: "data"},
	}
	makeGadgetData(c, s.dir, gdWritten)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					// contents of bar under /bar-name/
					UnresolvedSource: "bar/",
					Target:           "/bar-name",
				}, {
					// whole bar under /bar-copy/
					UnresolvedSource: "bar",
					Target:           "/bar-copy/",
				}, {
					// deep prefix
					UnresolvedSource: "deep-nested",
					Target:           "/this/is/some/deep/nesting/",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Backup()
	c.Assert(err, IsNil)

	err = rw.Update()
	c.Assert(err, IsNil)

	verifyWrittenGadgetData(c, outDir, gdWritten)
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterExpectsBackup(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{name: "bar", target: "foo", content: "update"},
		{name: "bar", target: "some-dir/foo", content: "update"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingData(c, outDir, []gadgetData{
		{target: "foo", content: "content"},
		{target: "some-dir/foo", content: "content"},
		{target: "/preserved", content: "preserve"},
	})

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "bar",
					Target:           "/foo",
				}, {
					UnresolvedSource: "bar",
					Target:           "/some-dir/foo",
				}, {
					UnresolvedSource: "bar",
					Target:           "/preserved",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
				// bar not in preserved files
				Preserve: []string{"preserved"},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Update()
	c.Assert(err, ErrorMatches, `cannot update content: missing backup file ".*/struct-0/foo.backup" for /foo`)
	// create a mock backup of first file
	makeSizedFile(c, filepath.Join(s.backup, "struct-0/foo.backup"), 0, nil)
	// try again
	err = rw.Update()
	c.Assert(err, ErrorMatches, `cannot update content: missing backup file ".*/struct-0/some-dir/foo.backup" for /some-dir/foo`)
	// create a mock backup of second entry
	makeSizedFile(c, filepath.Join(s.backup, "struct-0/some-dir/foo.backup"), 0, nil)
	// try again (preserved files need no backup)
	err = rw.Update()
	c.Assert(err, IsNil)

	verifyWrittenGadgetData(c, outDir, []gadgetData{
		{target: "foo", content: "update"},
		{target: "some-dir/foo", content: "update"},
		{target: "/preserved", content: "preserve"},
	})
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterEmptyDir(c *C) {
	// some data for the gadget
	err := os.MkdirAll(filepath.Join(s.dir, "empty-dir"), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(s.dir, "non-empty/empty-dir"), 0755)
	c.Assert(err, IsNil)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "/",
					Target:           "/",
				}, {
					UnresolvedSource: "/",
					Target:           "/foo",
				}, {
					UnresolvedSource: "/non-empty/empty-dir/",
					Target:           "/contents-of-empty/",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Update()
	c.Assert(err, Equals, gadget.ErrNoUpdate)

	verifyDirContents(c, outDir, map[string]contentType{
		// / -> /
		"empty-dir":           typeDir,
		"non-empty/empty-dir": typeDir,

		// / -> /foo
		"foo/empty-dir":           typeDir,
		"foo/non-empty/empty-dir": typeDir,

		// /non-empty/empty-dir/ -> /contents-of-empty/
		"contents-of-empty": typeDir,
	})
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterSameFileSkipped(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{name: "bar", target: "foo", content: "data"},
		{name: "bar", target: "some-dir/foo", content: "data"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingData(c, outDir, []gadgetData{
		{target: "foo", content: "same"},
		{target: "some-dir/foo", content: "same"},
	})

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "bar",
					Target:           "/foo",
				}, {
					UnresolvedSource: "bar",
					Target:           "/some-dir/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	// pretend a backup pass ran and found the files identical
	makeSizedFile(c, filepath.Join(s.backup, "struct-0/foo.same"), 0, nil)
	makeSizedFile(c, filepath.Join(s.backup, "struct-0/some-dir/foo.same"), 0, nil)

	err = rw.Update()
	c.Assert(err, Equals, gadget.ErrNoUpdate)
	// files were not modified
	verifyWrittenGadgetData(c, outDir, []gadgetData{
		{target: "foo", content: "same"},
		{target: "some-dir/foo", content: "same"},
	})
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterLonePrefix(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{name: "bar", target: "1/nested/bar", content: "data"},
		{name: "bar", target: "2/nested/foo", content: "data"},
		{name: "bar", target: "3/nested/bar", content: "data"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "bar",
					Target:           "/1/nested/",
				}, {
					UnresolvedSource: "bar",
					Target:           "/2/nested/foo",
				}, {
					UnresolvedSource: "/",
					Target:           "/3/nested/",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Update()
	c.Assert(err, IsNil)
	verifyWrittenGadgetData(c, outDir, gd)
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterUpdateErrorOnSymlinkToFile(c *C) {
	gdWritten := []gadgetData{
		{name: "data", target: "data", content: "some data"},
		{name: "foo", symlinkTo: "data"},
	}
	makeGadgetData(c, s.dir, gdWritten)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	existing := []gadgetData{
		{target: "data", content: "some data"},
	}
	makeExistingData(c, outDir, existing)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{UnresolvedSource: "/", Target: "/"},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	// create a mock backup of first file
	makeSizedFile(c, filepath.Join(s.backup, "struct-0/data.backup"), 0, nil)

	err = rw.Update()
	c.Assert(err, ErrorMatches, "cannot update content: cannot update file .*/foo: symbolic links are not supported")
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterBackupErrorOnSymlinkToDir(c *C) {
	gd := []gadgetData{
		{name: "bar/data", target: "bar/data", content: "some data"},
		{name: "baz", symlinkTo: "bar"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	existing := []gadgetData{
		{target: "bar/data", content: "some data"},
	}
	makeExistingData(c, outDir, existing)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{UnresolvedSource: "/", Target: "/"},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	// create a mock backup of first file
	makeSizedFile(c, filepath.Join(s.backup, "struct-0/bar/data.backup"), 0, nil)
	makeSizedFile(c, filepath.Join(s.backup, "struct-0/bar.backup"), 0, nil)

	err = rw.Update()
	c.Assert(err, ErrorMatches, "cannot update content: cannot update file .*/baz: symbolic links are not supported")
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterRollbackFromBackup(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{name: "bar", target: "foo", content: "data"},
		{name: "bar", target: "some-dir/foo", content: "data"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingData(c, outDir, []gadgetData{
		{target: "foo", content: "written"},
		{target: "some-dir/foo", content: "written"},
	})

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "bar",
					Target:           "/foo",
				}, {
					UnresolvedSource: "bar",
					Target:           "/some-dir/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	muo := &mockContentUpdateObserver{
		c:            c,
		expectedRole: ps.Role(),
	}
	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, muo)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	// pretend a backup pass ran and created a backup
	makeSizedFile(c, filepath.Join(s.backup, "struct-0/foo.backup"), 0, []byte("backup"))
	makeSizedFile(c, filepath.Join(s.backup, "struct-0/some-dir/foo.backup"), 0, []byte("backup"))

	err = rw.Rollback()
	c.Assert(err, IsNil)
	// files were restored from backup
	verifyWrittenGadgetData(c, outDir, []gadgetData{
		{target: "foo", content: "backup"},
		{target: "some-dir/foo", content: "backup"},
	})

	// only notified about content getting updated
	c.Check(muo.contentRollback, DeepEquals, map[string][]*mockContentChange{
		outDir: {
			// rollback restores from the backups
			{"foo", &gadget.ContentChange{
				After:  filepath.Join(s.dir, "bar"),
				Before: filepath.Join(s.backup, "struct-0/foo.backup"),
			}},
			{"some-dir/foo", &gadget.ContentChange{
				After:  filepath.Join(s.dir, "bar"),
				Before: filepath.Join(s.backup, "struct-0/some-dir/foo.backup"),
			}},
		},
	})
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterRollbackSkipSame(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{name: "bar", content: "data"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingData(c, outDir, []gadgetData{
		{target: "foo", content: "same"},
	})

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "bar",
					Target:           "/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	muo := &mockContentUpdateObserver{
		c:            c,
		expectedRole: ps.Role(),
	}
	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, muo)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	// pretend a backup pass ran and created a backup
	makeSizedFile(c, filepath.Join(s.backup, "struct-0/foo.same"), 0, nil)

	err = rw.Rollback()
	c.Assert(err, IsNil)
	// files were not modified
	verifyWrittenGadgetData(c, outDir, []gadgetData{
		{target: "foo", content: "same"},
	})
	// identical content did not need a rollback, no notifications
	c.Check(muo.contentRollback, HasLen, 0)
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterRollbackSkipPreserved(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{name: "bar", content: "data"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingData(c, outDir, []gadgetData{
		{target: "foo", content: "preserved"},
	})

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "bar",
					Target:           "/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition:  1,
				Preserve: []string{"foo"},
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	muo := &mockContentUpdateObserver{
		c:            c,
		expectedRole: ps.Role(),
	}
	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, muo)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	// preserved files get no backup, but gets a stamp instead
	makeSizedFile(c, filepath.Join(s.backup, "struct-0/foo.preserve"), 0, nil)

	err = rw.Rollback()
	c.Assert(err, IsNil)
	// files were not modified
	verifyWrittenGadgetData(c, outDir, []gadgetData{
		{target: "foo", content: "preserved"},
	})
	// preserved content did not need a rollback, no notifications
	c.Check(muo.contentRollback, HasLen, 0)
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterRollbackNewFiles(c *C) {
	makeGadgetData(c, s.dir, []gadgetData{
		{name: "bar", content: "data"},
	})

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingData(c, outDir, []gadgetData{
		{target: "foo", content: "written"},
		{target: "some-dir/bar", content: "written"},
		{target: "this/is/some/deep/nesting/bar", content: "written"},
	})

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "bar",
					Target:           "/foo",
				}, {
					UnresolvedSource: "bar",
					Target:           "some-dir/",
				}, {
					UnresolvedSource: "bar",
					Target:           "/this/is/some/deep/nesting/",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	muo := &mockContentUpdateObserver{
		c:            c,
		expectedRole: ps.Role(),
	}
	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, muo)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	// none of the marker files exists, files are new, will be removed
	err = rw.Rollback()
	c.Assert(err, IsNil)
	// everything was removed
	verifyDirContents(c, outDir, map[string]contentType{})
	// new files were rolled back
	c.Check(muo.contentRollback, DeepEquals, map[string][]*mockContentChange{
		outDir: {
			// files did not exist, so there was no 'before' content
			{"foo", &gadget.ContentChange{
				After:  filepath.Join(s.dir, "bar"),
				Before: "",
			}},
			{"some-dir/bar", &gadget.ContentChange{
				After:  filepath.Join(s.dir, "bar"),
				Before: "",
			}},
			{"this/is/some/deep/nesting/bar", &gadget.ContentChange{
				After:  filepath.Join(s.dir, "bar"),
				Before: "",
			}},
		},
	})
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterRollbackRestoreFails(c *C) {
	if os.Geteuid() == 0 {
		c.Skip("the test cannot be run by the root user")
	}

	makeGadgetData(c, s.dir, []gadgetData{
		{name: "bar", content: "data"},
	})

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingData(c, outDir, []gadgetData{
		{target: "foo", content: "written"},
		{target: "some-dir/foo", content: "written"},
	})
	// the file exists, and cannot be modified directly, rollback will still
	// restore the backup as we atomically swap copies with rename()
	err := os.Chmod(filepath.Join(outDir, "foo"), 0000)
	c.Assert(err, IsNil)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "bar",
					Target:           "/foo",
				}, {
					UnresolvedSource: "bar",
					Target:           "/some-dir/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	// one file backed up, the other is new
	makeSizedFile(c, filepath.Join(s.backup, "struct-0/foo.backup"), 0, []byte("backup"))

	err = rw.Rollback()
	c.Assert(err, IsNil)
	// the file was restored
	c.Check(filepath.Join(outDir, "foo"), testutil.FileEquals, "backup")
	// directory was removed
	c.Check(osutil.IsDirectory(filepath.Join(outDir, "some-dir")), Equals, false)

	// mock the data again
	makeExistingData(c, outDir, []gadgetData{
		{target: "foo", content: "written"},
		{target: "some-dir/foo", content: "written"},
	})

	// make the directory non-writable
	err = os.Chmod(filepath.Join(outDir, "some-dir"), 0555)
	c.Assert(err, IsNil)
	// restore permissions later, otherwise test suite cleanup complains
	defer os.Chmod(filepath.Join(outDir, "some-dir"), 0755)

	err = rw.Rollback()
	c.Assert(err, ErrorMatches, "cannot rollback content: cannot remove written update: remove .*/out-dir/some-dir/foo: permission denied")
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterRollbackNotWritten(c *C) {
	makeGadgetData(c, s.dir, []gadgetData{
		{name: "bar", content: "data"},
	})

	outDir := filepath.Join(c.MkDir(), "out-dir")

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "bar",
					Target:           "/foo",
				}, {
					UnresolvedSource: "bar",
					Target:           "/some-dir/foo",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	muo := &mockContentUpdateObserver{
		c:            c,
		expectedRole: ps.Role(),
	}
	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, muo)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	// rollback does not error out if files were not written
	err = rw.Rollback()
	c.Assert(err, IsNil)
	// observer would be notified that files were to be written, and so it
	// must be notified when they would be rolled back
	c.Check(muo.contentRollback, DeepEquals, map[string][]*mockContentChange{
		outDir: {
			// rollback restores from the backups
			{"foo", &gadget.ContentChange{
				After: filepath.Join(s.dir, "bar"),
			}},
			{"some-dir/foo", &gadget.ContentChange{
				After: filepath.Join(s.dir, "bar"),
			}},
		},
	})
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterRollbackDirectory(c *C) {
	makeGadgetData(c, s.dir, []gadgetData{
		{name: "some-dir/bar", content: "data"},
		{name: "some-dir/foo", content: "data"},
		{name: "some-dir/nested/nested-foo", content: "data"},
		{name: "empty-dir/"},
		{name: "bar", content: "data"},
	})

	outDir := filepath.Join(c.MkDir(), "out-dir")
	makeExistingData(c, outDir, []gadgetData{
		// some-dir/ -> /
		{target: "foo", content: "written"},
		{target: "bar", content: "written"},
		{target: "nested/nested-foo", content: "written"},
		// some-dir/ -> /other-dir/
		{target: "other-dir/foo", content: "written"},
		{target: "other-dir/bar", content: "written"},
		{target: "other-dir/nested/nested-foo", content: "written"},
		// some-dir/nested -> /other-dir/nested/
		{target: "other-dir/nested/nested/nested-foo", content: "written"},
		// bar -> /this/is/some/deep/nesting/
		{target: "this/is/some/deep/nesting/bar", content: "written"},
		{target: "lone-dir/"},
	})

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "some-dir/",
					Target:           "/",
				}, {
					UnresolvedSource: "some-dir/",
					Target:           "/other-dir/",
				}, {
					UnresolvedSource: "some-dir/nested",
					Target:           "/other-dir/nested/",
				}, {
					UnresolvedSource: "bar",
					Target:           "/this/is/some/deep/nesting/",
				}, {
					UnresolvedSource: "empty-dir/",
					Target:           "/lone-dir/",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	muo := &mockContentUpdateObserver{
		c:            c,
		expectedRole: ps.Role(),
	}
	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, muo)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	// one file backed up
	makeSizedFile(c, filepath.Join(s.backup, "struct-0/foo.backup"), 0, []byte("backup"))
	// pretend part of the directory structure existed before
	makeSizedFile(c, filepath.Join(s.backup, "struct-0/this/is/some.backup"), 0, nil)
	makeSizedFile(c, filepath.Join(s.backup, "struct-0/this/is.backup"), 0, nil)
	makeSizedFile(c, filepath.Join(s.backup, "struct-0/this.backup"), 0, nil)
	makeSizedFile(c, filepath.Join(s.backup, "struct-0/lone-dir.backup"), 0, nil)

	// files without a marker are new, will be removed
	err = rw.Rollback()
	c.Assert(err, IsNil)

	verifyDirContents(c, outDir, map[string]contentType{
		"lone-dir":     typeDir,
		"this/is/some": typeDir,
		"foo":          typeFile,
	})
	// this one got restored
	c.Check(filepath.Join(outDir, "foo"), testutil.FileEquals, "backup")

	c.Check(muo.contentRollback, DeepEquals, map[string][]*mockContentChange{
		outDir: {
			// a new file
			{"bar", &gadget.ContentChange{
				After: filepath.Join(s.dir, "some-dir/bar"),
			}},
			// this file was restored from backup
			{"foo", &gadget.ContentChange{
				After:  filepath.Join(s.dir, "some-dir/foo"),
				Before: filepath.Join(s.backup, "struct-0/foo.backup"),
			}},
			// new files till the end
			{"nested/nested-foo", &gadget.ContentChange{
				After: filepath.Join(s.dir, "some-dir/nested/nested-foo"),
			}},
			{"other-dir/bar", &gadget.ContentChange{
				After: filepath.Join(s.dir, "some-dir/bar"),
			}},
			{"other-dir/foo", &gadget.ContentChange{
				After: filepath.Join(s.dir, "some-dir/foo"),
			}},
			{"other-dir/nested/nested-foo", &gadget.ContentChange{
				After: filepath.Join(s.dir, "some-dir/nested/nested-foo"),
			}},
			{"other-dir/nested/nested/nested-foo", &gadget.ContentChange{
				After: filepath.Join(s.dir, "some-dir/nested/nested-foo"),
			}},
			{"this/is/some/deep/nesting/bar", &gadget.ContentChange{
				After: filepath.Join(s.dir, "bar"),
			}},
		},
	})
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterEndToEndOne(c *C) {
	// some data for the gadget
	gdWritten := []gadgetData{
		{name: "foo", target: "foo-dir/foo", content: "data"},
		{name: "bar", target: "bar-name", content: "data"},
		{name: "boot-assets/splash", target: "splash", content: "data"},
		{name: "boot-assets/dtb", target: "dtb", content: "data"},
		{name: "boot-assets/some-dir/data", target: "some-dir/data", content: "data"},
		{name: "boot-assets/some-dir/empty-file", target: "some-dir/empty-file", content: ""},
		{name: "boot-assets/nested-dir/more-nested/more", target: "/nested-copy/more-nested/more", content: "data"},
	}
	gdNotWritten := []gadgetData{
		{name: "foo", target: "/foo", content: "data"},
		{name: "boot-assets/some-dir/data", target: "data-copy", content: "data"},
		{name: "boot-assets/nested-dir/nested", target: "/nested-copy/nested", content: "data"},
		{name: "preserved/same-content", target: "preserved/same-content", content: "can't touch this"},
	}
	gdSameContent := []gadgetData{
		{name: "foo", target: "/foo-same", content: "data"},
	}
	makeGadgetData(c, s.dir, append(gdWritten, gdNotWritten...))
	err := os.MkdirAll(filepath.Join(s.dir, "boot-assets/empty-dir"), 0755)
	c.Assert(err, IsNil)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	makeExistingData(c, outDir, []gadgetData{
		{target: "dtb", content: "updated"},
		{target: "foo", content: "can't touch this"},
		{target: "foo-same", content: "data"},
		{target: "data-copy-preserved", content: "can't touch this"},
		{target: "data-copy", content: "can't touch this"},
		{target: "nested-copy/nested", content: "can't touch this"},
		{target: "nested-copy/more-nested/"},
		{target: "not-listed", content: "can't touch this"},
		{target: "unrelated/data/here", content: "unrelated"},
		{target: "preserved/same-content-for-list", content: "can't touch this"},
		{target: "preserved/same-content-for-observer", content: "can't touch this"},
	})
	// these exist in the root directory and are preserved
	preserve := []string{
		// mix entries with leading / and without
		"/foo",
		"/data-copy-preserved",
		"nested-copy/nested",
		"not-listed", // not present in 'gadget' contents
		"preserved/same-content-for-list",
	}
	// these are preserved, but don't exist in the root, so data from gadget
	// will be written
	preserveButNotPresent := []string{
		"/bar-name",
		"some-dir/data",
	}

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "foo",
					Target:           "/foo-dir/",
				}, {
					// would overwrite /foo
					UnresolvedSource: "foo",
					Target:           "/",
				}, {
					// nothing written, content is unchanged
					UnresolvedSource: "foo",
					Target:           "/foo-same",
				}, {
					// preserved, but not present, will be
					// written
					UnresolvedSource: "bar",
					Target:           "/bar-name",
				}, {
					// some-dir/data is preserved, but not
					// present, hence will be written
					UnresolvedSource: "boot-assets/",
					Target:           "/",
				}, {
					// would overwrite /data-copy
					UnresolvedSource: "boot-assets/some-dir/data",
					Target:           "/data-copy-preserved",
				}, {
					UnresolvedSource: "boot-assets/some-dir/data",
					Target:           "/data-copy",
				}, {
					// would overwrite /nested-copy/nested
					UnresolvedSource: "boot-assets/nested-dir/",
					Target:           "/nested-copy/",
				}, {
					UnresolvedSource: "boot-assets",
					Target:           "/boot-assets-copy/",
				}, {
					UnresolvedSource: "/boot-assets/empty-dir/",
					Target:           "/lone-dir/nested/",
				}, {
					UnresolvedSource: "preserved/same-content",
					Target:           "preserved/same-content-for-list",
				}, {
					UnresolvedSource: "preserved/same-content",
					Target:           "preserved/same-content-for-observer",
				},
			},
			Update: gadget.VolumeUpdate{
				Edition:  1,
				Preserve: append(preserve, preserveButNotPresent...),
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	muo := &mockContentUpdateObserver{
		c:            c,
		expectedRole: ps.Role(),
		preserveTargets: []string{
			"preserved/same-content-for-observer",
		},
	}
	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, muo)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	originalState := map[string]contentType{
		"foo":                                 typeFile,
		"foo-same":                            typeFile,
		"dtb":                                 typeFile,
		"data-copy":                           typeFile,
		"not-listed":                          typeFile,
		"data-copy-preserved":                 typeFile,
		"nested-copy/nested":                  typeFile,
		"nested-copy/more-nested":             typeDir,
		"unrelated/data/here":                 typeFile,
		"preserved/same-content-for-list":     typeFile,
		"preserved/same-content-for-observer": typeFile,
	}
	verifyDirContents(c, outDir, originalState)

	// run the backup phase
	err = rw.Backup()
	c.Assert(err, IsNil)

	verifyDirContents(c, filepath.Join(s.backup, "struct-0"), map[string]contentType{
		"nested-copy.backup":                       typeFile,
		"nested-copy/nested.preserve":              typeFile,
		"nested-copy/more-nested.backup":           typeFile,
		"foo.preserve":                             typeFile,
		"foo-same.same":                            typeFile,
		"data-copy-preserved.preserve":             typeFile,
		"data-copy.backup":                         typeFile,
		"dtb.backup":                               typeFile,
		"preserved.backup":                         typeFile,
		"preserved/same-content-for-list.preserve": typeFile,
		"preserved/same-content-for-observer.same": typeFile,
	})

	expectedObservedContentChange := map[string][]*mockContentChange{
		// observer is notified about changed and new files
		outDir: {
			{"foo-dir/foo", &gadget.ContentChange{
				After: filepath.Join(s.dir, "foo"),
			}},
			{"bar-name", &gadget.ContentChange{
				After: filepath.Join(s.dir, "bar"),
			}},
			// update with changed content
			{"dtb", &gadget.ContentChange{
				After:  filepath.Join(s.dir, "boot-assets/dtb"),
				Before: filepath.Join(s.backup, "struct-0/dtb.backup"),
			}},
			// new files
			{"nested-dir/more-nested/more", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/more-nested/more"),
			}},
			{"nested-dir/nested", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/nested"),
			}},
			{"some-dir/data", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/some-dir/data"),
			}},
			// new files
			{"some-dir/empty-file", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/some-dir/empty-file"),
			}},
			{"splash", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/splash"),
			}},
			// update with changed content
			{"data-copy", &gadget.ContentChange{
				After:  filepath.Join(s.dir, "boot-assets/some-dir/data"),
				Before: filepath.Join(s.backup, "struct-0/data-copy.backup"),
			}},
			// new files
			{"nested-copy/more-nested/more", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/more-nested/more"),
			}},
			{"boot-assets-copy/boot-assets/dtb", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/dtb"),
			}},
			{"boot-assets-copy/boot-assets/nested-dir/more-nested/more", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/more-nested/more")},
			},
			{"boot-assets-copy/boot-assets/nested-dir/nested", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/nested-dir/nested"),
			}},
			{"boot-assets-copy/boot-assets/some-dir/data", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/some-dir/data"),
			}},
			{"boot-assets-copy/boot-assets/some-dir/empty-file", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/some-dir/empty-file"),
			}},
			{"boot-assets-copy/boot-assets/splash", &gadget.ContentChange{
				After: filepath.Join(s.dir, "boot-assets/splash"),
			}},
		},
	}

	// observe calls happen in the order the structure content gets analyzed
	c.Check(muo.contentUpdate, DeepEquals, expectedObservedContentChange)

	// run the update phase
	err = rw.Update()
	c.Assert(err, IsNil)

	verifyDirContents(c, outDir, map[string]contentType{
		"foo":        typeFile,
		"foo-same":   typeFile,
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
		"dtb":                         typeFile,
		"splash":                      typeFile,
		"some-dir/data":               typeFile,
		"some-dir/empty-file":         typeFile,
		"nested-dir/nested":           typeFile,
		"nested-dir/more-nested/more": typeFile,
		"empty-dir":                   typeDir,

		// boot-assets -> /boot-assets-copy/
		"boot-assets-copy/boot-assets/dtb":                         typeFile,
		"boot-assets-copy/boot-assets/splash":                      typeFile,
		"boot-assets-copy/boot-assets/some-dir/data":               typeFile,
		"boot-assets-copy/boot-assets/some-dir/empty-file":         typeFile,
		"boot-assets-copy/boot-assets/nested-dir/nested":           typeFile,
		"boot-assets-copy/boot-assets/nested-dir/more-nested/more": typeFile,
		"boot-assets-copy/boot-assets/empty-dir":                   typeDir,

		// boot-assets/nested-dir/ -> /nested-copy/
		"nested-copy/nested":           typeFile,
		"nested-copy/more-nested/more": typeFile,

		// data that was not part of the update
		"unrelated/data/here": typeFile,

		// boot-assets/empty-dir/ -> /lone-dir/nested/
		"lone-dir/nested": typeDir,

		"preserved/same-content-for-list":     typeFile,
		"preserved/same-content-for-observer": typeFile,
	})

	// files that existed were preserved
	for _, en := range preserve {
		p := filepath.Join(outDir, en)
		c.Check(p, testutil.FileEquals, "can't touch this")
	}
	// everything else was written
	verifyWrittenGadgetData(c, outDir, append(gdWritten, gdSameContent...))

	err = rw.Rollback()
	c.Assert(err, IsNil)
	// back to square one
	verifyDirContents(c, outDir, originalState)

	c.Check(muo.contentRollback, DeepEquals, expectedObservedContentChange)
	// call rollback once more, we should observe the same files again
	muo.contentRollback = nil
	err = rw.Rollback()
	c.Assert(err, IsNil)
	c.Check(muo.contentRollback, DeepEquals, expectedObservedContentChange)
	// file contents are unchanged
	verifyDirContents(c, outDir, originalState)
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterTrivialValidation(c *C) {
	psNoFs := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2048,
			// no filesystem
			Content:         []gadget.VolumeContent{},
			EnclosingVolume: &gadget.Volume{},
		},
	}
	s.mustResolveVolumeContent(c, psNoFs)

	lookupFail := func(to *gadget.LaidOutStructure) (string, error) {
		c.Fatalf("unexpected call")
		return "", nil
	}

	rw, err := gadget.NewMountedFilesystemUpdater(psNoFs, s.backup, lookupFail, nil)
	c.Assert(err, ErrorMatches, "structure #0 has no filesystem")
	c.Assert(rw, IsNil)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:            2048,
			Filesystem:      "ext4",
			Content:         []gadget.VolumeContent{},
			EnclosingVolume: &gadget.Volume{},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err = gadget.NewMountedFilesystemUpdater(ps, "", lookupFail, nil)
	c.Assert(err, ErrorMatches, `internal error: backup directory must not be unset`)
	c.Assert(rw, IsNil)

	rw, err = gadget.NewMountedFilesystemUpdater(ps, s.backup, nil, nil)
	c.Assert(err, ErrorMatches, `internal error: mount lookup helper must be provided`)
	c.Assert(rw, IsNil)

	rw, err = gadget.NewMountedFilesystemUpdater(nil, s.backup, lookupFail, nil)
	c.Assert(err, ErrorMatches, `internal error: \*LaidOutStructure.*`)
	c.Assert(rw, IsNil)

	lookupOk := func(to *gadget.LaidOutStructure) (string, error) {
		return filepath.Join(s.dir, "foobar"), nil
	}

	for _, tc := range []struct {
		src, dst string
		match    string
	}{
		{src: "", dst: "", match: "internal error: source cannot be unset"},
		{src: "/", dst: "", match: "internal error: target cannot be unset"},
	} {
		testPs := &gadget.LaidOutStructure{
			VolumeStructure: &gadget.VolumeStructure{
				Size:       2048,
				Filesystem: "ext4",
				Content: []gadget.VolumeContent{
					{UnresolvedSource: "/", Target: "/"},
				},
			},
		}
		s.mustResolveVolumeContent(c, testPs)
		testPs.ResolvedContent[0].ResolvedSource = tc.src
		testPs.ResolvedContent[0].Target = tc.dst

		rw, err := gadget.NewMountedFilesystemUpdater(testPs, s.backup, lookupOk, nil)
		c.Assert(err, IsNil)
		c.Assert(rw, NotNil)

		err = rw.Update()
		c.Assert(err, ErrorMatches, "cannot update content: "+tc.match)

		err = rw.Backup()
		c.Assert(err, ErrorMatches, "cannot backup content: "+tc.match)

		err = rw.Rollback()
		c.Assert(err, ErrorMatches, "cannot rollback content: "+tc.match)
	}
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterMountLookupFail(c *C) {
	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{UnresolvedSource: "/", Target: "/"},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	lookupFail := func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return "", errors.New("fail fail fail")
	}

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, lookupFail, nil)
	c.Assert(err, ErrorMatches, "cannot find mount location of structure #0: fail fail fail")
	c.Assert(rw, IsNil)
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterNonFilePreserveError(c *C) {
	// some data for the gadget
	gd := []gadgetData{
		{name: "foo", content: "data"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")
	// will conflict with preserve entry
	err := os.MkdirAll(filepath.Join(outDir, "foo"), 0755)
	c.Assert(err, IsNil)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{UnresolvedSource: "/", Target: "/"},
			},
			Update: gadget.VolumeUpdate{
				Preserve: []string{"foo"},
				Edition:  1,
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup, func(to *gadget.LaidOutStructure) (string, error) {
		c.Check(to, DeepEquals, ps)
		return outDir, nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	err = rw.Backup()
	c.Check(err, ErrorMatches, `cannot map preserve entries for mount location ".*/out-dir": preserved entry "foo" cannot be a directory`)
	err = rw.Update()
	c.Check(err, ErrorMatches, `cannot map preserve entries for mount location ".*/out-dir": preserved entry "foo" cannot be a directory`)
	err = rw.Rollback()
	c.Check(err, ErrorMatches, `cannot map preserve entries for mount location ".*/out-dir": preserved entry "foo" cannot be a directory`)
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterObserverPreservesBootAssets(c *C) {
	// mirror pc-amd64-gadget
	gd := []gadgetData{
		{name: "grub.conf", content: "grub.conf from gadget"},
		{name: "grubx64.efi", content: "grubx64.efi from gadget"},
		{name: "foo", content: "foo from gadget"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	existingGrubCfg := `# Snapd-Boot-Config-Edition: 1
managed grub.cfg from disk`
	makeExistingData(c, outDir, []gadgetData{
		{target: "EFI/boot/grubx64.efi", content: "grubx64.efi from disk"},
		{target: "EFI/ubuntu/grub.cfg", content: existingGrubCfg},
		{target: "foo", content: "foo from disk"},
	})
	// based on pc gadget
	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Role:       gadget.SystemBoot,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{UnresolvedSource: "grubx64.efi", Target: "EFI/boot/grubx64.efi"},
				{UnresolvedSource: "grub.conf", Target: "EFI/ubuntu/grub.cfg"},
				{UnresolvedSource: "foo", Target: "foo"},
			},
			Update: gadget.VolumeUpdate{
				Preserve: []string{"foo"},
				Edition:  1,
			},
		},
	}
	s.mustResolveVolumeContent(c, ps)

	obs := &mockContentUpdateObserver{
		c:               c,
		expectedRole:    ps.Role(),
		preserveTargets: []string{"EFI/ubuntu/grub.cfg"},
	}
	rw, err := gadget.NewMountedFilesystemUpdater(ps, s.backup,
		func(to *gadget.LaidOutStructure) (string, error) {
			c.Check(to, DeepEquals, ps)
			return outDir, nil
		},
		obs)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	expectedFileStamps := map[string]contentType{
		"EFI.backup":                  typeFile,
		"EFI/boot/grubx64.efi.backup": typeFile,
		"EFI/boot.backup":             typeFile,
		"EFI/ubuntu.backup":           typeFile,

		// listed explicitly in the structure
		"foo.preserve": typeFile,
		// requested by observer
		"EFI/ubuntu/grub.cfg.ignore": typeFile,
	}

	for _, step := range []struct {
		name string
		call func() error
	}{
		{name: "backup", call: rw.Backup},
		{name: "update", call: rw.Update},
		{name: "rollback", call: rw.Rollback},
	} {
		c.Logf("step: %v", step.name)
		err := step.call()
		c.Assert(err, IsNil)

		switch step.name {
		case "backup":
			c.Check(filepath.Join(outDir, "EFI/boot/grubx64.efi"), testutil.FileEquals, "grubx64.efi from disk")
			c.Check(filepath.Join(outDir, "EFI/ubuntu/grub.cfg"), testutil.FileEquals, existingGrubCfg)
			c.Check(filepath.Join(outDir, "foo"), testutil.FileEquals, "foo from disk")
		case "update":
			c.Check(filepath.Join(outDir, "EFI/boot/grubx64.efi"), testutil.FileEquals, "grubx64.efi from gadget")
			c.Check(filepath.Join(outDir, "EFI/ubuntu/grub.cfg"), testutil.FileEquals,
				`# Snapd-Boot-Config-Edition: 1
managed grub.cfg from disk`)
			c.Check(filepath.Join(outDir, "foo"), testutil.FileEquals, "foo from disk")
		case "rollback":
			c.Check(filepath.Join(outDir, "EFI/boot/grubx64.efi"), testutil.FileEquals, "grubx64.efi from disk")
			c.Check(filepath.Join(outDir, "EFI/ubuntu/grub.cfg"), testutil.FileEquals, existingGrubCfg)
			c.Check(filepath.Join(outDir, "foo"), testutil.FileEquals, "foo from disk")
		default:
			c.Fatalf("unexpected step: %q", step.name)
		}
		verifyDirContents(c, filepath.Join(s.backup, "struct-0"), expectedFileStamps)
	}
}

var (
	// based on pc gadget
	psForObserver = &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       2048,
			Role:       gadget.SystemBoot,
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{UnresolvedSource: "foo", Target: "foo"},
			},
			Update: gadget.VolumeUpdate{
				Edition: 1,
			},
		},
	}
)

func (s *mountedfilesystemTestSuite) TestMountedUpdaterObserverPreserveNewFile(c *C) {
	gd := []gadgetData{
		{name: "foo", content: "foo from gadget"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	obs := &mockContentUpdateObserver{
		c:               c,
		expectedRole:    psForObserver.Role(),
		preserveTargets: []string{"foo"},
	}
	rw, err := gadget.NewMountedFilesystemUpdater(psForObserver, s.backup,
		func(to *gadget.LaidOutStructure) (string, error) {
			c.Check(to, DeepEquals, psForObserver)
			s.mustResolveVolumeContent(c, psForObserver)
			return outDir, nil
		},
		obs)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	expectedNewFileChanges := map[string][]*mockContentChange{
		outDir: {
			{"foo", &gadget.ContentChange{After: filepath.Join(s.dir, "foo")}},
		},
	}
	expectedStamps := map[string]contentType{
		"foo.ignore": typeFile,
	}
	// file does not exist
	err = rw.Backup()
	c.Assert(err, IsNil)
	// no stamps
	verifyDirContents(c, filepath.Join(s.backup, "struct-0"), expectedStamps)
	// observer got notified about change
	c.Assert(obs.contentUpdate, DeepEquals, expectedNewFileChanges)

	obs.reset()

	// try the same pass again
	err = rw.Backup()
	c.Assert(err, IsNil)
	verifyDirContents(c, filepath.Join(s.backup, "struct-0"), expectedStamps)
	// observer already requested the change to be ignored once
	c.Assert(obs.contentUpdate, HasLen, 0)

	// file does not exist and is not written
	err = rw.Update()
	c.Assert(err, Equals, gadget.ErrNoUpdate)
	c.Assert(filepath.Join(outDir, "foo"), testutil.FileAbsent)

	// nothing happens on rollback
	err = rw.Rollback()
	c.Assert(err, IsNil)
	c.Assert(filepath.Join(outDir, "foo"), testutil.FileAbsent)
}

func (s *mountedfilesystemTestSuite) TestMountedUpdaterObserverPreserveExistingFile(c *C) {
	gd := []gadgetData{
		{name: "foo", content: "foo from gadget"},
	}
	makeGadgetData(c, s.dir, gd)

	outDir := filepath.Join(c.MkDir(), "out-dir")

	obs := &mockContentUpdateObserver{
		c:               c,
		expectedRole:    psForObserver.Role(),
		preserveTargets: []string{"foo"},
	}
	rw, err := gadget.NewMountedFilesystemUpdater(psForObserver, s.backup,
		func(to *gadget.LaidOutStructure) (string, error) {
			c.Check(to, DeepEquals, psForObserver)
			s.mustResolveVolumeContent(c, psForObserver)
			return outDir, nil
		},
		obs)
	c.Assert(err, IsNil)
	c.Assert(rw, NotNil)

	// file exists now
	makeExistingData(c, outDir, []gadgetData{
		{target: "foo", content: "foo from disk"},
	})
	expectedExistingFileChanges := map[string][]*mockContentChange{
		outDir: {
			{"foo", &gadget.ContentChange{
				After:  filepath.Join(s.dir, "foo"),
				Before: filepath.Join(s.backup, "struct-0/foo.backup"),
			}},
		},
	}
	expectedExistingFileStamps := map[string]contentType{
		"foo.ignore": typeFile,
	}
	err = rw.Backup()
	c.Assert(err, IsNil)
	verifyDirContents(c, filepath.Join(s.backup, "struct-0"), expectedExistingFileStamps)
	// get notified about change
	c.Assert(obs.contentUpdate, DeepEquals, expectedExistingFileChanges)

	obs.reset()
	// backup called again (eg. after reset)
	err = rw.Backup()
	c.Assert(err, IsNil)
	verifyDirContents(c, filepath.Join(s.backup, "struct-0"), expectedExistingFileStamps)
	// observer already requested the change to be ignored once
	c.Assert(obs.contentUpdate, HasLen, 0)

	// and nothing gets updated
	err = rw.Update()
	c.Assert(err, Equals, gadget.ErrNoUpdate)
	c.Check(filepath.Join(outDir, "foo"), testutil.FileEquals, "foo from disk")

	// the file existed and was preserved, nothing gets removed on rollback
	err = rw.Rollback()
	c.Assert(err, IsNil)
	c.Check(filepath.Join(outDir, "foo"), testutil.FileEquals, "foo from disk")
}
