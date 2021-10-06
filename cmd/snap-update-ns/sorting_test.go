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
		{Dir: "/a/b-1"},
		{Dir: "/a/b", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/snap/bar/baz", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/snap/bar", Options: []string{osutil.XSnapdOriginOvername()}},
		{Dir: "/a/b-1/3"},
		{Dir: "/var/snap/bar", Options: []string{osutil.XSnapdOriginOvername()}},
		{Dir: "/a/b/c"},
	}
	sort.Sort(byOriginAndMagicDir(entries))
	// Entries sorted as if they had a trailing slash.
	c.Assert(entries, DeepEquals, []osutil.MountEntry{
		{Dir: "/snap/bar", Options: []string{osutil.XSnapdOriginOvername()}},
		{Dir: "/var/snap/bar", Options: []string{osutil.XSnapdOriginOvername()}},
		{Dir: "/a/b-1"},
		{Dir: "/a/b-1/3"},
		{Dir: "/a/b", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/a/b/c"},
		{Dir: "/snap/bar/baz", Options: []string{osutil.XSnapdOriginLayout()}},
	})
}

func (s *sortSuite) TestOvernameOrder(c *C) {
	expected := []osutil.MountEntry{
		{Dir: "/a/b/2", Options: []string{osutil.XSnapdOriginOvername()}},
		{Dir: "/a/b"},
	}
	entries := []osutil.MountEntry{
		{Dir: "/a/b"},
		{Dir: "/a/b/2", Options: []string{osutil.XSnapdOriginOvername()}},
	}
	entriesRev := []osutil.MountEntry{
		{Dir: "/a/b/2", Options: []string{osutil.XSnapdOriginOvername()}},
		{Dir: "/a/b"},
	}
	sort.Sort(byOriginAndMagicDir(entries))
	c.Assert(entries, DeepEquals, expected)
	sort.Sort(byOriginAndMagicDir(entriesRev))
	c.Assert(entriesRev, DeepEquals, expected)
}

func (s *sortSuite) TestParallelInstancesAlmostSorted(c *C) {
	// use a mount profile that was seen to be broken in the wild
	entries := []osutil.MountEntry{
		{Dir: "/snap/foo", Options: []string{osutil.XSnapdOriginOvername()}},
		{Dir: "/var/snap/foo", Options: []string{osutil.XSnapdOriginOvername()}},
		{Dir: "/snap/foo/44/foo/certs", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/snap/foo/44/foo/config", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/snap/foo/44/usr/bin/python", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/snap/foo/44/usr/bin/python3", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/usr/bin/java", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/usr/bin/java8", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/usr/bin/node", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/usr/bin/nodejs12x", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/usr/bin/python2.7", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/usr/bin/python3.7", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/usr/bin/python3.8", Options: []string{osutil.XSnapdOriginLayout()}},
	}
	sort.Sort(byOriginAndMagicDir(entries))
	// overname entries are always first
	c.Assert(entries, DeepEquals, []osutil.MountEntry{
		{Dir: "/snap/foo", Options: []string{osutil.XSnapdOriginOvername()}},
		{Dir: "/var/snap/foo", Options: []string{osutil.XSnapdOriginOvername()}},
		{Dir: "/snap/foo/44/foo/certs", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/snap/foo/44/foo/config", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/snap/foo/44/usr/bin/python", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/snap/foo/44/usr/bin/python3", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/usr/bin/java", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/usr/bin/java8", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/usr/bin/node", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/usr/bin/nodejs12x", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/usr/bin/python2.7", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/usr/bin/python3.7", Options: []string{osutil.XSnapdOriginLayout()}},
		{Dir: "/usr/bin/python3.8", Options: []string{osutil.XSnapdOriginLayout()}},
	})
}
