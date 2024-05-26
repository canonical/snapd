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

	"github.com/ddkwork/golibrary/mylog"
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
		// all valid
		// 20-byte sha1 build ID added by GNU ld
		{"7ac348ac9c934269214b00d1692dfa50d5d4a157 2.3.3 03e996919907bc7163bc83b95bca0ecab31300f20dfa365ea14047c698340e7c bpf-actlog", "7ac348ac9c934269214b00d1692dfa50d5d4a157 2.3.3 03e996919907bc7163bc83b95bca0ecab31300f20dfa365ea14047c698340e7c bpf-actlog", ""},
		{"7ac348ac9c934269214b00d1692dfa50d5d4a157 2.3.3 03e996919907bc7163bc83b95bca0ecab31300f20dfa365ea14047c698340e7c foo:bar", "7ac348ac9c934269214b00d1692dfa50d5d4a157 2.3.3 03e996919907bc7163bc83b95bca0ecab31300f20dfa365ea14047c698340e7c foo:bar", ""},
		{"7ac348ac9c934269214b00d1692dfa50d5d4a157 2.3.3 03e996919907bc7163bc83b95bca0ecab31300f20dfa365ea14047c698340e7c -", "7ac348ac9c934269214b00d1692dfa50d5d4a157 2.3.3 03e996919907bc7163bc83b95bca0ecab31300f20dfa365ea14047c698340e7c -", ""},
		// 16-byte md5/uuid build ID added by GNU ld
		{"3817b197e7abe71a952c1245e8bdf8d9 2.3.3 03e996919907bc7163bc83b95bca0ecab31300f20dfa365ea14047c698340e7c -", "3817b197e7abe71a952c1245e8bdf8d9 2.3.3 03e996919907bc7163bc83b95bca0ecab31300f20dfa365ea14047c698340e7c -", ""},
		// 83-byte Go build ID
		{"4e444571495f482d30796b5f57307065544e47692f594c61795f384b7a5258362d6a6f4272736e38302f773374475869496e433176527749797a457a4b532f3967324d4f76556f3130323644572d56326e6248 2.3.3 03e996919907bc7163bc83b95bca0ecab31300f20dfa365ea14047c698340e7c -", "4e444571495f482d30796b5f57307065544e47692f594c61795f384b7a5258362d6a6f4272736e38302f773374475869496e433176527749797a457a4b532f3967324d4f76556f3130323644572d56326e6248 2.3.3 03e996919907bc7163bc83b95bca0ecab31300f20dfa365ea14047c698340e7c -", ""},
		// validity
		{"abcdef 0.0.0 abcd bpf-actlog", "abcdef 0.0.0 abcd bpf-actlog", ""},
		{"abcdef 0.0.0 abcd -", "abcdef 0.0.0 abcd -", ""},

		// invalid all the way down from here
		// this is over/under the sane length limit for the fields
		{"00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001 2.4.1 0000000000000000000000000000000000000000000000000000000000000000 -", "", "invalid format of version-info: .*"},
		{"0000000000000000000000000000000000000000 123456.0.0 0000000000000000000000000000000000000000000000000000000000000000 -", "", "invalid format of version-info: .*"},
		{"0000000000000000000000000000000000000000 0.123456.0 0000000000000000000000000000000000000000000000000000000000000000 -", "", "invalid format of version-info: .*"},
		{"0000000000000000000000000000000000000000 0.0.123456 0000000000000000000000000000000000000000000000000000000000000000 -", "", "invalid format of version-info: .*"},
		{"0000000000000000000000000000000000000000 2.4.1 00000000000000000000000000000000000000000000000000000000000000001 -", "", "invalid format of version-info: .*"},
		{"0000000000000000000000000000000000000000 2.4.1 0000000000000000000000000000000000000000000000000000000000000000 012345678901234567890123456789a", "", "invalid format of version-info: .*"},
		{"0000000000000000000000000000000000000000 .4.1 0000000000000000000000000000000000000000000000000000000000000000 -", "", "invalid format of version-info: .*"},
		{"0000000000000000000000000000000000000000 2.4 0000000000000000000000000000000000000000000000000000000000000000 -", "", "invalid format of version-info: .*"},
		{"0000000000000000000000000000000000000000 2.4. 0000000000000000000000000000000000000000000000000000000000000000 -", "", "invalid format of version-info: .*"},
		{"0000000000000000000000000000000000000000 2..1 0000000000000000000000000000000000000000000000000000000000000000 -", "", "invalid format of version-info: .*"},
		{"0000000000000000000000000000000000000000 2.4.1 0000000000000000000000000000000000000000000000000000000000000000 ", "", "invalid format of version-info: .*"},
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
		compiler := mylog.Check2(seccomp.NewCompiler(fromCmd(c, cmd)))


		v := mylog.Check2(compiler.VersionInfo())
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
			c.Check(v, Equals, seccomp.VersionInfo(""))
		} else {
			c.Check(err, IsNil)
			c.Check(v, Equals, seccomp.VersionInfo(tc.exp))
			_ := mylog.Check2(seccomp.VersionInfo(v).LibseccompVersion())
			c.Check(err, IsNil)
			_ = mylog.Check2(seccomp.VersionInfo(v).Features())
			c.Check(err, IsNil)
		}
		c.Check(cmd.Calls(), DeepEquals, [][]string{
			{"snap-seccomp", "version-info"},
		})
		cmd.Restore()
	}
}

