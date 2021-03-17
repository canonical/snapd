// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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
	"math"
	"sort"
	"testing"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
)

func Test(t *testing.T) { check.TestingT(t) }

type strutilSuite struct{}

var _ = check.Suite(&strutilSuite{})

func (*strutilSuite) TestQuoted(c *check.C) {
	for _, t := range []struct {
		in  []string
		out string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"one"}, `"one"`},
		{[]string{"one", "two"}, `"one", "two"`},
		{[]string{"one", `tw"`}, `"one", "tw\""`},
	} {
		c.Check(strutil.Quoted(t.in), check.Equals, t.out, check.Commentf("expected %#v -> %s", t.in, t.out))
	}
}

func (ts *strutilSuite) TestSizeToStr(c *check.C) {
	for _, t := range []struct {
		size int64
		str  string
	}{
		{0, "0B"},
		{1, "1B"},
		{400, "400B"},
		{1000, "1kB"},
		{1000 + 1, "1kB"},
		{900 * 1000, "900kB"},
		{1000 * 1000, "1MB"},
		{20 * 1000 * 1000, "20MB"},
		{1000 * 1000 * 1000, "1GB"},
		{31 * 1000 * 1000 * 1000, "31GB"},
		{math.MaxInt64, "9EB"},
	} {
		c.Check(strutil.SizeToStr(t.size), check.Equals, t.str)
	}
}

func (ts *strutilSuite) TestListContains(c *check.C) {
	for _, xs := range [][]string{
		{},
		nil,
		{"foo"},
		{"foo", "baz", "barbar"},
	} {
		c.Check(strutil.ListContains(xs, "bar"), check.Equals, false)
		sort.Strings(xs)
		c.Check(strutil.SortedListContains(xs, "bar"), check.Equals, false)
	}

	for _, xs := range [][]string{
		{"bar"},
		{"foo", "bar", "baz"},
		{"bar", "foo", "baz"},
		{"foo", "baz", "bar"},
		{"bar", "bar", "bar", "bar", "bar", "bar"},
	} {
		c.Check(strutil.ListContains(xs, "bar"), check.Equals, true)
		sort.Strings(xs)
		c.Check(strutil.SortedListContains(xs, "bar"), check.Equals, true)
	}
}

func (ts *strutilSuite) TestTruncateOutput(c *check.C) {
	data := []byte("ab\ncd\nef\ngh\nij")
	out := strutil.TruncateOutput(data, 3, 500)
	c.Assert(out, check.DeepEquals, []byte("ef\ngh\nij"))

	out = strutil.TruncateOutput(data, 1000, 8)
	c.Assert(out, check.DeepEquals, []byte("ef\ngh\nij"))

	out = strutil.TruncateOutput(data, 1000, 1000)
	c.Assert(out, check.DeepEquals, []byte("ab\ncd\nef\ngh\nij"))

	out = strutil.TruncateOutput(data, 99, 5)
	c.Assert(out, check.DeepEquals, []byte("gh\nij"))

	out = strutil.TruncateOutput(data, 99, 6)
	c.Assert(out, check.DeepEquals, []byte("\ngh\nij"))

	out = strutil.TruncateOutput(data, 5, 1000)
	c.Assert(out, check.DeepEquals, []byte("ab\ncd\nef\ngh\nij"))

	out = strutil.TruncateOutput(data, 1000, len(data))
	c.Assert(out, check.DeepEquals, []byte("ab\ncd\nef\ngh\nij"))

	out = strutil.TruncateOutput(data, 1000, 1000)
	c.Assert(out, check.DeepEquals, []byte("ab\ncd\nef\ngh\nij"))

	out = strutil.TruncateOutput(data, 0, 0)
	c.Assert(out, check.HasLen, 0)
}

func (ts *strutilSuite) TestParseByteSizeHappy(c *check.C) {
	for _, t := range []struct {
		str      string
		expected int64
	}{
		{"0B", 0},
		{"1B", 1},
		{"400B", 400},
		{"1kB", 1000},
		// note the upper-case
		{"1KB", 1000},
		{"900kB", 900 * 1000},
		{"1MB", 1000 * 1000},
		{"20MB", 20 * 1000 * 1000},
		{"1GB", 1000 * 1000 * 1000},
		{"31GB", 31 * 1000 * 1000 * 1000},
		{"4TB", 4 * 1000 * 1000 * 1000 * 1000},
		{"6PB", 6 * 1000 * 1000 * 1000 * 1000 * 1000},
		{"8EB", 8 * 1000 * 1000 * 1000 * 1000 * 1000 * 1000},
	} {
		val, err := strutil.ParseByteSize(t.str)
		c.Check(err, check.IsNil)
		c.Check(val, check.Equals, t.expected, check.Commentf("incorrect result for input %q", t.str))
	}
}

