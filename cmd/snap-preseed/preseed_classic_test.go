// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package main_test

import (
	"testing"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-preseed"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/squashfs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&startPreseedSuite{})

type startPreseedSuite struct {
	testutil.BaseTest
}

func (s *startPreseedSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	restore := squashfs.MockNeedsFuse(false)
	s.BaseTest.AddCleanup(restore)
}

func (s *startPreseedSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("")
}

func testParser(c *C) *flags.Parser {
	parser := main.Parser()
	_ := mylog.Check2(parser.ParseArgs([]string{}))

	return parser
}

func (s *startPreseedSuite) TestRequiresRoot(c *C) {
	restore := main.MockOsGetuid(func() int {
		return 1000
	})
	defer restore()

	parser := testParser(c)
	c.Check(main.Run(parser, []string{"/"}), ErrorMatches, `must be run as root`)
}

func (s *startPreseedSuite) TestMissingArg(c *C) {
	restore := main.MockOsGetuid(func() int {
		return 0
	})
	defer restore()

	parser := testParser(c)
	c.Check(main.Run(parser, nil), ErrorMatches, `need chroot path as argument`)
}

func (s *startPreseedSuite) TestRunPreseedAgainstFilesystemRoot(c *C) {
	restore := main.MockOsGetuid(func() int { return 0 })
	defer restore()

	parser := testParser(c)
	c.Assert(main.Run(parser, []string{"/"}), ErrorMatches, `cannot run snap-preseed against /`)
}

func (s *startPreseedSuite) TestRunPreseedClassicHappy(c *C) {
	restore := main.MockOsGetuid(func() int {
		return 0
	})
	defer restore()

	var called bool
	restorePreseed := main.MockPreseedClassic(func(dir string) error {
		c.Check(dir, Equals, "/a/dir")
		called = true
		return nil
	})
	defer restorePreseed()

	parser := testParser(c)
	c.Assert(main.Run(parser, []string{"/a/dir"}), IsNil)
	c.Check(called, Equals, true)
}

func (s *startPreseedSuite) TestResetReexeced(c *C) {
	restore := main.MockOsGetuid(func() int {
		return 0
	})
	defer restore()

	var called bool
	main.MockResetPreseededChroot(func(dir string) error {
		c.Check(dir, Equals, "/")
		called = true
		return nil
	})

	parser := testParser(c)
	c.Assert(main.Run(parser, []string{"--reset-chroot"}), IsNil)
	c.Check(called, Equals, true)
}

func (s *startPreseedSuite) TestReset(c *C) {
	restore := main.MockOsGetuid(func() int {
		return 0
	})
	defer restore()

	var called bool
	main.MockPreseedClassicReset(func(dir string) error {
		c.Check(dir, Equals, "/a/dir")
		called = true
		return nil
	})

	parser := testParser(c)
	c.Assert(main.Run(parser, []string{"--reset", "/a/dir"}), IsNil)
	c.Check(called, Equals, true)
}

func (s *startPreseedSuite) TestReadInfoValidity(c *C) {
	var called bool
	inf := &snap.Info{
		BadInterfaces: make(map[string]string),
		Plugs: map[string]*snap.PlugInfo{
			"foo": {
				Interface: "bad",
			},
		},
	}

	// set an empty sanitize method.
	snap.SanitizePlugsSlots = func(*snap.Info) { called = true }

	parser := testParser(c)
	tmpDir := c.MkDir()
	_ = main.Run(parser, []string{tmpDir})

	// real sanitize method should be set after Run()
	snap.SanitizePlugsSlots(inf)
	c.Assert(called, Equals, false)
	c.Assert(inf.BadInterfaces, HasLen, 1)
}
