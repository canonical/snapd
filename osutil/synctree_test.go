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

package osutil_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type EnsureTreeStateSuite struct {
	dir   string
	globs []string
}

var _ = Suite(&EnsureTreeStateSuite{globs: []string{"*.snap"}})

func (s *EnsureTreeStateSuite) SetUpTest(c *C) {
	s.dir = c.MkDir()
}

func (s *EnsureTreeStateSuite) TestVerifiesExpectedFiles(c *C) {
	c.Assert(os.MkdirAll(filepath.Join(s.dir, "foo", "bar"), 0755), IsNil)
	name := filepath.Join(s.dir, "foo", "bar", "expected.snap")
	c.Assert(os.WriteFile(name, []byte("expected"), 0600), IsNil)
	changed, removed := mylog.Check3(osutil.EnsureTreeState(s.dir, s.globs, map[string]map[string]osutil.FileState{
		"foo/bar": {
			"expected.snap": &osutil.MemoryFileState{Content: []byte("expected"), Mode: 0600},
		},
	}))

	c.Check(changed, HasLen, 0)
	c.Check(removed, HasLen, 0)

	// The content and permissions are correct
	c.Check(name, testutil.FileEquals, "expected")
	stat := mylog.Check2(os.Stat(name))

	c.Check(stat.Mode().Perm(), Equals, os.FileMode(0600))
}

func (s *EnsureTreeStateSuite) TestCreatesMissingFiles(c *C) {
	c.Assert(os.MkdirAll(filepath.Join(s.dir, "foo"), 0755), IsNil)

	changed, removed := mylog.Check3(osutil.EnsureTreeState(s.dir, s.globs, map[string]map[string]osutil.FileState{
		"foo": {
			"missing1.snap": &osutil.MemoryFileState{Content: []byte(`content-1`), Mode: 0600},
		},
		"bar": {
			"missing2.snap": &osutil.MemoryFileState{Content: []byte(`content-2`), Mode: 0600},
		},
	}))

	c.Check(changed, DeepEquals, []string{"bar/missing2.snap", "foo/missing1.snap"})
	c.Check(removed, HasLen, 0)
}

