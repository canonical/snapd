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

package lightweight

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"launchpad.net/snappy/dirs"
	"launchpad.net/snappy/pkg"
	"launchpad.net/snappy/pkg/remote"
	"launchpad.net/snappy/pkg/removed"
	"launchpad.net/snappy/snappy"
)

type lightweightSuite struct {
	d string
}

func Test(t *testing.T) { check.TestingT(t) }

var _ = check.Suite(&lightweightSuite{})

func (s *lightweightSuite) SetUpTest(c *check.C) {
	s.d = c.MkDir()
	dirs.SetRootDir(s.d)

	s.MkInstalled(c, pkg.TypeApp, dirs.SnapAppsDir, "foo", "bar", "1.0", true)
	s.MkRemoved(c, "foo.bar", "0.9")
	s.MkRemoved(c, "foo.baz", "0.8")

	s.MkInstalled(c, pkg.TypeFramework, dirs.SnapAppsDir, "fmk", "", "123", false)
	s.MkInstalled(c, pkg.TypeFramework, dirs.SnapAppsDir, "fmk", "", "120", true)
	s.MkInstalled(c, pkg.TypeFramework, dirs.SnapAppsDir, "fmk", "", "119", false)
	s.MkRemoved(c, "fmk", "12a1")

	s.MkRemoved(c, "fmk2", "4.2.0ubuntu1")

	s.MkInstalled(c, pkg.TypeOem, dirs.SnapOemDir, "oem", "", "3", false)

	newCoreRepo = func() repo {
		// you can't ever have a removed systemimagepart, but for testing it'll do
		return mockrepo{removed.New(snappy.SystemImagePartName, snappy.SystemImagePartOrigin, "1", pkg.TypeCore)}
	}
}

func (s *lightweightSuite) TearDownTest(c *check.C) {
	newCoreRepo = newCoreRepoImpl
}

func (s *lightweightSuite) MkInstalled(c *check.C, _type pkg.Type, appdir, name, origin, version string, active bool) {
	qn := name
	if origin != "" {
		qn += "." + origin
	}

	s.MkRemoved(c, qn, version)

	apath := filepath.Join(appdir, qn, version, "meta")
	yaml := fmt.Sprintf("name: %s\nversion: %s\nvendor: example.com\nicon: icon.png\ntype: %s\n", name, version, _type)
	c.Check(os.MkdirAll(apath, 0755), check.IsNil)
	c.Check(ioutil.WriteFile(filepath.Join(apath, "package.yaml"), []byte(yaml), 0644), check.IsNil)
	c.Check(ioutil.WriteFile(filepath.Join(apath, "hashes.yaml"), nil, 0644), check.IsNil)

	if active {
		c.Check(os.Symlink(version, filepath.Join(appdir, qn, "current")), check.IsNil)
		c.Check(os.Symlink(version, filepath.Join(dirs.SnapDataDir, qn, "current")), check.IsNil)
	}
}

func (s *lightweightSuite) MkRemoved(c *check.C, qn, version string) {
	dpath := filepath.Join(dirs.SnapDataDir, qn, version)
	c.Check(os.MkdirAll(dpath, 0755), check.IsNil)
	c.Check(ioutil.WriteFile(filepath.Join(dpath, "test.txt"), []byte("hello there\n"), 0644), check.IsNil)

}

func (s *lightweightSuite) TestLoadBadName(c *check.C) {
	c.Check(func() { PartBagByName("*", "*") }, check.PanicMatches, "invalid name .*")
}

func (s *lightweightSuite) TestMapFmkNoPart(c *check.C) {
	bag := PartBagByName("fmk", "sideload")
	m := bag.Map(nil)
	c.Check(m["installed_size"], check.Matches, "[0-9]+")
	delete(m, "installed_size")
	c.Check(m, check.DeepEquals, map[string]string{
		"name":               "fmk",
		"origin":             "sideload",
		"status":             "active",
		"version":            "120",
		"icon":               filepath.Join(s.d, "apps", "fmk", "120", "icon.png"),
		"type":               "framework",
		"vendor":             "example.com",
		"download_size":      "-1",
		"description":        "",
		"rollback_available": "119",
	})
}

