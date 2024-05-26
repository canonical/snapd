// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package sysparams_test

import (
	"errors"
	"os"
	"path"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap/sysparams"
	"github.com/snapcore/snapd/testutil"
)

func TestSysParamsTest(t *testing.T) { TestingT(t) }

type sysParamsTestSuite struct {
	testutil.BaseTest
}

var _ = Suite(&sysParamsTestSuite{})

func (s *sysParamsTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *sysParamsTestSuite) TestOpenNewEmpty(c *C) {
	// Opening the file when it doesn't exist, should not create
	// the file unless Write is called. And that an empty write
	// provides empty members
	sspPath := dirs.SnapSystemParamsUnder(dirs.GlobalRootDir)
	c.Assert(os.MkdirAll(path.Dir(sspPath), 0755), IsNil)

	ssp := mylog.Check2(sysparams.Open(""))
	c.Check(err, IsNil)
	c.Assert(ssp, NotNil)
	c.Check(sspPath, testutil.FileAbsent)
	mylog.

		// Save the file
		Check(ssp.Write())
	c.Check(err, IsNil)

	// Verify the contents of the file, and that the file
	// has the correct permissions
	c.Assert(sspPath, testutil.FileEquals, "homedirs=\n")

	stat := mylog.Check2(os.Stat(sspPath))

	c.Check(stat.Mode(), Equals, os.FileMode(0644))
}

func (s *sysParamsTestSuite) TestWriteFailure(c *C) {
	// Opening the file when it doesn't exist, should not create
	// the file unless Write is called. And that an empty write
	// provides empty members
	ssp := mylog.Check2(sysparams.Open(""))
	c.Check(err, IsNil)
	c.Assert(ssp, NotNil)
	c.Check(dirs.SnapSystemParamsUnder(dirs.GlobalRootDir), testutil.FileAbsent)

	r := sysparams.MockOsutilAtomicWriteFile(func(filename string, data []byte, perm os.FileMode, flags osutil.AtomicWriteFlags) error {
		return errors.New("some write error")
	})
	defer r()
	mylog.Check(ssp.Write())
	c.Assert(err, ErrorMatches, "cannot write system-params: some write error")
}

func (s *sysParamsTestSuite) TestOpenExisting(c *C) {
	sspPath := dirs.SnapSystemParamsUnder(dirs.GlobalRootDir)
	c.Assert(os.MkdirAll(path.Dir(sspPath), 0755), IsNil)
	c.Assert(os.WriteFile(sspPath, []byte("homedirs=my-path/foo/bar,foo\n"), 0644), IsNil)

	ssp := mylog.Check2(sysparams.Open(""))
	c.Check(err, IsNil)
	c.Assert(ssp, NotNil)
	c.Assert(ssp.Homedirs, Equals, "my-path/foo/bar,foo")
}

func (s *sysParamsTestSuite) TestOpenExistingEmpty(c *C) {
	sspPath := dirs.SnapSystemParamsUnder(dirs.GlobalRootDir)
	c.Assert(os.MkdirAll(path.Dir(sspPath), 0755), IsNil)
	c.Assert(os.WriteFile(sspPath, []byte("\n"), 0644), IsNil)

	ssp := mylog.Check2(sysparams.Open(""))
	c.Check(err, IsNil)
	c.Check(ssp.Homedirs, Equals, "")
}

func (s *sysParamsTestSuite) TestOpenExistingWithInvalidContent(c *C) {
	sspPath := dirs.SnapSystemParamsUnder(dirs.GlobalRootDir)
	c.Assert(os.MkdirAll(path.Dir(sspPath), 0755), IsNil)
	c.Assert(os.WriteFile(sspPath, []byte("xuifu93\n"), 0644), IsNil)

	ssp := mylog.Check2(sysparams.Open(""))
	c.Check(err, ErrorMatches, `cannot parse system-params: invalid line: "xuifu93"`)
	c.Check(ssp, IsNil)
}

func (s *sysParamsTestSuite) TestOpenExistingWithComments(c *C) {
	sspPath := dirs.SnapSystemParamsUnder(dirs.GlobalRootDir)
	c.Assert(os.MkdirAll(path.Dir(sspPath), 0755), IsNil)
	c.Assert(os.WriteFile(sspPath, []byte("# this is a comment line\n"), 0644), IsNil)

	ssp := mylog.Check2(sysparams.Open(""))
	c.Check(err, IsNil)
	c.Check(ssp.Homedirs, Equals, "")
}

func (s *sysParamsTestSuite) TestOpenExistingWithDoubleEqual(c *C) {
	sspPath := dirs.SnapSystemParamsUnder(dirs.GlobalRootDir)
	c.Assert(os.MkdirAll(path.Dir(sspPath), 0755), IsNil)
	c.Assert(os.WriteFile(sspPath, []byte("homedirs=my-path/foo/bar,foo=bar\n"), 0644), IsNil)

	ssp := mylog.Check2(sysparams.Open(""))
	c.Check(err, IsNil)
	c.Check(ssp.Homedirs, Equals, "my-path/foo/bar,foo=bar")
}

func (s *sysParamsTestSuite) TestOpenExistingWithDuplicateLine(c *C) {
	contents := `
homedirs=foo/bar
homedirs=foo/baz
`
	sspPath := dirs.SnapSystemParamsUnder(dirs.GlobalRootDir)
	c.Assert(os.MkdirAll(path.Dir(sspPath), 0755), IsNil)
	c.Assert(os.WriteFile(sspPath, []byte(contents), 0644), IsNil)

	ssp := mylog.Check2(sysparams.Open(""))
	c.Check(err, ErrorMatches, `cannot parse system-params: duplicate entry found: "homedirs"`)
	c.Check(ssp, IsNil)
}
