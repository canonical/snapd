// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package classic

import (
	"fmt"
	. "gopkg.in/check.v1"
	"io/ioutil"
	"path/filepath"

	"github.com/snapcore/snapd/testutil"
)

type HelpersTestSuite struct {
	testutil.BaseTest
}

var _ = Suite(&HelpersTestSuite{})

func (t *HelpersTestSuite) makeMockMountpointCmd(c *C, cmd string) {
	mp := filepath.Join(c.MkDir(), "mountpoint")
	content := fmt.Sprintf("#!/bin/sh\n%s", cmd)
	err := ioutil.WriteFile(mp, []byte(content), 0755)
	c.Assert(err, IsNil)

	origMountpointCmd := mountpointCmd
	t.AddCleanup(func() { mountpointCmd = origMountpointCmd })
	mountpointCmd = mp
}

func (t *HelpersTestSuite) SetUpTest(c *C) {
	t.BaseTest.SetUpTest(c)
}

func (t *HelpersTestSuite) TestIsMounted(c *C) {
	t.makeMockMountpointCmd(c, "echo '/ is not a mountpoint'; false")

	mounted, err := isMounted("/")
	c.Assert(err, IsNil)
	c.Assert(mounted, Equals, false)
}

func (t *HelpersTestSuite) TestIsNotMounted(c *C) {
	t.makeMockMountpointCmd(c, "echo '/ is a mountpoint'; true")

	mounted, err := isMounted("/")
	c.Assert(err, IsNil)
	c.Assert(mounted, Equals, true)
}

func (t *HelpersTestSuite) TestMountpointReturnsUnexpectedString(c *C) {
	t.makeMockMountpointCmd(c, "echo 'random message'; false")

	_, err := isMounted("/")
	c.Assert(err, ErrorMatches, "(?m)unexpected output from mountpoint: random message")
}

func (t *HelpersTestSuite) TestIsMountedSignaled(c *C) {

	t.makeMockMountpointCmd(c, "echo ctrl-c;kill -INT $$")

	_, err := isMounted("/")
	c.Assert(err, ErrorMatches, "(?m)got unexpected exit code.*: ctrl-c$")
}

func (t *HelpersTestSuite) TestIsMountedNoBinary(c *C) {
	// we just do this for the cleanup of mountpointCmd we get for free ;)
	t.makeMockMountpointCmd(c, "")
	mountpointCmd = "no/such/file"

	_, err := isMounted("/")
	c.Assert(err, ErrorMatches, "fork/exec .*: no such file or directory")
}
