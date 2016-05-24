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
	"path/filepath"
	"sort"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/wrappers"
)

type environmentTestSuite struct {
	tempdir string
}

var _ = Suite(&environmentTestSuite{})

func (s *environmentTestSuite) SetUpTest(c *C) {
	s.tempdir = c.MkDir()
	dirs.SetRootDir(s.tempdir)
}

func (s *environmentTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

const packageHelloEnv = `name: hello-snap
version: 1.10
summary: hello
description: Hello...
environment:
 LD_LIBRARY_PATH: /some/dir
 FOO: global
apps:
 hello:
   command: bin/hello
   environment:
    FOO: per-app
 svc1:
  command: bin/hello
`

func (s *environmentTestSuite) TestAddSnapEnvironmentAndRemove(c *C) {
	info := snaptest.MockSnap(c, packageHelloEnv, &snap.SideInfo{Revision: snap.R(11)})

	err := wrappers.AddSnapEnvironment(info)
	c.Assert(err, IsNil)

	// the app
	helloFile := filepath.Join(s.tempdir, "/var/lib/snapd/environment/snap.hello-snap.hello")

	content, err := ioutil.ReadFile(helloFile)
	c.Assert(err, IsNil)
	lines := strings.Split(string(content), "\n")
	sort.Strings(lines)
	c.Assert(lines, DeepEquals, []string{
		"",
		"FOO=per-app",
		"LD_LIBRARY_PATH=/some/dir",
	})

	// the service
	svcFile := filepath.Join(s.tempdir, "/var/lib/snapd/environment/snap.hello-snap.svc1")

	content, err = ioutil.ReadFile(svcFile)
	c.Assert(err, IsNil)
	lines = strings.Split(string(content), "\n")
	sort.Strings(lines)
	c.Assert(lines, DeepEquals, []string{
		"",
		"FOO=global",
		"LD_LIBRARY_PATH=/some/dir",
	})

	// ensure removal works
	err = wrappers.RemoveSnapEnvironment(info)
	c.Assert(err, IsNil)

	c.Check(osutil.FileExists(helloFile), Equals, false)
	c.Check(osutil.FileExists(svcFile), Equals, false)
}