func (s *compilerSuite) TestCompilerVersionInfo(c *C) {
	const vi = "7ac348ac9c934269214b00d1692dfa50d5d4a157 2.3.3 03e996919907bc7163bc83b95bca0ecab31300f20dfa365ea14047c698340e7c bpf-actlog"
	cmd := testutil.MockCommand(c, "snap-seccomp", fmt.Sprintf(`echo "%s"`, vi))

	vi1 := mylog.Check2(seccomp.CompilerVersionInfo(fromCmd(c, cmd)))
	c.Check(err, IsNil)
	c.Check(vi1, Equals, seccomp.VersionInfo(vi))
}

func (s *compilerSuite) TestEmptyVersionInfo(c *C) {
	vi := seccomp.VersionInfo("")

	_ := mylog.Check2(vi.LibseccompVersion())
	c.Check(err, ErrorMatches, "empty version-info")

	_ = mylog.Check2(vi.Features())
	c.Check(err, ErrorMatches, "empty version-info")
}

func (s *compilerSuite) TestVersionInfoUnhappy(c *C) {
	cmd := testutil.MockCommand(c, "snap-seccomp", `
if [ "$1" = "version-info" ]; then echo "unknown command version-info"; exit 1; fi
exit 0
`)
	defer cmd.Restore()
	compiler := mylog.Check2(seccomp.NewCompiler(fromCmd(c, cmd)))


	_ = mylog.Check2(compiler.VersionInfo())
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
	compiler := mylog.Check2(seccomp.NewCompiler(fromCmd(c, cmd)))

	mylog.Check(compiler.Compile("foo.src", "foo.bin"))

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
	compiler := mylog.Check2(seccomp.NewCompiler(fromCmd(c, cmd)))

	mylog.Check(compiler.Compile("foo.src", "foo.bin"))
	c.Assert(err, ErrorMatches, "i will not")
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"snap-seccomp", "compile", "foo.src", "foo.bin"},
	})
}

func (s *compilerSuite) TestCompilerNewUnhappy(c *C) {
	compiler := mylog.Check2(seccomp.NewCompiler(func(name string) (string, error) { return "", errors.New("failed") }))
	c.Assert(err, ErrorMatches, "failed")
	c.Assert(compiler, IsNil)

	c.Assert(func() { seccomp.NewCompiler(nil) }, PanicMatches, "lookup tool func not provided")
}

func (s *compilerSuite) TestLibseccompVersion(c *C) {
	v := mylog.Check2(seccomp.VersionInfo("a 2.4.1 b -").LibseccompVersion())

	c.Check(v, Equals, "2.4.1")

	v = mylog.Check2(seccomp.VersionInfo("a phooey b -").LibseccompVersion())
	c.Assert(err, ErrorMatches, "invalid format of version-info: .*")
	c.Check(v, Equals, "")
}

