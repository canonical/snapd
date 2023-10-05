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

type SnapSignBuildSuite struct {
	BaseSnapSuite
}

var _ = Suite(&SnapSignBuildSuite{})

func (s *SnapSignBuildSuite) TestSignBuildMandatoryFlags(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"sign-build", "foo_1_amd64.snap"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "the required flags `--developer-id' and `--snap-id' were not specified")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSignBuildSuite) TestSignBuildMissingSnap(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"sign-build", "foo_1_amd64.snap", "--developer-id", "dev-id1", "--snap-id", "snap-id-1"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot compute snap \"foo_1_amd64.snap\" digest: open foo_1_amd64.snap: no such file or directory")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSignBuildSuite) TestSignBuildMissingKey(c *C) {
	snapFilename := "foo_1_amd64.snap"
	_err := os.WriteFile(snapFilename, []byte("sample"), 0644)
	c.Assert(_err, IsNil)
	defer os.Remove(snapFilename)

	tempdir := c.MkDir()
	os.Setenv("SNAP_GNUPG_HOME", tempdir)
	defer os.Unsetenv("SNAP_GNUPG_HOME")

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"sign-build", snapFilename, "--developer-id", "dev-id1", "--snap-id", "snap-id-1"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot use \"default\" key: cannot find key pair in GPG keyring")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSignBuildSuite) TestSignBuildWorks(c *C) {
	snapFilename := "foo_1_amd64.snap"
	snapContent := []byte("sample")
	_err := os.WriteFile(snapFilename, snapContent, 0644)
	c.Assert(_err, IsNil)
	defer os.Remove(snapFilename)

	tempdir := c.MkDir()
	for _, fileName := range []string{"pubring.gpg", "secring.gpg", "trustdb.gpg"} {
		data, err := ioutil.ReadFile(filepath.Join("test-data", fileName))
		c.Assert(err, IsNil)
		err = os.WriteFile(filepath.Join(tempdir, fileName), data, 0644)
		c.Assert(err, IsNil)
	}
	os.Setenv("SNAP_GNUPG_HOME", tempdir)
	defer os.Unsetenv("SNAP_GNUPG_HOME")

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"sign-build", snapFilename, "--developer-id", "dev-id1", "--snap-id", "snap-id-1"})
	c.Assert(err, IsNil)

	assertion, err := asserts.Decode([]byte(s.Stdout()))
	c.Assert(err, IsNil)
	c.Check(assertion.Type(), Equals, asserts.SnapBuildType)
	c.Check(assertion.Revision(), Equals, 0)
	c.Check(assertion.HeaderString("authority-id"), Equals, "dev-id1")
	c.Check(assertion.HeaderString("developer-id"), Equals, "dev-id1")
	c.Check(assertion.HeaderString("grade"), Equals, "stable")
	c.Check(assertion.HeaderString("snap-id"), Equals, "snap-id-1")
	c.Check(assertion.HeaderString("snap-size"), Equals, fmt.Sprintf("%d", len(snapContent)))
	c.Check(assertion.HeaderString("snap-sha3-384"), Equals, "jyP7dUgb8HiRNd1SdYPp_il-YNrl6P6PgNAe-j6_7WytjKslENhMD3Of5XBU5bQK")

	// check for valid signature ?!
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSignBuildSuite) TestSignBuildWorksDevelGrade(c *C) {
	snapFilename := "foo_1_amd64.snap"
	snapContent := []byte("sample")
	_err := os.WriteFile(snapFilename, snapContent, 0644)
	c.Assert(_err, IsNil)
	defer os.Remove(snapFilename)

	tempdir := c.MkDir()
	for _, fileName := range []string{"pubring.gpg", "secring.gpg", "trustdb.gpg"} {
		data, err := ioutil.ReadFile(filepath.Join("test-data", fileName))
		c.Assert(err, IsNil)
		err = os.WriteFile(filepath.Join(tempdir, fileName), data, 0644)
		c.Assert(err, IsNil)
	}
	os.Setenv("SNAP_GNUPG_HOME", tempdir)
	defer os.Unsetenv("SNAP_GNUPG_HOME")

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"sign-build", snapFilename, "--developer-id", "dev-id1", "--snap-id", "snap-id-1", "--grade", "devel"})
	c.Assert(err, IsNil)
	assertion, err := asserts.Decode([]byte(s.Stdout()))
	c.Assert(err, IsNil)
	c.Check(assertion.Type(), Equals, asserts.SnapBuildType)
	c.Check(assertion.HeaderString("grade"), Equals, "devel")

	// check for valid signature ?!
	c.Check(s.Stderr(), Equals, "")
}
