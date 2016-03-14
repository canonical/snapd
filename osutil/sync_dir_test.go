// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/osutil"
)

type JanitorSuite struct {
	dir      string
	j        osutil.Janitor
	uid, gid uint32
}

var _ = Suite(&JanitorSuite{
	uid: uint32(os.Getuid()),
	gid: uint32(os.Getgid()),
})

func (s *JanitorSuite) SetUpTest(c *C) {
	s.dir = c.MkDir()
	s.j = osutil.Janitor{
		Path: s.dir,
		Glob: "*.snap", // we manage all files ending with ".snap"
	}
}

func (s *JanitorSuite) TestVerifiesExpectedFiles(c *C) {
	name := path.Join(s.dir, "expected.snap")
	err := ioutil.WriteFile(name, []byte(`expected`), 0600)
	c.Assert(err, IsNil)
	created, corrected, removed, err := s.j.Tidy(map[string]*osutil.File{
		"expected.snap": {Content: []byte(`expected`), Mode: 0600, UID: s.uid, Gid: s.gid},
	})
	c.Assert(err, IsNil)
	// Report says that nothing has changed
	c.Assert(removed, HasLen, 0)
	c.Assert(created, HasLen, 0)
	c.Assert(corrected, HasLen, 0)
	// The content is correct
	content, err := ioutil.ReadFile(path.Join(s.dir, "expected.snap"))
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, []byte(`expected`))
	// The permissions are correct
	stat, err := os.Stat(name)
	c.Assert(err, IsNil)
	c.Assert(stat.Mode().Perm(), Equals, os.FileMode(0600))
}

func (s *JanitorSuite) TestCreatesMissingFiles(c *C) {
	created, corrected, removed, err := s.j.Tidy(map[string]*osutil.File{
		"missing.snap": {Content: []byte(`content`), Mode: 0600, UID: s.uid, Gid: s.gid},
	})
	c.Assert(err, IsNil)
	// Created file is reported
	c.Assert(removed, HasLen, 0)
	c.Assert(created, DeepEquals, []string{"missing.snap"})
	c.Assert(corrected, HasLen, 0)
	// The content is correct
	content, err := ioutil.ReadFile(path.Join(s.dir, "missing.snap"))
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, []byte(`content`))
	// The permissions are correct
	name := path.Join(s.dir, "missing.snap")
	stat, err := os.Stat(name)
	c.Assert(err, IsNil)
	c.Assert(stat.Mode().Perm(), Equals, os.FileMode(0600))
}

func (s *JanitorSuite) TestRemovesUnexpectedFiless(c *C) {
	name := path.Join(s.dir, "evil.snap")
	err := ioutil.WriteFile(name, []byte(`evil text`), 0600)
	c.Assert(err, IsNil)
	created, corrected, removed, err := s.j.Tidy(map[string]*osutil.File{})
	c.Assert(err, IsNil)
	// Removed file is reported
	c.Assert(removed, DeepEquals, []string{"evil.snap"})
	c.Assert(created, HasLen, 0)
	c.Assert(corrected, HasLen, 0)
	// The file is removed
	_, err = os.Stat(name)
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *JanitorSuite) TestIgnoresUnrelatedFiles(c *C) {
	name := path.Join(s.dir, "unrelated")
	err := ioutil.WriteFile(name, []byte(`text`), 0600)
	c.Assert(err, IsNil)
	created, corrected, removed, err := s.j.Tidy(map[string]*osutil.File{})
	c.Assert(err, IsNil)
	// Report says that nothing has changed
	c.Assert(removed, HasLen, 0)
	c.Assert(created, HasLen, 0)
	c.Assert(corrected, HasLen, 0)
	// The file is still there
	_, err = os.Stat(name)
	c.Assert(err, IsNil)
}

func (s *JanitorSuite) TestCorrectsCorruptedFilesWithDifferentSize(c *C) {
	name := path.Join(s.dir, "corrupted.snap")
	err := ioutil.WriteFile(name, []byte(``), 0600)
	c.Assert(err, IsNil)
	created, corrected, removed, err := s.j.Tidy(map[string]*osutil.File{
		"corrupted.snap": {Content: []byte(`Hello World`), Mode: 0600, UID: s.uid, Gid: s.gid},
	})
	c.Assert(err, IsNil)
	// corrected file is reported
	c.Assert(removed, HasLen, 0)
	c.Assert(created, HasLen, 0)
	c.Assert(corrected, DeepEquals, []string{"corrupted.snap"})
	// The content is corrected
	content, err := ioutil.ReadFile(path.Join(s.dir, "corrupted.snap"))
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, []byte(`Hello World`))
	// The permissions are what we expect
	stat, err := os.Stat(name)
	c.Assert(err, IsNil)
	c.Assert(stat.Mode().Perm(), Equals, os.FileMode(0600))
}