func (s *compilerSuite) TestGetGoSeccompFeatures(c *C) {
	for _, tc := range []struct {
		v   string
		exp string
		err string
	}{
		// valid
		{"a 2.4.1 b -", "-", ""},
		{"a 2.4.1 b foo", "foo", ""},
		{"a 2.4.1 b foo:bar", "foo:bar", ""},
		// invalid
		{"a 2.4.1 b b@rf", "", "invalid format of version-info: .*"},
	} {
		v := mylog.Check2(seccomp.VersionInfo(tc.v).Features())
		if err == nil {

			c.Check(v, Equals, tc.exp)
		} else {
			c.Assert(err, ErrorMatches, "invalid format of version-info: .*")
			c.Check(v, Equals, tc.exp)
		}
	}
}

func (s *compilerSuite) TestHasFeature(c *C) {
	for _, tc := range []struct {
		v   string
		f   string
		exp bool
		err string
	}{
		// valid negative
		{"a 2.4.1 b -", "foo", false, ""},
		{"a 2.4.1 b foo:bar", "foo:bar", false, ""},
		// valid affirmative
		{"a 2.4.1 b foo", "foo", true, ""},
		{"a 2.4.1 b foo:bar", "foo", true, ""},
		{"a 2.4.1 b foo:bar", "bar", true, ""},
		// invalid
		{"a 1.2.3 b b@rf", "b@rf", false, "invalid format of version-info: .*"},
	} {
		v := mylog.Check2(seccomp.VersionInfo(tc.v).HasFeature(tc.f))
		if err == nil {

			c.Check(v, Equals, tc.exp)
		} else {
			c.Assert(err, ErrorMatches, "invalid format of version-info: .*")
			c.Check(v, Equals, tc.exp)
		}
	}
}

func (s *compilerSuite) TestSupportsRobustArgumentFiltering(c *C) {
	for _, tc := range []struct {
		v   string
		err string
	}{
		// libseccomp < 2.3.3 and golang-seccomp < 0.9.1
		{"a 2.3.3 b -", "robust argument filtering requires a snapd built against libseccomp >= 2.4, golang-seccomp >= 0.9.1"},
		// libseccomp < 2.3.3
		{"a 2.3.3 b bpf-actlog", "robust argument filtering requires a snapd built against libseccomp >= 2.4"},
		// golang-seccomp < 0.9.1
		{"a 2.4.1 b -", "robust argument filtering requires a snapd built against golang-seccomp >= 0.9.1"},
		{"a 2.4.1 b bpf-other", "robust argument filtering requires a snapd built against golang-seccomp >= 0.9.1"},
		// libseccomp >= 2.4.1 and golang-seccomp >= 0.9.1
		{"a 2.4.1 b bpf-actlog", ""},
		{"a 3.0.0 b bpf-actlog", ""},
		// invalid
		{"a 1.2.3 b b@rf", "invalid format of version-info: .*"},
	} {
		mylog.Check(seccomp.VersionInfo(tc.v).SupportsRobustArgumentFiltering())
		if tc.err == "" {

		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}
	}
}

func (s *compilerSuite) TestCompilerStdoutOnly(c *C) {
	const vi = "7ac348ac9c934269214b00d1692dfa50d5d4a157 2.3.3 03e996919907bc7163bc83b95bca0ecab31300f20dfa365ea14047c698340e7c bpf-actlog"
	cmd := testutil.MockCommand(c, "snap-seccomp", fmt.Sprintf(`
echo "this goes to stderr" >&2
# this goes to stdout
echo "%s"
`, vi))

	vi1 := mylog.Check2(seccomp.CompilerVersionInfo(fromCmd(c, cmd)))
	c.Check(err, IsNil)
	c.Check(vi1, Equals, seccomp.VersionInfo(vi))
}

func (s *compilerSuite) TestCompilerStderrErr(c *C) {
	cmd := testutil.MockCommand(c, "snap-seccomp", `
echo "this goes to stderr" >&2
# this goes to stdout
echo "this goes to stdout"
exit 1`)

	_ := mylog.Check2(seccomp.CompilerVersionInfo(fromCmd(c, cmd)))
	c.Assert(err, ErrorMatches, "this goes to stderr")
}
