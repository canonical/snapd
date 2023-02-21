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

package osutil_test

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/osutil"
)

type kcmdlineTestSuite struct{}

var _ = Suite(&kcmdlineTestSuite{})

func (s *kcmdlineTestSuite) TestSplitKernelCommandLine(c *C) {
	for idx, tc := range []struct {
		cmd    string
		exp    []string
		errStr string
	}{
		{cmd: ``, exp: nil},
		{cmd: `foo bar baz`, exp: []string{"foo", "bar", "baz"}},
		{cmd: `foo=" many   spaces  " bar`, exp: []string{`foo=" many   spaces  "`, "bar"}},
		{cmd: `foo="1$2"`, exp: []string{`foo="1$2"`}},
		{cmd: `foo=1$2`, exp: []string{`foo=1$2`}},
		{cmd: `foo= bar`, exp: []string{"foo=", "bar"}},
		{cmd: `foo=""`, exp: []string{`foo=""`}},
		{cmd: `   cpu=1,2,3   mem=0x2000;0x4000:$2  `, exp: []string{"cpu=1,2,3", "mem=0x2000;0x4000:$2"}},
		{cmd: "isolcpus=1,2,10-20,100-2000:2/25", exp: []string{"isolcpus=1,2,10-20,100-2000:2/25"}},
		// something more realistic
		{
			cmd: `BOOT_IMAGE=/vmlinuz-linux root=/dev/mapper/linux-root rw quiet loglevel=3 rd.udev.log_priority=3 vt.global_cursor_default=0 rd.luks.uuid=1a273f76-3118-434b-8597-a3b12a59e017 rd.luks.uuid=775e4582-33c1-423b-ac19-f734e0d5e21c rd.luks.options=discard,timeout=0 root=/dev/mapper/linux-root apparmor=1 security=apparmor`,
			exp: []string{
				"BOOT_IMAGE=/vmlinuz-linux",
				"root=/dev/mapper/linux-root",
				"rw", "quiet",
				"loglevel=3",
				"rd.udev.log_priority=3",
				"vt.global_cursor_default=0",
				"rd.luks.uuid=1a273f76-3118-434b-8597-a3b12a59e017",
				"rd.luks.uuid=775e4582-33c1-423b-ac19-f734e0d5e21c",
				"rd.luks.options=discard,timeout=0",
				"root=/dev/mapper/linux-root",
				"apparmor=1",
				"security=apparmor",
			},
		},
		// this is actually ok, eg. rd.luks.options=discard,timeout=0
		{cmd: `a=b=`, exp: []string{"a=b="}},
		// bad quoting, or otherwise malformed command line
		{cmd: `foo="1$2`, errStr: "unbalanced quoting"},
		{cmd: `"foo"`, errStr: "unexpected quoting"},
		{cmd: `foo"foo"`, errStr: "unexpected quoting"},
		{cmd: `foo=foo"`, errStr: "unexpected quoting"},
		{cmd: `foo="a""b"`, errStr: "unexpected quoting"},
		{cmd: `foo="a foo="b`, errStr: "unexpected argument"},
		{cmd: `foo="a"="b"`, errStr: "unexpected assignment"},
		{cmd: `=`, errStr: "unexpected assignment"},
		{cmd: `a =`, errStr: "unexpected assignment"},
		{cmd: `="foo"`, errStr: "unexpected assignment"},
		{cmd: `a==`, errStr: "unexpected assignment"},
		{cmd: `foo ==a`, errStr: "unexpected assignment"},
	} {
		c.Logf("%v: cmd: %q", idx, tc.cmd)
		out, err := osutil.KernelCommandLineSplit(tc.cmd)
		if tc.errStr != "" {
			c.Assert(err, ErrorMatches, tc.errStr)
			c.Check(out, IsNil)
		} else {
			c.Assert(err, IsNil)
			c.Check(out, DeepEquals, tc.exp)
		}
	}
}