func (s *lightweightSuite) TestMapRemovedFmkNoPart(c *check.C) {
	bag := PartBagByName("fmk2", "sideload")
	m := bag.Map(nil)
	c.Check(m, check.DeepEquals, map[string]string{
		"name":           "fmk2",
		"origin":         "sideload",
		"status":         "removed",
		"version":        "4.2.0ubuntu1",
		"icon":           "",
		"type":           "framework",
		"vendor":         "",
		"installed_size": "-1",
		"download_size":  "-1",
		"description":    "",
	})
}

func (s *lightweightSuite) TestMapRemovedFmkNoPartButStoreMeta(c *check.C) {
	snap := remote.Snap{
		Name:         "fmk2",
		Origin:       "fmk2origin",
		Version:      "4.2.0ubuntu1",
		Type:         pkg.TypeFramework,
		IconURL:      "http://example.com/icon",
		DownloadSize: 42,
		Publisher:    "Example Inc.",
	}
	part := snappy.NewRemoteSnapPart(snap)

	content, err := yaml.Marshal(snap)
	c.Assert(err, check.IsNil)

	p := snappy.RemoteManifestPath(part)
	c.Assert(os.MkdirAll(filepath.Dir(p), 0755), check.IsNil)
	c.Assert(ioutil.WriteFile(p, content, 0644), check.IsNil)

	bag := PartBagByName("fmk2", "fmk2origin")
	m := bag.Map(nil)
	c.Check(m, check.DeepEquals, map[string]string{
		"name":           "fmk2",
		"origin":         "fmk2origin",
		"status":         "removed",
		"version":        "4.2.0ubuntu1",
		"icon":           "http://example.com/icon",
		"type":           "framework",
		"vendor":         "Example Inc.",
		"installed_size": "-1",
		"download_size":  "42",
		"description":    "",
	})
}

func (s *lightweightSuite) TestMapAppNoPart(c *check.C) {
	bag := PartBagByName("foo", "bar")
	m := bag.Map(nil)
	c.Check(m["installed_size"], check.Matches, "[0-9]+")
	delete(m, "installed_size")
	c.Check(m, check.DeepEquals, map[string]string{
		"name":          "foo",
		"origin":        "bar",
		"status":        "active",
		"version":       "1.0",
		"icon":          filepath.Join(s.d, "apps", "foo.bar", "1.0", "icon.png"),
		"type":          "app",
		"vendor":        "example.com",
		"download_size": "-1",
		"description":   "",
	})
}

func (s *lightweightSuite) TestMapAppWithPart(c *check.C) {
	snap := remote.Snap{
		Name:         "foo",
		Origin:       "bar",
		Version:      "2",
		Type:         pkg.TypeApp,
		IconURL:      "http://example.com/icon",
		DownloadSize: 42,
	}
	part := snappy.NewRemoteSnapPart(snap)

	bag := PartBagByName("foo", "bar")
	m := bag.Map(part)
	c.Check(m["installed_size"], check.Matches, "[0-9]+")
	delete(m, "installed_size")
	c.Check(m, check.DeepEquals, map[string]string{
		"name":             "foo",
		"origin":           "bar",
		"status":           "active",
		"version":          "1.0",
		"icon":             filepath.Join(s.d, "apps", "foo.bar", "1.0", "icon.png"),
		"type":             "app",
		"vendor":           "example.com",
		"download_size":    "42",
		"description":      "",
		"update_available": "2",
	})
}

