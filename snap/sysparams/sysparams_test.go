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
	"io/ioutil"
	"os"
	"path"
	"testing"

	. "gopkg.in/check.v1"

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
	// the file unless Update is called. And that an empty update
	// provides empty members
	sspPath := dirs.SnapSystemParamsUnder(dirs.GlobalRootDir)
	ssp, err := sysparams.Open(sspPath)
	c.Check(err, IsNil)
	c.Assert(ssp, NotNil)
	c.Check(sspPath, testutil.FileAbsent)

	r := sysparams.MockOsutilAtomicWriteFile(func(filename string, data []byte, perm os.FileMode, flags osutil.AtomicWriteFlags) error {
		c.Check(string(data), Equals, "homedirs=\n")
		return nil
	})
	defer r()

	// Save the file
	err = ssp.Write()
	c.Check(err, IsNil)
}

func (s *sysParamsTestSuite) TestUpdateWriteFailure(c *C) {
	// Opening the file when it doesn't exist, should not create
	// the file unless Update is called. And that an empty update
	// provides empty members
	sspPath := dirs.SnapSystemParamsUnder(dirs.GlobalRootDir)
	ssp, err := sysparams.Open(sspPath)
	c.Check(err, IsNil)
	c.Assert(ssp, NotNil)
	c.Check(sspPath, testutil.FileAbsent)

	r := sysparams.MockOsutilAtomicWriteFile(func(filename string, data []byte, perm os.FileMode, flags osutil.AtomicWriteFlags) error {
		return errors.New("some write error")
	})
	defer r()

	err = ssp.Write()
	c.Assert(err, ErrorMatches, "some write error")
}

func (s *sysParamsTestSuite) TestOpenExisting(c *C) {
	sspPath := dirs.SnapSystemParamsUnder(dirs.GlobalRootDir)
	c.Assert(os.MkdirAll(path.Dir(sspPath), 0755), IsNil)
	c.Assert(ioutil.WriteFile(sspPath, []byte("homedirs=my-path/foo/bar,foo\n"), 0644), IsNil)

	ssp, err := sysparams.Open(sspPath)
	c.Check(err, IsNil)
	c.Assert(ssp, NotNil)
	c.Assert(ssp.Homedirs, Equals, "my-path/foo/bar,foo")
}

func (s *sysParamsTestSuite) TestOpenExistingEmpty(c *C) {
	sspPath := dirs.SnapSystemParamsUnder(dirs.GlobalRootDir)
	c.Assert(os.MkdirAll(path.Dir(sspPath), 0755), IsNil)
	c.Assert(ioutil.WriteFile(sspPath, []byte("\n"), 0644), IsNil)

	ssp, err := sysparams.Open(sspPath)
	c.Check(err, IsNil)
	c.Check(ssp, NotNil)
}

func (s *sysParamsTestSuite) TestOpenExistingWithInvalidContent(c *C) {
	sspPath := dirs.SnapSystemParamsUnder(dirs.GlobalRootDir)
	c.Assert(os.MkdirAll(path.Dir(sspPath), 0755), IsNil)
	c.Assert(ioutil.WriteFile(sspPath, []byte("xuifu93\n"), 0644), IsNil)

	ssp, err := sysparams.Open(sspPath)
	c.Check(err, ErrorMatches, `cannot parse invalid line: xuifu93`)
	c.Check(ssp, NotNil)
}

func (s *sysParamsTestSuite) TestOpenExistingWithComments(c *C) {
	sspPath := dirs.SnapSystemParamsUnder(dirs.GlobalRootDir)
	c.Assert(os.MkdirAll(path.Dir(sspPath), 0755), IsNil)
	c.Assert(ioutil.WriteFile(sspPath, []byte("# this is a comment line\n"), 0644), IsNil)

	ssp, err := sysparams.Open(sspPath)
	c.Check(err, IsNil)
	c.Check(ssp, NotNil)
}
