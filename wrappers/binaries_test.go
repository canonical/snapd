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
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
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
 svc1:
  command: bin/hello
  stop-command: bin/goodbye
  post-stop-command: bin/missya
  daemon: forking
`

func (s *binariesTestSuite) TestAddSnapBinariesAndRemove(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(11)})

	err := wrappers.AddSnapBinaries(info)
	c.Assert(err, IsNil)

	wrapper := filepath.Join(s.tempdir, "/snap/bin/hello-snap.hello")

	content, err := ioutil.ReadFile(wrapper)
	c.Assert(err, IsNil)

	needle := fmt.Sprintf(`
/usr/bin/ubuntu-core-launcher snap.hello-snap.hello snap.hello-snap.hello %s/snap/hello-snap/11/bin/hello "$@"
`, s.tempdir)

	c.Assert(string(content), Matches, "(?ms).*"+regexp.QuoteMeta(needle)+".*")

	err = wrappers.RemoveSnapBinaries(info)
	c.Assert(err, IsNil)

	c.Check(osutil.FileExists(wrapper), Equals, false)
}
