// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package desktopentry_test

import (
	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/desktop/desktopentry"
)

func (s *desktopentrySuite) TestExpandExecHelper(c *C) {
	de := &desktopentry.DesktopEntry{
		Filename: "/path/file.desktop",
		Name:     "App Name",
		Icon:     "/path/icon.png",
	}
	for i, tc := range []struct {
		in   string
		uris []string
		out  []string
		err  string
	}{{
		in:  "foo --bar",
		out: []string{"foo", "--bar"},
	}, {
		in:   "foo --bar",
		uris: []string{"file:///test"},
		out:  []string{"foo", "--bar", "/test"},
	}, {
		in:   "foo --bar",
		uris: []string{"file:///Bob%27s%20files/x"},
		out:  []string{"foo", "--bar", "/Bob's files/x"},
	}, {
		in:   "foo --bar",
		uris: []string{"http://example.org"},
		err:  `"http://example.org" is not a file URI`,
	}, {
		in:  "foo %f",
		out: []string{"foo"},
	}, {
		in:  "foo %F",
		out: []string{"foo"},
	}, {
		in:  "foo %u",
		out: []string{"foo"},
	}, {
		in:  "foo %U",
		out: []string{"foo"},
	}, {
		in:   "foo %f bar",
		uris: []string{"file:///test1", "file:///test2"},
		out:  []string{"foo", "/test1", "bar"},
	}, {
		in:   "foo %F bar",
		uris: []string{"file:///test1", "file:///test2", "file:///test3"},
		out:  []string{"foo", "/test1", "/test2", "/test3", "bar"},
	}, {
		in:   "foo %u bar",
		uris: []string{"file:///test1", "file:///test2"},
		out:  []string{"foo", "/test1", "bar"},
	}, {
		in:   "foo %U bar",
		uris: []string{"file:///test1", "file:///test2", "file:///test3"},
		out:  []string{"foo", "/test1", "/test2", "/test3", "bar"},
	}, {
		in:   "foo %f",
		uris: []string{"http://example.org"},
		err:  `"http://example.org" is not a file URI`,
	}, {
		in:   "foo %F",
		uris: []string{"http://example.org"},
		err:  `"http://example.org" is not a file URI`,
	}, {
		in:   "foo %u",
		uris: []string{"http://example.org"},
		out:  []string{"foo", "http://example.org"},
	}, {
		in:   "foo %U",
		uris: []string{"http://example.org", "http://example.com"},
		out:  []string{"foo", "http://example.org", "http://example.com"},
	}, {
		in:  "foo %i bar",
		out: []string{"foo", "--icon", "/path/icon.png", "bar"},
	}, {
		in:  "foo %c bar",
		out: []string{"foo", "App Name", "bar"},
	}, {
		in:  "foo %k bar",
		out: []string{"foo", "/path/file.desktop", "bar"},
	}, {
		in:  `foo --bar "%%p" %U %D +%s %%`,
		out: []string{"foo", "--bar", "%p", "+", "%"},
	}, {
		in:   "skype --share-file=%f",
		uris: []string{"file:///test"},
		out:  []string{"skype", "--share-file=/test"},
	}, {
		in:  `foo "double quotes" 'single quotes'`,
		out: []string{"foo", "double quotes", "single quotes"},
	}, {
		in:  `foo "unclosed double quote`,
		err: "EOF found when expecting closing quote",
	}, {
		in:   `foo %f`,
		uris: []string{"/foo"},
		err:  `"/foo" is not an absolute URI`,
	}, {
		in:   `foo %f`,
		uris: []string{"foo/bar"},
		err:  `"foo/bar" is not an absolute URI`,
	}, {
		in:   `foo %f`,
		uris: []string{"file:foo/bar"},
		err:  `"file:foo/bar" does not have an absolute file path`,
	}, {
		// Environment variables are not expanded.
		in:  `foo $PATH`,
		out: []string{"foo", "$PATH"},
	}, {
		// Special characters within URIs are preserved in the
		// final command line.
		in:   `foo %f`,
		uris: []string{"file:///special%20chars%5c%27%22%25%20%24foo"},
		out:  []string{"foo", `/special chars\'"% $foo`},
	}, {
		// Undefined behaviour: macro within a single quoted string
		in:   `foo '-f %U %%bar'`,
		uris: []string{"http://example.org"},
		out:  []string{"foo", "-f http://example.org %bar"},
	}, {
		// Undefined behaviour: macro within a double quoted string
		in:   `foo "-f %f %%bar"`,
		uris: []string{"file:///test%27.txt"},
		out:  []string{"foo", "-f '/test'''.txt' %bar"},
	}, {
		// Undefined behaviour: macro within a double quoted string
		in:   `foo "-f %f %%bar"`,
		uris: []string{"file:///test%22.txt"},
		err:  `EOF found when expecting closing quote`,
	}} {
		c.Logf("tc %d", i)

		args := mylog.Check2(desktopentry.ExpandExec(de, tc.in, tc.uris))
		if tc.err != "" {
			c.Assert(err, ErrorMatches, tc.err)
			continue
		}
		c.Check(err, IsNil)
		c.Check(args, DeepEquals, tc.out)
	}
}