func (ts *strutilSuite) TestParseByteSizeUnhappy(c *check.C) {
	for _, t := range []struct {
		str    string
		errStr string
	}{
		{"B", `cannot parse "B": no numerical prefix`},
		{"1", `cannot parse "1": need a number with a unit as input`},
		{"11", `cannot parse "11": need a number with a unit as input`},
		{"400x", `cannot parse "400x": try 'kB' or 'MB'`},
		{"400xx", `cannot parse "400xx": try 'kB' or 'MB'`},
		{"1k", `cannot parse "1k": try 'kB' or 'MB'`},
		{"200KiB", `cannot parse "200KiB": try 'kB' or 'MB'`},
		{"-200KB", `cannot parse "-200KB": size cannot be negative`},
		{"-200B", `cannot parse "-200B": size cannot be negative`},
		{"-B", `cannot parse "-B": "-" is not a number`},
		{"-", `cannot parse "-": "-" is not a number`},
		{"", `cannot parse "": "" is not a number`},
		// Digits outside of Latin1 range
		// ARABIC-INDIC DIGIT SEVEN
		{"٧kB", `cannot parse "٧kB": no numerical prefix`},
		{"1٧kB", `cannot parse "1٧kB": try 'kB' or 'MB'`},
	} {
		_, err := strutil.ParseByteSize(t.str)
		c.Check(err, check.ErrorMatches, t.errStr, check.Commentf("incorrect error for %q", t.str))
	}
}

func (strutilSuite) TestCommaSeparatedList(c *check.C) {
	table := []struct {
		in  string
		out []string
	}{
		{"", []string{}},
		{",", []string{}},
		{"foo,bar", []string{"foo", "bar"}},
		{"foo , bar", []string{"foo", "bar"}},
		{"foo ,, bar", []string{"foo", "bar"}},
		{" foo ,, bar,baz", []string{"foo", "bar", "baz"}},
		{" foo bar ,,,baz", []string{"foo bar", "baz"}},
	}

	for _, test := range table {
		c.Check(strutil.CommaSeparatedList(test.in), check.DeepEquals, test.out, check.Commentf("%q", test.in))
	}
}

func (strutilSuite) TestEllipt(c *check.C) {
	type T struct {
		in    string
		n     int
		right string
		left  string
	}
	for _, t := range []T{
		{"", 10, "", ""},
		{"", -1, "", ""},
		{"hello", -1, "…", "…"},
		{"hello", 0, "…", "…"},
		{"hello", 1, "…", "…"},
		{"hello", 2, "h…", "…o"},
		{"hello", 3, "he…", "…lo"},
		{"hello", 4, "hel…", "…llo"},
		{"hello", 5, "hello", "hello"},
		{"hello", 10, "hello", "hello"},
		{"héllo", 4, "hé…", "…llo"},
		{"héllo", 3, "he…", "…lo"},
		{"he🐧lo", 4, "he🐧…", "…🐧lo"},
		{"he🐧lo", 3, "he…", "…lo"},
	} {
		c.Check(strutil.ElliptRight(t.in, t.n), check.Equals, t.right, check.Commentf("%q[:%d] -> %q", t.in, t.n, t.right))
		c.Check(strutil.ElliptLeft(t.in, t.n), check.Equals, t.left, check.Commentf("%q[-%d:] -> %q", t.in, t.n, t.left))
	}
}

func (strutilSuite) TestSortedListsUniqueMerge(c *check.C) {
	l1 := []string{"a", "a", "c", "d", "e", "f", "h", "h"}
	l2 := []string{"b", "c", "d", "d", "g"}
	l3 := []string{"a", "b", "c", "d", "e", "f", "g", "h"}

	tests := []struct {
		sl1 []string
		sl2 []string
		res []string
	}{
		{nil, nil, nil},
		{nil, []string{"a", "a", "b"}, []string{"a", "b"}},
		{[]string{"a", "a", "b"}, nil, []string{"a", "b"}},
		{l1, l2, l3},
		{l2, l1, l3},
		{l3, l3, l3},
	}

	for _, t := range tests {
		res := strutil.SortedListsUniqueMerge(t.sl1, t.sl2)
		c.Check(res, check.DeepEquals, t.res)
	}
}
