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

package strutil_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
)

type cmdlineTestSuite struct{}

var _ = Suite(&cmdlineTestSuite{})

func (s *cmdlineTestSuite) TestSplitKernelCommandLine(c *C) {
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
		out, err := strutil.KernelCommandLineSplit(tc.cmd)
		if tc.errStr != "" {
			c.Assert(err, ErrorMatches, tc.errStr)
			c.Check(out, IsNil)
		} else {
			c.Assert(err, IsNil)
			c.Check(out, DeepEquals, tc.exp)
		}
	}
}