func (s *lightweightSuite) TestMapAppNoPartBag(c *check.C) {
	snap := remote.Snap{
		Name:         "foo",
		Origin:       "bar",
		Version:      "2",
		Type:         pkg.TypeApp,
		IconURL:      "http://example.com/icon",
		Publisher:    "example.com",
		DownloadSize: 42,
	}
	part := snappy.NewRemoteSnapPart(snap)

	m := (*PartBag)(nil).Map(part)
	c.Check(m, check.DeepEquals, map[string]string{
		"name":           "foo",
		"origin":         "bar",
		"status":         "not installed",
		"version":        "2",
		"icon":           snap.IconURL,
		"type":           "app",
		"vendor":         "example.com",
		"installed_size": "-1",
		"download_size":  "42",
		"description":    "",
	})

}

func (s *lightweightSuite) TestMapRemovedAppNoPart(c *check.C) {
	bag := PartBagByName("foo", "baz")
	m := bag.Map(nil)
	c.Check(m, check.DeepEquals, map[string]string{
		"name":           "foo",
		"origin":         "baz",
		"status":         "removed",
		"version":        "0.8",
		"icon":           "",
		"type":           "app",
		"vendor":         "",
		"installed_size": "-1",
		"download_size":  "-1",
		"description":    "",
	})
}

func (s *lightweightSuite) TestMapInactiveOemNoPart(c *check.C) {
	bag := PartBagByName("oem", "canonical")
	m := bag.Map(nil)
	c.Check(m["installed_size"], check.Matches, "[0-9]+")
	delete(m, "installed_size")
	c.Check(m, check.DeepEquals, map[string]string{
		"name":          "oem",
		"origin":        "sideload", // best guess
		"status":        "installed",
		"version":       "3",
		"icon":          filepath.Join(s.d, "oem", "oem", "3", "icon.png"),
		"type":          "oem",
		"vendor":        "example.com",
		"download_size": "-1",
		"description":   "",
	})
}

func (s *lightweightSuite) TestLoadBadApp(c *check.C) {
	s.MkRemoved(c, "quux.blah", "1")
	// an unparsable package.yaml:
	c.Check(os.MkdirAll(filepath.Join(dirs.SnapAppsDir, "quux.blah", "1", "meta", "package.yaml"), 0755), check.IsNil)

	bag := PartBagByName("quux", "blah")
	c.Assert(bag, check.NotNil)
	c.Assert(bag.Versions, check.DeepEquals, []string{"1"})

	p, err := bag.Load(0)
	c.Check(err, check.NotNil)
	c.Check(p, check.IsNil)
	c.Check(p == nil, check.Equals, true) // NOTE this is stronger than the above
}

func (s *lightweightSuite) TestLoadFmk(c *check.C) {
	bag := PartBagByName("fmk", "")
	c.Assert(bag, check.NotNil)
	c.Assert(bag.Versions, check.HasLen, 4)
	// versions are sorted backwards by version -- index 0 is always newest version
	c.Check(bag.Versions, check.DeepEquals, []string{"123", "120", "119", "12a1"})
	// other things are as expected
	c.Check(bag.Name, check.Equals, "fmk")
	c.Check(bag.Type, check.Equals, pkg.TypeFramework)
	c.Check(bag.ActiveIndex(), check.Equals, 1)

	c.Check(bag.IsInstalled(0), check.Equals, true)
	p, err := bag.Load(0)
	c.Check(err, check.IsNil)
	// load loaded the right implementation of Part
	c.Check(p, check.FitsTypeOf, new(snappy.SnapPart))
	c.Check(p.IsActive(), check.Equals, false)
	c.Check(p.Version(), check.Equals, "123")

	c.Check(bag.IsInstalled(1), check.Equals, true)
	p, err = bag.Load(1)
	c.Check(err, check.IsNil)
	c.Check(p, check.FitsTypeOf, new(snappy.SnapPart))
	c.Check(p.IsActive(), check.Equals, true)
	c.Check(p.Version(), check.Equals, "120")

	c.Check(bag.IsInstalled(2), check.Equals, true)
	p, err = bag.Load(2)
	c.Check(err, check.IsNil)
	c.Check(p, check.FitsTypeOf, new(snappy.SnapPart))
	c.Check(p.IsActive(), check.Equals, false)
	c.Check(p.Version(), check.Equals, "119")

	c.Check(bag.IsInstalled(3), check.Equals, false)
	p, err = bag.Load(3)
	c.Check(err, check.IsNil)
	c.Check(p, check.FitsTypeOf, new(removed.Removed))
	c.Check(p.Version(), check.Equals, "12a1")

	_, err = bag.Load(42)
	c.Check(err, check.Equals, ErrBadVersionIndex)

}