func (s *kcmdlineTestSuite) TestGetKernelCommandLineKeyValue(c *C) {
	for _, t := range []struct {
		cmdline string
		keys    []string
		exp     map[string]string
		err     string
		comment string
	}{
		{
			cmdline: "",
			comment: "empty cmdline",
			keys:    []string{"foo"},
		},
		{
			cmdline: "foo",
			comment: "cmdline non-key-value",
			keys:    []string{"foo"},
		},
		{
			cmdline: "foo=1",
			comment: "key-value pair",
			keys:    []string{"foo"},
			exp: map[string]string{
				"foo": "1",
			},
		},
		{
			cmdline: "foo=1 otherfoo=2",
			comment: "multiple key-value pairs",
			keys:    []string{"foo", "otherfoo"},
			exp: map[string]string{
				"foo":      "1",
				"otherfoo": "2",
			},
		},
		{
			cmdline: "foo=",
			comment: "empty value in key-value pair",
			keys:    []string{"foo"},
			exp: map[string]string{
				"foo": "",
			},
		},
		{
			cmdline: "foo=1 foo=2",
			comment: "duplicated key-value pair uses last one",
			keys:    []string{"foo"},
			exp: map[string]string{
				"foo": "2",
			},
		},
		{
			cmdline: "foo=1 foo foo2=other",
			comment: "cmdline key-value pair and non-key-value",
			keys:    []string{"foo"},
			exp: map[string]string{
				"foo": "1",
			},
		},
		{
			cmdline: "foo=a=1",
			comment: "key-value pair with = in value",
			keys:    []string{"foo"},
			exp: map[string]string{
				"foo": "a=1",
			},
		},
		{
			cmdline: "=foo",
			comment: "missing key",
			keys:    []string{"foo"},
			err:     "unexpected assignment",
		},
		{
			cmdline: `"foo`,
			comment: "invalid kernel cmdline",
			keys:    []string{"foo"},
			err:     "unexpected quoting",
		},
	} {
		cmdlineFile := filepath.Join(c.MkDir(), "cmdline")
		err := ioutil.WriteFile(cmdlineFile, []byte(t.cmdline), 0644)
		c.Assert(err, IsNil)
		r := osutil.MockProcCmdline(cmdlineFile)
		defer r()
		res, err := osutil.KernelCommandLineKeyValues(t.keys...)
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err, Commentf(t.comment))
		} else {
			c.Assert(err, IsNil)
			exp := t.exp
			if t.exp == nil {
				exp = map[string]string{}
			}
			c.Assert(res, DeepEquals, exp, Commentf(t.comment))
		}
	}
}

func (s *kcmdlineTestSuite) TestKernelCommandLine(c *C) {
	d := c.MkDir()
	newProcCmdline := filepath.Join(d, "cmdline")
	restore := osutil.MockProcCmdline(newProcCmdline)
	defer restore()

	cmd, err := osutil.KernelCommandLine()
	c.Assert(err, ErrorMatches, `.*/cmdline: no such file or directory`)
	c.Check(cmd, Equals, "")

	err = ioutil.WriteFile(newProcCmdline, []byte("foo bar baz panic=-1\n"), 0644)
	c.Assert(err, IsNil)
	cmd, err = osutil.KernelCommandLine()
	c.Assert(err, IsNil)
	c.Check(cmd, Equals, "foo bar baz panic=-1")
}

