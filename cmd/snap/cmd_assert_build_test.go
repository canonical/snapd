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

package main_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	snap "github.com/snapcore/snapd/cmd/snap"
)

type SnapAssertBuildSuite struct {
	BaseSnapSuite
}

var _ = Suite(&SnapAssertBuildSuite{})

func (s *SnapAssertBuildSuite) TestAssertBuildMandatoryFlags(c *C) {
	_, err := snap.Parser().ParseArgs([]string{"assert-build", "foo_1_amd64.snap"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "the required flags `--developer-id' and `--snap-id' were not specified")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapAssertBuildSuite) TestAssertBuildMissingSnap(c *C) {
	_, err := snap.Parser().ParseArgs([]string{"assert-build", "foo_1_amd64.snap", "--developer-id", "dev-id1", "--snap-id", "snap-id-1"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot open snap: open foo_1_amd64.snap: no such file or directory")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapAssertBuildSuite) TestAssertBuildMissingKey(c *C) {
	snap_filename := "foo_1_amd64.snap"
	_err := ioutil.WriteFile(snap_filename, []byte("sample"), 0644)
	c.Assert(_err, IsNil)
	defer os.Remove(snap_filename)

	_, err := snap.Parser().ParseArgs([]string{"assert-build", snap_filename, "--developer-id", "dev-id1", "--snap-id", "snap-id-1"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot get key by name: cannot find key named \"default\" in GPG keyring")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapAssertBuildSuite) TestAssertBuildWorks(c *C) {
	snap_filename := "foo_1_amd64.snap"
	snap_content := []byte("sample")
	_err := ioutil.WriteFile(snap_filename, snap_content, 0644)
	c.Assert(_err, IsNil)
	defer os.Remove(snap_filename)

	tempdir := c.MkDir()
	for _, fileName := range []string{"pubring.gpg", "secring.gpg", "trustdb.gpg"} {
		data, err := ioutil.ReadFile(filepath.Join("test-data", fileName))
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(filepath.Join(tempdir, fileName), data, 0644)
		c.Assert(err, IsNil)
	}
	os.Setenv("SNAP_GNUPG_HOME", tempdir)
	defer os.Unsetenv("SNAP_GNUPG_HOME")

	_, err := snap.Parser().ParseArgs([]string{"assert-build", snap_filename, "--developer-id", "dev-id1", "--snap-id", "snap-id-1"})
	c.Assert(err, IsNil)

	build_filename := snap_filename + ".build"
	data, err := ioutil.ReadFile(build_filename)
	c.Assert(err, IsNil)
	assertion, err := asserts.Decode(data)
	c.Assert(err, IsNil)
	c.Check(assertion.Type(), Equals, asserts.SnapBuildType)
	c.Check(assertion.Revision(), Equals, 0)
	c.Check(assertion.HeaderString("authority-id"), Equals, "dev-id1")
	c.Check(assertion.HeaderString("developer-id"), Equals, "dev-id1")
	c.Check(assertion.HeaderString("grade"), Equals, "stable")
	c.Check(assertion.HeaderString("snap-id"), Equals, "snap-id-1")
	c.Check(assertion.HeaderString("snap-size"), Equals, fmt.Sprintf("%d", len(snap_content)))
	c.Check(assertion.HeaderString("snap-sha3-384"), Equals, "jyP7dUgb8HiRNd1SdYPp_il-YNrl6P6PgNAe-j6_7WytjKslENhMD3Of5XBU5bQK")

	// check for valid signature ?!

	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")

	defer os.Remove(build_filename)
}
