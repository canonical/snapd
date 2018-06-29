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

package snap_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type brokenSuite struct{}

var _ = Suite(&brokenSuite{})

func (s *brokenSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *brokenSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func touch(c *C, path string) {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(path, nil, 0644)
	c.Assert(err, IsNil)
}

func (s *brokenSuite) TestGuessAppsForBrokenBinaries(c *C) {
	touch(c, filepath.Join(dirs.SnapBinariesDir, "foo"))
	touch(c, filepath.Join(dirs.SnapBinariesDir, "foo.bar"))
	touch(c, filepath.Join(dirs.SnapBinariesDir, "foo_instance"))
	touch(c, filepath.Join(dirs.SnapBinariesDir, "foo_instance.baz"))

	info := &snap.Info{SuggestedName: "foo"}
	apps := snap.GuessAppsForBroken(info)
	c.Check(apps, HasLen, 2)
	c.Check(apps["foo"], DeepEquals, &snap.AppInfo{Snap: info, Name: "foo"})
	c.Check(apps["bar"], DeepEquals, &snap.AppInfo{Snap: info, Name: "bar"})

	info = &snap.Info{SuggestedName: "foo", InstanceKey: "instance"}
	apps = snap.GuessAppsForBroken(info)
	c.Check(apps, HasLen, 2)
	c.Check(apps["foo"], DeepEquals, &snap.AppInfo{Snap: info, Name: "foo"})
	c.Check(apps["baz"], DeepEquals, &snap.AppInfo{Snap: info, Name: "baz"})
}

func (s *brokenSuite) TestGuessAppsForBrokenServices(c *C) {
	touch(c, filepath.Join(dirs.SnapServicesDir, "snap.foo.foo.service"))
	touch(c, filepath.Join(dirs.SnapServicesDir, "snap.foo.bar.service"))
	touch(c, filepath.Join(dirs.SnapServicesDir, "snap.foo_instance.foo.service"))
	touch(c, filepath.Join(dirs.SnapServicesDir, "snap.foo_instance.baz.service"))

	info := &snap.Info{SuggestedName: "foo"}
	apps := snap.GuessAppsForBroken(info)
	c.Check(apps, HasLen, 2)
	c.Check(apps["foo"], DeepEquals, &snap.AppInfo{Snap: info, Name: "foo", Daemon: "simple"})
	c.Check(apps["bar"], DeepEquals, &snap.AppInfo{Snap: info, Name: "bar", Daemon: "simple"})

	info = &snap.Info{SuggestedName: "foo", InstanceKey: "instance"}
	apps = snap.GuessAppsForBroken(info)
	c.Check(apps, HasLen, 2)
	c.Check(apps["foo"], DeepEquals, &snap.AppInfo{Snap: info, Name: "foo", Daemon: "simple"})
	c.Check(apps["baz"], DeepEquals, &snap.AppInfo{Snap: info, Name: "baz", Daemon: "simple"})
}

func (s *brokenSuite) TestRenamePlug(c *C) {
	snapInfo := snaptest.MockInvalidInfo(c, `name: core
version: 0
plugs:
  old:
    interface: iface
slots:
  old:
    interface: iface
apps:
  app:
hooks:
  configure:
`, nil)
	c.Assert(snapInfo.Plugs["old"], Not(IsNil))
	c.Assert(snapInfo.Plugs["old"].Name, Equals, "old")
	c.Assert(snapInfo.Slots["old"], Not(IsNil))
	c.Assert(snapInfo.Slots["old"].Name, Equals, "old")
	c.Assert(snapInfo.Apps["app"].Plugs["old"], DeepEquals, snapInfo.Plugs["old"])
	c.Assert(snapInfo.Apps["app"].Slots["old"], DeepEquals, snapInfo.Slots["old"])
	c.Assert(snapInfo.Hooks["configure"].Plugs["old"], DeepEquals, snapInfo.Plugs["old"])

	// Rename the plug now.
	snapInfo.RenamePlug("old", "new")

	// Check that there's no trace of the old plug name.
	c.Assert(snapInfo.Plugs["old"], IsNil)
	c.Assert(snapInfo.Plugs["new"], Not(IsNil))
	c.Assert(snapInfo.Plugs["new"].Name, Equals, "new")
	c.Assert(snapInfo.Apps["app"].Plugs["old"], IsNil)
	c.Assert(snapInfo.Apps["app"].Plugs["new"], DeepEquals, snapInfo.Plugs["new"])
	c.Assert(snapInfo.Hooks["configure"].Plugs["old"], IsNil)
	c.Assert(snapInfo.Hooks["configure"].Plugs["new"], DeepEquals, snapInfo.Plugs["new"])

	// Check that slots with the old name are unaffected.
	c.Assert(snapInfo.Slots["old"], Not(IsNil))
	c.Assert(snapInfo.Slots["old"].Name, Equals, "old")
	c.Assert(snapInfo.Apps["app"].Slots["old"], DeepEquals, snapInfo.Slots["old"])

	// Check that the rename made the snap valid now
	c.Assert(snap.Validate(snapInfo), IsNil)
}