// Outcome of test cases here match the next_arg function from
// lib/cmdline.c in the linux kernel (minus sometimes the linux code
// returning NULL or "" while we always return "" in both cases, but
// that should be irrelevant).
func (s *kcmdlineTestSuite) TestKernelParseCommandLine(c *C) {
	for idx, tc := range []struct {
		cmd string
		exp []osutil.KernelArgument
	}{
		{cmd: ``, exp: []osutil.KernelArgument{}},
		{cmd: `foo bar baz`, exp: []osutil.KernelArgument{
			{"foo", "", false}, {"bar", "", false}, {"baz", "", false}}},
		{cmd: `"foo"=" many   spaces  " bar`, exp: []osutil.KernelArgument{
			{`foo"`, " many   spaces  ", true}, {"bar", "", false}}},
		{cmd: `"foo=bar" foo="bar"`, exp: []osutil.KernelArgument{
			{"foo", "bar", true}, {"foo", "bar", true}}},
		{cmd: `foo=* baz=bar`, exp: []osutil.KernelArgument{
			{"foo", "*", false}, {"baz", "bar", false}}},
		{cmd: `foo_bar-tee=bar_aa-bb`, exp: []osutil.KernelArgument{
			{"foo_bar_tee", "bar_aa-bb", false}}},
		{cmd: `foo="1$2"`, exp: []osutil.KernelArgument{{"foo", "1$2", true}}},
		{cmd: `foo=1$2`, exp: []osutil.KernelArgument{{"foo", "1$2", false}}},
		{cmd: `foo= bar`, exp: []osutil.KernelArgument{{"foo", "", false}, {"bar", "", false}}},
		{cmd: `foo=""`, exp: []osutil.KernelArgument{{"foo", "", true}}},
		{cmd: `   cpu=1,2,3   mem=0x2000;0x4000:$2  `,
			exp: []osutil.KernelArgument{{"cpu", "1,2,3", false}, {"mem", "0x2000;0x4000:$2", false}}},
		{cmd: "isolcpus=1,2,10-20,100-2000:2/25",
			exp: []osutil.KernelArgument{{"isolcpus", "1,2,10-20,100-2000:2/25", false}}},
		// something more realistic
		{
			cmd: `BOOT_IMAGE=/vmlinuz-linux root=/dev/mapper/linux-root rw quiet loglevel=3 rd.udev.log_priority=3 vt.global_cursor_default=0 rd.luks.uuid=1a273f76-3118-434b-8597-a3b12a59e017 rd.luks.uuid=775e4582-33c1-423b-ac19-f734e0d5e21c rd.luks.options=discard,timeout=0 root=/dev/mapper/linux-root apparmor=1 security=apparmor`,
			exp: []osutil.KernelArgument{
				{"BOOT_IMAGE", "/vmlinuz-linux", false},
				{"root", "/dev/mapper/linux-root", false},
				{"rw", "", false},
				{"quiet", "", false},
				{"loglevel", "3", false},
				{"rd.udev.log_priority", "3", false},
				{"vt.global_cursor_default", "0", false},
				{"rd.luks.uuid", "1a273f76-3118-434b-8597-a3b12a59e017", false},
				{"rd.luks.uuid", "775e4582-33c1-423b-ac19-f734e0d5e21c", false},
				{"rd.luks.options", "discard,timeout=0", false},
				{"root", "/dev/mapper/linux-root", false},
				{"apparmor", "1", false},
				{"security", "apparmor", false},
			},
		},
		// this is actually ok, eg. rd.luks.options=discard,timeout=0
		{cmd: `a=b=`, exp: []osutil.KernelArgument{{"a", "b=", false}}},
		// bad quoting, or otherwise malformed command line
		{cmd: `foo="1$2`, exp: []osutil.KernelArgument{{"foo", "1$2", true}}},
		{cmd: `"foo"`, exp: []osutil.KernelArgument{{"foo", "", true}}},
		{cmd: `foo"foo"`, exp: []osutil.KernelArgument{{`foo"foo"`, "", false}}},
		{cmd: `foo=foo"`, exp: []osutil.KernelArgument{{"foo", `foo"`, false}}},
		{cmd: `foo=bar=baz`, exp: []osutil.KernelArgument{{"foo", `bar=baz`, false}}},
		{cmd: `"f"o"o"="b"a"r"`, exp: []osutil.KernelArgument{{`f"o"o"`, `b"a"r`, true}}},
		{cmd: `foo="a""b"`, exp: []osutil.KernelArgument{{"foo", `a""b`, true}}},
		{cmd: `foo="a foo="b`, exp: []osutil.KernelArgument{{"foo", `a foo="b`, true}}},
		{cmd: `foo="a"="b"`, exp: []osutil.KernelArgument{{"foo", `a"="b`, true}}},
		{cmd: `=`, exp: []osutil.KernelArgument{{"=", "", false}}},
		{cmd: `a =`, exp: []osutil.KernelArgument{{"a", "", false}, {"=", "", false}}},
		{cmd: `="foo"`, exp: []osutil.KernelArgument{{`="foo"`, "", false}}},
		{cmd: `a==`, exp: []osutil.KernelArgument{{"a", "=", false}}},
		{cmd: `foo ==a`, exp: []osutil.KernelArgument{{"foo", "", false}, {"=", "a", false}}},
	} {
		c.Logf("%v, cmd: %q", idx, tc.cmd)
		out := osutil.ParseKernelCommandline(tc.cmd)
		c.Check(out, DeepEquals, tc.exp)
	}
}

type argsList struct {
	Args []osutil.KernelArgument `yaml:"args"`
}

func buildYamlArgsList(list []string) string {
	yaml := "args:\n"
	for _, arg := range list {
		yaml += fmt.Sprintf("  - %s\n", arg)
	}
	return yaml
}

func (s *kcmdlineTestSuite) TestUnmarshalKernelArgument(c *C) {
	for idx, tc := range []struct {
		args   []string
		exp    argsList
		errStr string
	}{
		{
			[]string{`par1=val1`, `par2="val2"`},
			argsList{[]osutil.KernelArgument{{"par1", "val1", false}, {"par2", "val2", true}}},
			"",
		},
		{
			[]string{`par1="*"`, `par2`},
			argsList{[]osutil.KernelArgument{{"par1", "*", true}, {"par2", "", false}}},
			"",
		},
		{
			[]string{`par1=*`, `par2`},
			argsList{[]osutil.KernelArgument{{"par1", "*", false}, {"par2", "", false}}},
			"",
		},
		{
			[]string{`par1=val`, `par2=3[a-b]`, `par3=val`},
			argsList{},
			`\"3\[a-b\]\" contains globbing characters and is not quoted`,
		},
		{
			[]string{`par=ab*`},
			argsList{},
			`\"ab\*\" contains globbing characters and is not quoted`,
		},
		{
			[]string{`par=ab?`},
			argsList{},
			`\"ab\?\" contains globbing characters and is not quoted`,
		},
		{
			[]string{`par=\a`},
			argsList{},
			`\"\\\\a\" contains globbing characters and is not quoted`,
		},
		{
			[]string{`par="ab?g*[s-d]\q"`},
			argsList{[]osutil.KernelArgument{{"par", `ab?g*[s-d]\q`, true}}},
			"",
		},
	} {
		c.Logf("%v, args: %v", idx, tc.args)
		var args argsList
		err := yaml.Unmarshal([]byte(buildYamlArgsList(tc.args)), &args)
		if tc.errStr == "" {
			c.Check(err, IsNil)
			c.Check(args, DeepEquals, tc.exp)
		} else {
			c.Check(err, ErrorMatches, tc.errStr)
		}
	}
}