func (s *EnsureTreeStateSuite) TestRemovesUnexpectedFiles(c *C) {
	c.Assert(os.MkdirAll(filepath.Join(s.dir, "foo"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(s.dir, "bar"), 0755), IsNil)
	name1 := filepath.Join(s.dir, "foo", "evil1.snap")
	name2 := filepath.Join(s.dir, "bar", "evil2.snap")
	c.Assert(os.WriteFile(name1, []byte(`evil-1`), 0600), IsNil)
	c.Assert(os.WriteFile(name2, []byte(`evil-2`), 0600), IsNil)

	changed, removed := mylog.Check3(osutil.EnsureTreeState(s.dir, s.globs, map[string]map[string]osutil.FileState{
		"foo": {},
	}))

	c.Check(changed, HasLen, 0)
	c.Check(removed, DeepEquals, []string{"bar/evil2.snap", "foo/evil1.snap"})
	c.Check(name1, testutil.FileAbsent)
	c.Check(name2, testutil.FileAbsent)
}

func (s *EnsureTreeStateSuite) TestRemovesEmptyDirectories(c *C) {
	c.Assert(os.MkdirAll(filepath.Join(s.dir, "foo"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(s.dir, "bar", "baz"), 0755), IsNil)
	name1 := filepath.Join(s.dir, "foo", "file1.snap")
	name2 := filepath.Join(s.dir, "foo", "unrelated")
	name3 := filepath.Join(s.dir, "bar", "baz", "file2.snap")
	c.Assert(os.WriteFile(name1, []byte(`text`), 0600), IsNil)
	c.Assert(os.WriteFile(name2, []byte(`text`), 0600), IsNil)
	c.Assert(os.WriteFile(name3, []byte(`text`), 0600), IsNil)

	_, _ := mylog.Check3(osutil.EnsureTreeState(s.dir, s.globs, nil))


	// The "foo" directory is still present, while the "bar" tree
	// has been removed.
	c.Check(filepath.Join(s.dir, "foo"), testutil.FilePresent)
	c.Check(filepath.Join(s.dir, "bar"), testutil.FileAbsent)
}

func (s *EnsureTreeStateSuite) TestIgnoresUnrelatedFiles(c *C) {
	c.Assert(os.MkdirAll(filepath.Join(s.dir, "foo"), 0755), IsNil)
	name := filepath.Join(s.dir, "foo", "unrelated")
	mylog.Check(os.WriteFile(name, []byte(`text`), 0600))

	changed, removed := mylog.Check3(osutil.EnsureTreeState(s.dir, s.globs, map[string]map[string]osutil.FileState{}))

	// Report says that nothing has changed
	c.Check(changed, HasLen, 0)
	c.Check(removed, HasLen, 0)
	// The file is still there
	c.Check(name, testutil.FilePresent)
}

func (s *EnsureTreeStateSuite) TestErrorsOnBadGlob(c *C) {
	_, _ := mylog.Check3(osutil.EnsureTreeState(s.dir, []string{"["}, nil))
	c.Check(err, ErrorMatches, `internal error: EnsureTreeState got invalid pattern "\[": syntax error in pattern`)
}

func (s *EnsureTreeStateSuite) TestErrorsOnDirectoryPathsMatchingGlobs(c *C) {
	_, _ := mylog.Check3(osutil.EnsureTreeState(s.dir, s.globs, map[string]map[string]osutil.FileState{
		"foo/bar.snap/baz": nil,
	}))
	c.Check(err, ErrorMatches, `internal error: EnsureTreeState got path "foo/bar.snap/baz" that matches glob pattern "\*.snap"`)
}

func (s *EnsureTreeStateSuite) TestErrorsOnFilenamesWithSlashes(c *C) {
	_, _ := mylog.Check3(osutil.EnsureTreeState(s.dir, s.globs, map[string]map[string]osutil.FileState{
		"foo": {
			"dir/file1.snap": &osutil.MemoryFileState{Content: []byte(`content-1`), Mode: 0600},
		},
	}))
	c.Check(err, ErrorMatches, `internal error: EnsureTreeState got filename "dir/file1.snap" in "foo", which has a path component`)
}

func (s *EnsureTreeStateSuite) TestErrorsOnFilenamesNotMatchingGlobs(c *C) {
	_, _ := mylog.Check3(osutil.EnsureTreeState(s.dir, s.globs, map[string]map[string]osutil.FileState{
		"foo": {
			"file1.not-snap": &osutil.MemoryFileState{Content: []byte(`content-1`), Mode: 0600},
		},
	}))
	c.Check(err, ErrorMatches, `internal error: EnsureTreeState got filename "file1.not-snap" in "foo", which doesn't match any glob patterns \["\*.snap"\]`)
}

func (s *EnsureTreeStateSuite) TestRemovesFilesOnError(c *C) {
	c.Assert(os.MkdirAll(filepath.Join(s.dir, "foo"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(s.dir, "bar", "dir.snap"), 0755), IsNil)
	name1 := filepath.Join(s.dir, "foo", "file1.snap")
	name2 := filepath.Join(s.dir, "bar", "file2.snap")
	name3 := filepath.Join(s.dir, "bar", "dir.snap", "sentinel")
	c.Assert(os.WriteFile(name1, []byte(`text`), 0600), IsNil)
	c.Assert(os.WriteFile(name2, []byte(`text`), 0600), IsNil)
	c.Assert(os.WriteFile(name3, []byte(`text`), 0600), IsNil)

	changed, removed := mylog.Check3(osutil.EnsureTreeState(s.dir, s.globs, map[string]map[string]osutil.FileState{
		"foo": {
			"file1.snap": &osutil.MemoryFileState{Content: []byte(`content-1`), Mode: 0600},
		},
	}))
	c.Check(err, ErrorMatches, `remove .*/bar/dir.snap: directory not empty`)
	c.Check(changed, HasLen, 0)
	c.Check(removed, DeepEquals, []string{"bar/file2.snap", "foo/file1.snap"})

	// Matching files have been removed, along with the empty directory
	c.Check(name1, testutil.FileAbsent)
	c.Check(filepath.Dir(name1), testutil.FileAbsent)
	c.Check(name2, testutil.FileAbsent)

	// But the unmatched file in the bad directory remains
	c.Check(name3, testutil.FilePresent)
}
