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

	"github.com/snapcore/snapd/osutil/disks"
	"gopkg.in/check.v1"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type diskLabelSuite struct{}

var _ = Suite(&diskLabelSuite{})

func (ts *diskLabelSuite) TestEncodeHexBlkIDFormat(c *C) {
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
		{"ubuntu\ttab", `ubuntu\x9tab`},
		{"ubuntu\nnewline", `ubuntu\xanewline`},
		{"foo bar", `foo\x20bar`},
		{"foo/bar", `foo\x2fbar`},
		{"foo/../bar", `foo\x2f..\x2fbar`},
		{"foo\\bar", `foo\x5cbar`},
		{"pinkié pie", `pinkié\x20pie`},
		{"(EFI Boot)", `\x28EFI\x20Boot\x29`},
		{"[System Boot]", `\x5bSystem\x20Boot\x5d`},
	}
	for _, t := range tt {
		c.Logf("tc: %v %q", t.in, t.out)
		c.Assert(disks.BlkIDEncodeLabel(t.in), check.Equals, t.out)
	}
}
