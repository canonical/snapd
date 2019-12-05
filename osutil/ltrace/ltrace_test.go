// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package ltrace_test

import (
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/ltrace"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type ltraceSuite struct {
	rootdir    string
	mockSudo   *testutil.MockCmd
	mockLtrace *testutil.MockCmd
}

var _ = Suite(&ltraceSuite{})

func (s *ltraceSuite) SetUpTest(c *C) {
	s.rootdir = c.MkDir()
	dirs.SetRootDir(s.rootdir)

	s.mockSudo = testutil.MockCommand(c, "sudo", "")
	s.mockLtrace = testutil.MockCommand(c, "ltrace", "")
}

func (s *ltraceSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
	s.mockSudo.Restore()
	s.mockLtrace.Restore()
}

func (s *ltraceSuite) TestLtraceCommandHappy(c *C) {
	u, err := user.Current()
	c.Assert(err, IsNil)

	cmd, err := ltrace.Command(nil, "foo")
	c.Assert(err, IsNil)
	c.Assert(cmd.Path, Equals, s.mockSudo.Exe())
	c.Assert(cmd.Args, DeepEquals, []string{
		s.mockSudo.Exe(), "-E",
		s.mockLtrace.Exe(), "-u", u.Username, "-f",
		// the command
		"foo",
	})
}

func (s *ltraceSuite) TestLtraceCommandNoSudo(c *C) {
	origPath := os.Getenv("PATH")
	defer func() { os.Setenv("PATH", origPath) }()

	os.Setenv("PATH", "/not-exists")
	_, err := ltrace.Command(nil, "foo")
	c.Assert(err, ErrorMatches, `cannot use ltrace without sudo: exec: "sudo": executable file not found in \$PATH`)
}

func (s *ltraceSuite) TestLtraceCommandNoLtrace(c *C) {
	origPath := os.Getenv("PATH")
	defer func() { os.Setenv("PATH", origPath) }()

	tmp := c.MkDir()
	os.Setenv("PATH", tmp)
	err := ioutil.WriteFile(filepath.Join(tmp, "sudo"), nil, 0755)
	c.Assert(err, IsNil)

	_, err = ltrace.Command(nil, "foo")
	c.Assert(err, ErrorMatches, `cannot find an installed ltrace`)
}
