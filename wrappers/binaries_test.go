// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package wrappers_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/wrappers"
)

func TestWrappers(t *testing.T) { TestingT(t) }

type binariesTestSuite struct {
	tempdir string
}

var _ = Suite(&binariesTestSuite{})

func (s *binariesTestSuite) SetUpTest(c *C) {
	s.tempdir = c.MkDir()
	dirs.SetRootDir(s.tempdir)
}

func (s *binariesTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

const packageHello = `name: hello-snap
version: 1.10
summary: hello
description: Hello...
apps:
 hello:
   command: bin/hello
 world:
   command: bin/world
   completer: world-completer.sh
 svc1:
  command: bin/hello
  stop-command: bin/goodbye
  post-stop-command: bin/missya
  daemon: forking
`

func (s *binariesTestSuite) TestAddSnapBinariesAndRemove(c *C) {
	// no completers support -> no problem \o/
	c.Assert(osutil.FileExists(dirs.CompletersDir), Equals, false)

	s.testAddSnapBinariesAndRemove(c)
}

func (s *binariesTestSuite) TestAddSnapBinariesAndRemoveWithCompleters(c *C) {
	c.Assert(os.MkdirAll(dirs.CompletersDir, 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Dir(dirs.CompleteSh), 0755), IsNil)
	c.Assert(ioutil.WriteFile(dirs.CompleteSh, nil, 0644), IsNil)
	// full completers support -> we get completers \o/

	s.testAddSnapBinariesAndRemove(c)
}

func (s *binariesTestSuite) TestAddSnapBinariesAndRemoveWithExistingCompleters(c *C) {
	c.Assert(os.MkdirAll(dirs.CompletersDir, 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Dir(dirs.CompleteSh), 0755), IsNil)
	c.Assert(ioutil.WriteFile(dirs.CompleteSh, nil, 0644), IsNil)
	// existing completers -> they're left alone \o/
	c.Assert(ioutil.WriteFile(filepath.Join(dirs.CompletersDir, "hello-snap.world"), nil, 0644), IsNil)

	s.testAddSnapBinariesAndRemove(c)
}

func (s *binariesTestSuite) testAddSnapBinariesAndRemove(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(11)})
	completer := filepath.Join(dirs.CompletersDir, "hello-snap.world")
	completerExisted := osutil.FileExists(completer)

	err := wrappers.AddSnapBinaries(info)
	c.Assert(err, IsNil)

	bins := []string{"hello-snap.hello", "hello-snap.world"}

	for _, bin := range bins {
		link := filepath.Join(dirs.SnapBinariesDir, bin)
		target, err := os.Readlink(link)
		c.Assert(err, IsNil, Commentf(bin))
		c.Check(target, Equals, "/usr/bin/snap", Commentf(bin))
	}

	if osutil.FileExists(dirs.CompletersDir) {
		if completerExisted {
			// there was a completer there before, so it should _not_ be a symlink to our complete.sh
			c.Assert(osutil.IsSymlink(completer), Equals, false)
		} else {
			target, err := os.Readlink(completer)
			c.Assert(err, IsNil)
			c.Check(target, Equals, dirs.CompleteSh)
		}
	}

	err = wrappers.RemoveSnapBinaries(info)
	c.Assert(err, IsNil)

	for _, bin := range bins {
		link := filepath.Join(dirs.SnapBinariesDir, bin)
		c.Check(osutil.FileExists(link), Equals, false, Commentf(bin))
	}

	// we left the existing completer alone, but removed it otherwise
	c.Check(osutil.FileExists(completer), Equals, completerExisted)
}

func (s *binariesTestSuite) TestAddSnapBinariesCleansUpOnFailure(c *C) {
	link := filepath.Join(dirs.SnapBinariesDir, "hello-snap.hello")
	c.Assert(osutil.FileExists(link), Equals, false)
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapBinariesDir, "hello-snap.bye", "potato"), 0755), IsNil)

	info := snaptest.MockSnap(c, packageHello+`
 bye:
  command: bin/bye
`, &snap.SideInfo{Revision: snap.R(11)})

	err := wrappers.AddSnapBinaries(info)
	c.Assert(err, NotNil)

	c.Check(osutil.FileExists(link), Equals, false)
}
