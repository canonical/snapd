// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package disks_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil/disks"
)

func Test(t *testing.T) { TestingT(t) }

type diskLabelSuite struct{}

var _ = Suite(&diskLabelSuite{})

func (ts *diskLabelSuite) TestBlkIDEncodeDecodeLabelHappy(c *C) {
	// Test output obtained with the following program:
	//
	// #include <string.h>
	// #include <stdio.h>
	// #include <blkid/blkid.h>
	// int main(int argc, char *argv[]) {
	//   char out[2048] = {0};
	//   if (blkid_encode_string(argv[1], out, sizeof(out)) != 0) {
	//     fprintf(stderr, "failed to encode string\n");
	//     return 1;
	//   }
	//   fprintf(stdout, out);
	//   return 0;
	// }

	tt := []struct {
		in  string
		out string
	}{
		// no changes
		{"foo", "foo"},
		{"plain", "plain"},
		{"plain-ol-data", "plain-ol-data"},
		{"foo:#.@bar", `foo:#.@bar`},
		{"foo..bar", `foo..bar`},
		{"3005", "3005"},
		{"#1-the_BEST@colons:+easter.eggs=something", "#1-the_BEST@colons:+easter.eggs=something"},
		{"", ""},
		{"befs_test", "befs_test"},
		{"P01_S16A", "P01_S16A"},

		// these are single length utf-8 runes, so they are not encoded
		{"héllo", "héllo"},
		{"he🐧lo", "he🐧lo"},
		{"Новый_том", "Новый_том"},

		// these are "unsafe" chars, so they get encoded
		{"ubuntu data", `ubuntu\x20data`},
		{"ubuntu\ttab", `ubuntu\x09tab`},
		{"ubuntu\t9tab", `ubuntu\x099tab`},
		{"ubuntu\nnewline", `ubuntu\x0anewline`},
		{"foo bar", `foo\x20bar`},
		{"foo/bar", `foo\x2fbar`},
		{"foo/../bar", `foo\x2f..\x2fbar`},
		{"foo\\bar", `foo\x5cbar`},
		{"pinkié pie", `pinkié\x20pie`},
		{"(EFI Boot)", `\x28EFI\x20Boot\x29`},
		{"[System Boot]", `\x5bSystem\x20Boot\x5d`},
		// 0x7e is just a 1-rune long character that is not in the allowed set
		// to demonstrate that these two input strings are encoded/decoded
		// properly with the constant double width
		{"ubuntu\x7etab", `ubuntu\x7etab`},
		{"ubuntu\x07" + "etab", `ubuntu\x07etab`},
		// works when the only character is an escaped one too
		{"\t", `\x09`},
	}
	for _, t := range tt {
		c.Logf("tc: %v %q", t.in, t.out)
		c.Assert(disks.BlkIDEncodeLabel(t.in), Equals, t.out)

		// make sure the other way around works too
		expin := mylog.Check2(disks.BlkIDDecodeLabel(t.out))


		c.Assert(expin, Equals, t.in)
	}
}

func (ts *diskLabelSuite) TestBlkIDDecodeLabelUnhappy(c *C) {
	tt := []struct {
		in     string
		experr string
	}{
		{
			`\x7z`,
			`string is malformed, unparsable escape sequence at "\\x7z"`,
		},
		{
			`\x09\x7y`,
			`string is malformed, unparsable escape sequence at "\\x7y"`,
		},
		{
			`\z`,
			`string is malformed, unparsable escape sequence at "\\z"`,
		},
		{
			`\`,
			`string is malformed, unparsable escape sequence at "\\"`,
		},
		{
			`\x40\`,
			`string is malformed, unparsable escape sequence at "\\"`,
		},
		{
			`\x`,
			`string is malformed, unparsable escape sequence at "\\x"`,
		},
		{
			`\x40\x`,
			`string is malformed, unparsable escape sequence at "\\x"`,
		},
		{
			`\x0`,
			`string is malformed, unparsable escape sequence at "\\x0"`,
		},
		{
			`\x40\x4`,
			`string is malformed, unparsable escape sequence at "\\x4"`,
		},
	}

	for _, t := range tt {
		c.Logf("input: %q", t.in)
		_ := mylog.Check2(disks.BlkIDDecodeLabel(t.in))
		c.Assert(err, ErrorMatches, t.experr)
	}
}
