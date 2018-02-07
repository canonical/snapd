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
	"math"
	"math/rand"
	"sort"
	"testing"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
)

func Test(t *testing.T) { check.TestingT(t) }

type strutilSuite struct{}

var _ = check.Suite(&strutilSuite{})

func (ts *strutilSuite) TestMakeRandomString(c *check.C) {
	// for our tests
	rand.Seed(1)

	s1 := strutil.MakeRandomString(10)
	c.Assert(s1, check.Equals, "pw7MpXh0JB")

	s2 := strutil.MakeRandomString(5)
	c.Assert(s2, check.Equals, "4PQyl")
}

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

func (ts *strutilSuite) TestWordWrap(c *check.C) {
	for _, t := range []struct {
		in  string
		out []string
		n   int
	}{
		// pathological
		{"12345", []string{"12345"}, 3},
		{"123 456789", []string{"123", "456789"}, 3},
		// valid
		{"abc def ghi", []string{"abc", "def", "ghi"}, 3},
		{"a b c d e f", []string{"a b", "c d", "e f"}, 3},
		{"ab cd ef", []string{"ab cd", "ef"}, 5},
		// intentional (but slightly strange)
		{"ab            cd", []string{"ab", "cd"}, 2},
	} {
		c.Check(strutil.WordWrap(t.in, t.n), check.DeepEquals, t.out)
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

	out = strutil.TruncateOutput(data, 0, 8)
	c.Assert(out, check.DeepEquals, []byte("ef\ngh\nij"))

	out = strutil.TruncateOutput(data, 0, 1000)
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
