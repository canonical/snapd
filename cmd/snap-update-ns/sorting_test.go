// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package main

import (
	"sort"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type sortSuite struct{}

var _ = Suite(&sortSuite{})

func (s *sortSuite) TestTrailingSlashesComparison(c *C) {
	// Naively sorted entries.
	entries := []osutil.MountEntry{
		{Dir: "/a/b"},
		{Dir: "/a/b-1"},
		{Dir: "/a/b-1/3"},
		{Dir: "/a/b/c"},
	}
	sort.Sort(byOriginAndMagicDir(entries))
	// Entries sorted as if they had a trailing slash.
	c.Assert(entries, DeepEquals, []osutil.MountEntry{
		{Dir: "/a/b-1"},
		{Dir: "/a/b-1/3"},
		{Dir: "/a/b"},
		{Dir: "/a/b/c"},
	})
}

func (s *sortSuite) TestParallelInstancesAndSimple(c *C) {
	// Naively sorted entries.
	entries := []osutil.MountEntry{
		{Dir: "/a/b"},
		{Dir: "/a/b-1"},
		{Dir: "/snap/bar", Options: []string{osutil.XSnapdOriginParallelInstance()}},
		{Dir: "/a/b-1/3"},
		{Dir: "/foo/bar", Options: []string{osutil.XSnapdOriginParallelInstance()}},
		{Dir: "/a/b/c"},
	}
	sort.Sort(byOriginAndMagicDir(entries))
	// Entries sorted as if they had a trailing slash.
	c.Assert(entries, DeepEquals, []osutil.MountEntry{
		{Dir: "/foo/bar", Options: []string{osutil.XSnapdOriginParallelInstance()}},
		{Dir: "/snap/bar", Options: []string{osutil.XSnapdOriginParallelInstance()}},
		{Dir: "/a/b-1"},
		{Dir: "/a/b-1/3"},
		{Dir: "/a/b"},
		{Dir: "/a/b/c"},
	})
}
