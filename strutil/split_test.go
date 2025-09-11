// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"github.com/snapcore/snapd/strutil"

	. "gopkg.in/check.v1"
)

type splitSuite struct{}

var _ = Suite(&splitSuite{})

func (*splitSuite) TestRSplitOK(c *C) {
	tests := []struct {
		s    string
		sep  string
		n    int
		want []string
	}{
		{
			s:    "a.b.c.d.e",
			sep:  ".",
			n:    2,
			want: []string{"a.b.c.d", "e"},
		},
		{
			s:    "a.b.c.d.e",
			sep:  ".",
			n:    3,
			want: []string{"a.b.c", "d", "e"},
		},
		{
			s:    "a.b.c.d.e",
			sep:  ".",
			n:    10,
			want: []string{"a", "b", "c", "d", "e"},
		},
		{
			s:    "a.b.c.d.e",
			sep:  ".",
			n:    -2,
			want: []string{"a", "b", "c", "d", "e"},
		},
		{
			s:    "a.b.c.d.e",
			sep:  ".",
			n:    1,
			want: []string{"a.b.c.d.e"},
		},
		{
			s:    "a.b.c.d.e",
			sep:  "/",
			n:    2,
			want: []string{"a.b.c.d.e"},
		},
		{
			s:    "a.b.c.d.e",
			sep:  ".",
			n:    0,
			want: nil,
		},
	}

	for _, tt := range tests {
		got := strutil.RSplitN(tt.s, tt.sep, tt.n)
		c.Check(got, DeepEquals, tt.want)
	}
}