func (s *JanitorSuite) TestCorrectsCorruptedFilesWithSameSize(c *C) {
	name := path.Join(s.dir, "corrupted.snap")
	err := ioutil.WriteFile(name, []byte(`evil`), 0600)
	c.Assert(err, IsNil)
	created, corrected, removed, err := s.j.Tidy(map[string]*osutil.File{
		"corrupted.snap": {Content: []byte(`good`), Mode: 0600, UID: s.uid, Gid: s.gid},
	})
	c.Assert(err, IsNil)
	// corrected file is reported
	c.Assert(removed, HasLen, 0)
	c.Assert(created, HasLen, 0)
	c.Assert(corrected, DeepEquals, []string{"corrupted.snap"})
	// The content is corrected
	content, err := ioutil.ReadFile(path.Join(s.dir, "corrupted.snap"))
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, []byte(`good`))
	// The permissions are what we expect
	stat, err := os.Stat(name)
	c.Assert(err, IsNil)
	c.Assert(stat.Mode().Perm(), Equals, os.FileMode(0600))
}

func (s *JanitorSuite) TestFixesFilesWithBadPermissions(c *C) {
	name := path.Join(s.dir, "sensitive.snap")
	// NOTE: the file is wide-open for everyone
	err := ioutil.WriteFile(name, []byte(`password`), 0666)
	c.Assert(err, IsNil)
	created, corrected, removed, err := s.j.Tidy(map[string]*osutil.File{
		// NOTE: we want the file to be private
		"sensitive.snap": {Content: []byte(`password`), Mode: 0600, UID: s.uid, Gid: s.gid},
	})
	c.Assert(err, IsNil)
	// corrected file is reported
	c.Assert(removed, HasLen, 0)
	c.Assert(created, HasLen, 0)
	c.Assert(corrected, DeepEquals, []string{"sensitive.snap"})
	// The content is still the same
	content, err := ioutil.ReadFile(path.Join(s.dir, "sensitive.snap"))
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, []byte(`password`))
	// The permissions are corrected
	stat, err := os.Stat(name)
	c.Assert(err, IsNil)
	c.Assert(stat.Mode().Perm(), Equals, os.FileMode(0600))
}

func (s *JanitorSuite) TestTriesToFixFilesWithBadOwnership(c *C) {
	name := path.Join(s.dir, "root-owned.snap")
	err := ioutil.WriteFile(name, []byte(`state`), 0600)
	c.Assert(err, IsNil)
	created, corrected, removed, err := s.j.Tidy(map[string]*osutil.File{
		// NOTE: we want this file to be root-owned
		"root-owned.snap": {Content: []byte(`state`), Mode: 0600, UID: 0, Gid: 0},
	})
	// XXX: we'd like to chown the file but could not
	c.Assert(err, ErrorMatches, "chown .*: operation not permitted")
	// The file is not reported as corrected because the error happened before that
	c.Assert(removed, HasLen, 0)
	c.Assert(created, HasLen, 0)
	c.Assert(corrected, HasLen, 0)
	// The content is still the same
	content, err := ioutil.ReadFile(path.Join(s.dir, "root-owned.snap"))
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, []byte(`state`))
	// The permissions still the same
	stat, err := os.Stat(name)
	c.Assert(err, IsNil)
	c.Assert(stat.Mode().Perm(), Equals, os.FileMode(0600))
}

func (s *JanitorSuite) TestReportsAbnormalFileName(c *C) {
	created, corrected, removed, err := s.j.Tidy(map[string]*osutil.File{
		"without-namespace": {Content: nil, Mode: 0600, UID: s.uid, Gid: s.gid},
	})
	c.Assert(err.Error(), Equals, `expected files must match pattern: "without-namespace" (pattern: "*.snap")`)
	c.Assert(removed, HasLen, 0)
	c.Assert(created, HasLen, 0)
	c.Assert(corrected, HasLen, 0)
}

func (s *JanitorSuite) TestReportsAbnormalFileLocation(c *C) {
	created, corrected, removed, err := s.j.Tidy(map[string]*osutil.File{
		"subdir/file.snap": {Content: nil, Mode: 0600, UID: s.uid, Gid: s.gid},
	})
	c.Assert(err.Error(), Equals, `expected files cannot have path component: "subdir/file.snap"`)
	c.Assert(removed, HasLen, 0)
	c.Assert(created, HasLen, 0)
	c.Assert(corrected, HasLen, 0)
}

func (s *JanitorSuite) TestReportsAbnormalPatterns(c *C) {
	// NOTE: the pattern is invalid
	j := osutil.Janitor{Glob: "[", Path: s.dir}
	created, corrected, removed, err := j.Tidy(map[string]*osutil.File{"unused": {}})
	c.Assert(err, ErrorMatches, "syntax error in pattern")
	c.Assert(removed, HasLen, 0)
	c.Assert(created, HasLen, 0)
	c.Assert(corrected, HasLen, 0)
}