func (s *lightweightSuite) TestLoadApp(c *check.C) {
	bag0 := PartBagByName("foo", "bar")
	bag1 := PartBagByName("foo", "baz")

	c.Check(bag0.QualifiedName(), check.Equals, "foo.bar")
	c.Check(bag0.Versions, check.DeepEquals, []string{"1.0", "0.9"})
	c.Check(bag0.Type, check.Equals, pkg.TypeApp)
	c.Check(bag0.ActiveIndex(), check.Equals, 0)

	c.Check(bag1.QualifiedName(), check.Equals, "foo.baz")
	c.Check(bag1.Versions, check.DeepEquals, []string{"0.8"})
	c.Check(bag1.Type, check.Equals, pkg.TypeApp)
	c.Check(bag1.ActiveIndex(), check.Equals, -1)

	c.Check(bag0.IsInstalled(0), check.Equals, true)
	p, err := bag0.Load(0)
	c.Check(err, check.IsNil)
	c.Check(p, check.FitsTypeOf, new(snappy.SnapPart))
	c.Check(p.IsActive(), check.Equals, true)
	c.Check(p.Version(), check.Equals, "1.0")

	c.Check(bag0.IsInstalled(1), check.Equals, false)
	p, err = bag0.Load(1)
	c.Check(err, check.IsNil)
	c.Check(p, check.FitsTypeOf, new(removed.Removed))
	c.Check(p.IsActive(), check.Equals, false)
	c.Check(p.Version(), check.Equals, "0.9")

	c.Check(bag1.IsInstalled(0), check.Equals, false)
	p, err = bag1.Load(0)
	c.Check(err, check.IsNil)
	c.Check(p, check.FitsTypeOf, new(removed.Removed))
	c.Check(p.IsActive(), check.Equals, false)
	c.Check(p.Version(), check.Equals, "0.8")
}

func (s *lightweightSuite) TestLoadOem(c *check.C) {
	oem := PartBagByName("oem", "whatever")
	c.Assert(oem, check.NotNil)
	c.Check(oem.Versions, check.DeepEquals, []string{"3"})
	c.Check(oem.Type, check.Equals, pkg.TypeOem)

	c.Check(oem.IsInstalled(0), check.Equals, true)
	c.Check(oem.ActiveIndex(), check.Equals, -1)
	p, err := oem.Load(0)
	c.Check(err, check.IsNil)
	c.Check(p, check.FitsTypeOf, new(snappy.OemSnap))
	c.Check(p.Version(), check.Equals, "3")
}

type mockrepo struct{ p snappy.Part }

func (r mockrepo) All() ([]snappy.Part, error) {
	return []snappy.Part{r.p}, nil
}

func (s *lightweightSuite) TestLoadCore(c *check.C) {
	core := PartBagByName(snappy.SystemImagePartName, snappy.SystemImagePartOrigin)
	c.Assert(core, check.NotNil)
	c.Check(core.Versions, check.DeepEquals, []string{"1"})

	c.Check(core.IsInstalled(0), check.Equals, true)
	c.Check(core.ActiveIndex(), check.Equals, 0)
	p, err := core.Load(0)
	c.Check(err, check.IsNil)
	c.Check(p.Version(), check.Equals, "1")
}

func (s *lightweightSuite) TestAll(c *check.C) {
	all := AllPartBags()

	c.Check(all, check.HasLen, 6) // 2 fmk, 2 app, 1 oem, 1 core
}
