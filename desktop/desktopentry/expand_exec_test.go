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
		in:   "foo %f",
		uris: []string{"file:///test1", "file:///test2"},
		out:  []string{"foo", "/test1"},
	}, {
		in:   "foo %F",
		uris: []string{"file:///test1", "file:///test2"},
		out:  []string{"foo", "/test1", "/test2"},
	}, {
		in:   "foo %u",
		uris: []string{"file:///test1", "file:///test2"},
		out:  []string{"foo", "file:///test1"},
	}, {
		in:   "foo %U",
		uris: []string{"file:///test1", "file:///test2"},
		out:  []string{"foo", "file:///test1", "file:///test2"},
	}, {
		in:   "foo %U",
		uris: []string{"http://example.org"},
		out:  []string{"foo", "http://example.org"},
	}, {
		in:  "foo %i",
		out: []string{"foo", "--icon", "/path/icon.png"},
	}, {
		in:  "foo %c",
		out: []string{"foo", "App Name"},
	}, {
		in:  "foo %k",
		out: []string{"foo", "/path/file.desktop"},
	}, {
		in:  `foo --bar "%%p" %U %D +%s %%`,
		out: []string{"foo", "--bar", "%p", "+", "%"},
	}, {
		in:   "skype --share-file=%f",
		uris: []string{"file:///test"},
		out:  []string{"skype", "--share-file=/test"},
	}, {
		in:  `foo "unclosed double quote`,
		err: "EOF found when expecting closing quote",
	}} {
		c.Logf("tc %d", i)

		args, err := desktopentry.ExpandExec(de, tc.in, tc.uris)
		if tc.err != "" {
			c.Assert(err, ErrorMatches, tc.err)
			continue
		}
		c.Check(err, IsNil)
		c.Check(args, DeepEquals, tc.out)
	}
}
