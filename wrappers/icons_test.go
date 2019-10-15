// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"sort"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/wrappers"
)

type iconsTestSuite struct {
	testutil.BaseTest
	tempdir string
}

var _ = Suite(&iconsTestSuite{})

func (s *iconsTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	s.tempdir = c.MkDir()
	dirs.SetRootDir(s.tempdir)
}

func (s *iconsTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	s.BaseTest.TearDownTest(c)
}

func (s *iconsTestSuite) TestFindIconFiles(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(11)})

	baseDir := info.MountDir()
	iconsDir := filepath.Join(baseDir, "meta", "gui", "icons")
	c.Assert(os.MkdirAll(filepath.Join(iconsDir, "hicolor", "256x256", "apps"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(iconsDir, "hicolor", "scalable", "apps"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(iconsDir, "hicolor", "256x256", "apps", "snap.hello-snap.foo.png"), []byte("256x256"), 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(iconsDir, "hicolor", "scalable", "apps", "snap.hello-snap.foo.svg"), []byte("scalable"), 0644), IsNil)

	// Some files that shouldn't be picked up
	c.Assert(ioutil.WriteFile(filepath.Join(iconsDir, "hicolor", "scalable", "apps", "snap.hello-snap.foo.exe"), []byte("bad extension"), 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(iconsDir, "hicolor", "scalable", "apps", "org.example.hello.png"), []byte("bad prefix"), 0644), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(iconsDir, "snap.whatever"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(iconsDir, "snap.whatever", "snap.hello-snap.foo.png"), []byte("bad dir"), 0644), IsNil)

	icons, err := wrappers.FindIconFiles(info.SnapName(), iconsDir)
	sort.Strings(icons)
	c.Assert(err, IsNil)
	c.Check(icons, DeepEquals, []string{
		"hicolor/256x256/apps/snap.hello-snap.foo.png",
		"hicolor/scalable/apps/snap.hello-snap.foo.svg",
	})
}

func (s *iconsTestSuite) TestAddSnapIcons(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(11)})

	baseDir := info.MountDir()
	iconsDir := filepath.Join(baseDir, "meta", "gui", "icons")
	c.Assert(os.MkdirAll(filepath.Join(iconsDir, "hicolor", "scalable", "apps"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(iconsDir, "hicolor", "scalable", "apps", "snap.hello-snap.foo.svg"), []byte("scalable"), 0644), IsNil)

	c.Assert(wrappers.AddSnapIcons(info), IsNil)
	iconFile := filepath.Join(dirs.SnapDesktopIconsDir, "hicolor", "scalable", "apps", "snap.hello-snap.foo.svg")
	c.Check(iconFile, testutil.FileEquals, "scalable")
}

func (s *iconsTestSuite) TestRemoveSnapIcons(c *C) {
	iconDir := filepath.Join(dirs.SnapDesktopIconsDir, "hicolor", "scalable", "apps")
	iconFile := filepath.Join(iconDir, "snap.hello-snap.foo.svg")
	c.Assert(os.MkdirAll(iconDir, 0755), IsNil)
	c.Assert(ioutil.WriteFile(iconFile, []byte("contents"), 0644), IsNil)

	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(11)})
	c.Assert(wrappers.RemoveSnapIcons(info), IsNil)
	c.Check(iconFile, testutil.FileAbsent)
}

func (s *iconsTestSuite) TestParallelInstanceAddIcons(c *C) {
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(11)})
	info.InstanceKey = "instance"

	baseDir := info.MountDir()
	iconsDir := filepath.Join(baseDir, "meta", "gui", "icons")
	c.Assert(os.MkdirAll(filepath.Join(iconsDir, "hicolor", "scalable", "apps"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(iconsDir, "hicolor", "scalable", "apps", "snap.hello-snap.foo.svg"), []byte("scalable"), 0644), IsNil)

	c.Assert(wrappers.AddSnapIcons(info), IsNil)
	iconFile := filepath.Join(dirs.SnapDesktopIconsDir, "hicolor", "scalable", "apps", "snap.hello-snap_instance.foo.svg")
	c.Check(iconFile, testutil.FileEquals, "scalable")

	// No file installed without the instance qualifier
	iconFile = filepath.Join(dirs.SnapDesktopIconsDir, "hicolor", "scalable", "apps", "snap.hello-snap.foo.svg")
	c.Check(iconFile, testutil.FileAbsent)
}

func (s *iconsTestSuite) TestParallelInstanceRemoveIcons(c *C) {
	iconDir := filepath.Join(dirs.SnapDesktopIconsDir, "hicolor", "scalable", "apps")
	c.Assert(os.MkdirAll(iconDir, 0755), IsNil)
	snapNameFile := filepath.Join(iconDir, "snap.hello-snap.foo.svg")
	c.Assert(ioutil.WriteFile(snapNameFile, []byte("contents"), 0644), IsNil)
	instanceNameFile := filepath.Join(iconDir, "snap.hello-snap_instance.foo.svg")
	c.Assert(ioutil.WriteFile(instanceNameFile, []byte("contents"), 0644), IsNil)

	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(11)})
	info.InstanceKey = "instance"
	c.Assert(wrappers.RemoveSnapIcons(info), IsNil)
	c.Check(instanceNameFile, testutil.FileAbsent)
	// The non-instance qualified icon remains
	c.Check(snapNameFile, testutil.FilePresent)
}
