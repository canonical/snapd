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

package removed

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/check.v1"

	"launchpad.net/snappy/dirs"
	"launchpad.net/snappy/pkg"
	"launchpad.net/snappy/progress"
	"launchpad.net/snappy/snappy"
)

type removedSuite struct{}

func Test(t *testing.T) { check.TestingT(t) }

var _ = check.Suite(&removedSuite{})

func (s *removedSuite) SetUpTest(c *check.C) {
	dirs.SetRootDir(c.MkDir())
	c.Check(os.MkdirAll(filepath.Join(dirs.SnapDataDir, "foo.bar", "1"), 0755), check.IsNil)
}

func (s *removedSuite) MkStoreYaml(c *check.C, pkgType pkg.Type) {
	// creating the part to get its manifest path is cheating, a little
	part := &Removed{
		name:    "foo",
		origin:  "bar",
		version: "1",
		pkgType: pkgType,
	}

	content := `
name: foo
origin: bar
version: 1
type: app
description: |-
  bla bla bla
publisher: example.com
iconurl: http://i.stack.imgur.com/i8q1U.jpg
downloadsize: 5554242
`
	p := snappy.RemoteManifestPath(part)
	c.Assert(os.MkdirAll(filepath.Dir(p), 0755), check.IsNil)
	c.Assert(ioutil.WriteFile(p, []byte(content), 0644), check.IsNil)

}

func (s *removedSuite) TestNoStore(c *check.C) {
	part := New("foo", "bar", "1", pkg.TypeApp)

	c.Check(part.Name(), check.Equals, "foo")
	c.Check(part.Origin(), check.Equals, "bar")
	c.Check(part.Version(), check.Equals, "1")
	c.Check(part.Description(), check.Equals, "")
	c.Check(part.Vendor(), check.Equals, "")
	c.Check(part.Hash(), check.Equals, "")
	c.Check(part.Icon(), check.Equals, "")
	c.Check(part.DownloadSize(), check.Equals, int64(-1))

	c.Check(part.InstalledSize(), check.Equals, int64(-1))
	c.Check(part.IsActive(), check.Equals, false)
	c.Check(part.IsInstalled(), check.Equals, false)
	c.Check(part.NeedsReboot(), check.Equals, false)

	prog := &progress.NullProgress{}
	c.Check(part.Uninstall(prog), check.Equals, ErrRemoved)
	c.Check(part.SetActive(true, prog), check.Equals, ErrRemoved)
	_, err := part.Install(prog, 0)
	c.Check(err, check.Equals, ErrRemoved)
	_, err = part.Config(nil)
	c.Check(err, check.Equals, ErrRemoved)
	_, err = part.Frameworks()
	c.Check(err, check.Equals, ErrRemoved)
}

func (s *removedSuite) TestNoOrigin(c *check.C) {
	part := New("foo", "", "1", pkg.TypeFramework)
	c.Check(part.Origin(), check.Equals, snappy.SideloadedOrigin)

	s.MkStoreYaml(c, pkg.TypeFramework)
	part = New("foo", "", "1", pkg.TypeFramework)
	c.Check(part.Origin(), check.Equals, "bar")
}

func (s *removedSuite) TestWithStore(c *check.C) {
	s.MkStoreYaml(c, pkg.TypeApp)
	part := New("foo", "bar", "1", pkg.TypeApp)

	c.Check(part.Name(), check.Equals, "foo")
	c.Check(part.Origin(), check.Equals, "bar")
	c.Check(part.Version(), check.Equals, "1")
	c.Check(part.Description(), check.Equals, "bla bla bla")
	c.Check(part.Vendor(), check.Equals, "example.com")
	c.Check(part.Hash(), check.Equals, "")
	c.Check(part.Icon(), check.Equals, "http://i.stack.imgur.com/i8q1U.jpg")
	c.Check(part.DownloadSize(), check.Equals, int64(5554242))

	c.Check(part.InstalledSize(), check.Equals, int64(-1))
	c.Check(part.IsActive(), check.Equals, false)
	c.Check(part.IsInstalled(), check.Equals, false)
	c.Check(part.NeedsReboot(), check.Equals, false)

	prog := &progress.NullProgress{}
	c.Check(part.Uninstall(prog), check.Equals, ErrRemoved)
	c.Check(part.SetActive(true, prog), check.Equals, ErrRemoved)
	_, err := part.Install(prog, 0)
	c.Check(err, check.Equals, ErrRemoved)
	_, err = part.Config(nil)
	c.Check(err, check.Equals, ErrRemoved)
	_, err = part.Frameworks()
	c.Check(err, check.Equals, ErrRemoved)
}
