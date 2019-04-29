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
package seccomp_test

import (
	"errors"
	"fmt"
	"testing"

	. "gopkg.in/check.v1"

	seccomp "github.com/snapcore/snapd/sandbox/seccomp"
	"github.com/snapcore/snapd/testutil"
)

type compilerSuite struct{}

var _ = Suite(&compilerSuite{})

func TestSeccomp(t *testing.T) { TestingT(t) }

func fromCmd(c *C, cmd *testutil.MockCmd) func(string) (string, error) {
	return func(name string) (string, error) {
		c.Check(name, Equals, "snap-seccomp")
		return cmd.Exe(), nil
	}
}

func (s *compilerSuite) TestVersionInfoValidate(c *C) {

	for i, tc := range []struct {
		v   string
		exp string
		err string
	}{
		// valid
		{"7ac348ac9c934269214b00d1692dfa50d5d4a157 2.3.3 03e996919907bc7163bc83b95bca0ecab31300f20dfa365ea14047c698340e7c", "7ac348ac9c934269214b00d1692dfa50d5d4a157 2.3.3 03e996919907bc7163bc83b95bca0ecab31300f20dfa365ea14047c698340e7c", ""},
		{"abcdef 0.0.0 abcd", "abcdef 0.0.0 abcd", ""},

		// invalid all the way down from here

		// this is over the sane length limit
		{"b27bacf755e589437c08c46f616d8531e6918dca44e575a597d93b538825b853 2.3.3 b27bacf755e589437c08c46f616d8531e6918dca44e575a597d93b538825b853", "", "invalid version-info length: .*"},
		// incorrect format
		{"abcd 0.0.0 fg", "", "invalid format of version-info: .*"},
		{"ggg 0.0.0 abc", "", "invalid format of version-info: .*"},
		{"foo", "", "invalid format of version-info: .*"},
		{"1", "", "invalid format of version-info: .*"},
		{"i\ncan\nhave\nnewlines", "", "invalid format of version-info: .*"},
		{"# invalid", "", "invalid format of version-info: .*"},
		{"-1", "", "invalid format of version-info: .*"},
	} {
		c.Logf("tc: %v", i)
		cmd := testutil.MockCommand(c, "snap-seccomp", fmt.Sprintf("echo \"%s\"", tc.v))
		compiler, err := seccomp.New(fromCmd(c, cmd))
		c.Assert(err, IsNil)

		v, err := compiler.VersionInfo()
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
			c.Check(v, Equals, "")
		} else {
			c.Check(err, IsNil)
			c.Check(v, Equals, tc.exp)
		}
		c.Check(cmd.Calls(), DeepEquals, [][]string{
			{"snap-seccomp", "version-info"},
		})
		cmd.Restore()
	}

}

func (s *compilerSuite) TestVersionInfoUnhappy(c *C) {
	cmd := testutil.MockCommand(c, "snap-seccomp", `
if [ "$1" = "version-info" ]; then echo "unknown command version-info"; exit 1; fi
exit 0
`)
	defer cmd.Restore()
	compiler, err := seccomp.New(fromCmd(c, cmd))
	c.Assert(err, IsNil)

	_, err = compiler.VersionInfo()
	c.Assert(err, ErrorMatches, "unknown command version-info")
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"snap-seccomp", "version-info"},
	})
}

func (s *compilerSuite) TestCompileEasy(c *C) {
	cmd := testutil.MockCommand(c, "snap-seccomp", `
if [ "$1" = "compile" ]; then exit 0; fi
exit 1
`)
	defer cmd.Restore()
	compiler, err := seccomp.New(fromCmd(c, cmd))
	c.Assert(err, IsNil)

	err = compiler.Compile("foo.src", "foo.bin")
	c.Assert(err, IsNil)
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"snap-seccomp", "compile", "foo.src", "foo.bin"},
	})
}

func (s *compilerSuite) TestCompileUnhappy(c *C) {
	cmd := testutil.MockCommand(c, "snap-seccomp", `
if [ "$1" = "compile" ]; then echo "i will not"; exit 1; fi
exit 0
`)
	defer cmd.Restore()
	compiler, err := seccomp.New(fromCmd(c, cmd))
	c.Assert(err, IsNil)

	err = compiler.Compile("foo.src", "foo.bin")
	c.Assert(err, ErrorMatches, "i will not")
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"snap-seccomp", "compile", "foo.src", "foo.bin"},
	})
}

func (s *compilerSuite) TestCompilerNewUnhappy(c *C) {
	compiler, err := seccomp.New(func(name string) (string, error) { return "", errors.New("failed") })
	c.Assert(err, ErrorMatches, "failed")
	c.Assert(compiler, IsNil)

	c.Assert(func() { seccomp.New(nil) }, PanicMatches, "lookup tool func not provided")
}
