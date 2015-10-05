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

package husk

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

type huskSuite struct {
	d string
}

func Test(t *testing.T) { check.TestingT(t) }

var _ = check.Suite(&huskSuite{})

func (s *huskSuite) SetUpTest(c *check.C) {
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

func (s *huskSuite) TearDownTest(c *check.C) {
	newCoreRepo = newCoreRepoImpl
}

func (s *huskSuite) MkInstalled(c *check.C, _type pkg.Type, appdir, name, origin, version string, active bool) {
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
	}
}

func (s *huskSuite) MkRemoved(c *check.C, qn, version string) {
	dpath := filepath.Join(dirs.SnapDataDir, qn, version)
	c.Check(os.MkdirAll(dpath, 0755), check.IsNil)
	c.Check(ioutil.WriteFile(filepath.Join(dpath, "test.txt"), []byte("hello there\n"), 0644), check.IsNil)

}

func (s *huskSuite) TestLoadBadName(c *check.C) {
	c.Check(func() { ByName("*", "*") }, check.PanicMatches, "invalid name .*")
}

func (s *huskSuite) TestMapFmkNoPart(c *check.C) {
	h := ByName("fmk", "sideload")
	m := h.Map(nil)
	c.Check(m, check.DeepEquals, map[string]string{
		"name":               "fmk",
		"origin":             "sideload",
		"status":             "active",
		"version":            "120",
		"icon":               filepath.Join(s.d, "apps", "fmk", "120", "icon.png"),
		"type":               "framework",
		"vendor":             "example.com",
		"installed_size":     "214", // this'll change :-/
		"download_size":      "-1",
		"description":        "",
		"rollback_available": "119",
	})
}

func (s *huskSuite) TestMapRemovedFmkNoPart(c *check.C) {
	h := ByName("fmk2", "sideload")
	m := h.Map(nil)
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

func (s *huskSuite) TestMapRemovedFmkNoPartButStoreMeta(c *check.C) {
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

	p := snappy.ManifestPath(part)
	c.Assert(os.MkdirAll(filepath.Dir(p), 0755), check.IsNil)
	c.Assert(ioutil.WriteFile(p, content, 0644), check.IsNil)

	h := ByName("fmk2", "fmk2origin")
	m := h.Map(nil)
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

func (s *huskSuite) TestMapAppNoPart(c *check.C) {
	h := ByName("foo", "bar")
	m := h.Map(nil)
	c.Check(m, check.DeepEquals, map[string]string{
		"name":           "foo",
		"origin":         "bar",
		"status":         "active",
		"version":        "1.0",
		"icon":           filepath.Join(s.d, "apps", "foo.bar", "1.0", "icon.png"),
		"type":           "app",
		"vendor":         "example.com",
		"installed_size": "208", // this'll change :-/
		"download_size":  "-1",
		"description":    "",
	})
}

func (s *huskSuite) TestMapAppWithPart(c *check.C) {
	snap := remote.Snap{
		Name:         "foo",
		Origin:       "bar",
		Version:      "2",
		Type:         pkg.TypeApp,
		IconURL:      "http://example.com/icon",
		DownloadSize: 42,
	}
	part := snappy.NewRemoteSnapPart(snap)

	h := ByName("foo", "bar")
	m := h.Map(part)
	c.Check(m, check.DeepEquals, map[string]string{
		"name":             "foo",
		"origin":           "bar",
		"status":           "active",
		"version":          "1.0",
		"icon":             filepath.Join(s.d, "apps", "foo.bar", "1.0", "icon.png"),
		"type":             "app",
		"vendor":           "example.com",
		"installed_size":   "208", // this'll change :-/
		"download_size":    "42",
		"description":      "",
		"update_available": "2",
	})
}

func (s *huskSuite) TestMapAppNoHusk(c *check.C) {
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

	m := (*Husk)(nil).Map(part)
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

func (s *huskSuite) TestMapRemovedAppNoPart(c *check.C) {
	h := ByName("foo", "baz")
	m := h.Map(nil)
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

func (s *huskSuite) TestMapInactiveOemNoPart(c *check.C) {
	h := ByName("oem", "canonical")
	m := h.Map(nil)
	c.Check(m, check.DeepEquals, map[string]string{
		"name":           "oem",
		"origin":         "sideload", // best guess
		"status":         "installed",
		"version":        "3",
		"icon":           filepath.Join(s.d, "oem", "oem", "3", "icon.png"),
		"type":           "oem",
		"vendor":         "example.com",
		"installed_size": "206",
		"download_size":  "-1",
		"description":    "",
	})
}

func (s *huskSuite) TestLoadBadApp(c *check.C) {
	s.MkRemoved(c, "quux.blah", "1")
	// an unparsable package.yaml:
	c.Check(os.MkdirAll(filepath.Join(dirs.SnapAppsDir, "quux.blah", "1", "meta", "package.yaml"), 0755), check.IsNil)

	h := ByName("quux", "blah")
	c.Assert(h, check.NotNil)
	c.Assert(h.Versions, check.DeepEquals, []string{"1"})

	p, err := h.Load(0)
	c.Check(err, check.NotNil)
	c.Check(p, check.IsNil)
	c.Check(p == nil, check.Equals, true) // NOTE this is stronger than the above
}

func (s *huskSuite) TestLoadFmk(c *check.C) {
	h := ByName("fmk", "")
	c.Assert(h, check.NotNil)
	c.Assert(h.Versions, check.HasLen, 4)
	// versions are sorted backwards by version -- index 0 is always newest version
	c.Check(h.Versions, check.DeepEquals, []string{"123", "120", "119", "12a1"})
	// other things are as expected
	c.Check(h.Name, check.Equals, "fmk")
	c.Check(h.Type, check.Equals, pkg.TypeFramework)
	c.Check(h.ActiveIndex(), check.Equals, 1)

	c.Check(h.IsInstalled(0), check.Equals, true)
	p, err := h.Load(0)
	c.Check(err, check.IsNil)
	// load loaded the right implementation of Part
	c.Check(p, check.FitsTypeOf, new(snappy.SnapPart))
	c.Check(p.IsActive(), check.Equals, false)
	c.Check(p.Version(), check.Equals, "123")

	c.Check(h.IsInstalled(1), check.Equals, true)
	p, err = h.Load(1)
	c.Check(err, check.IsNil)
	c.Check(p, check.FitsTypeOf, new(snappy.SnapPart))
	c.Check(p.IsActive(), check.Equals, true)
	c.Check(p.Version(), check.Equals, "120")

	c.Check(h.IsInstalled(2), check.Equals, true)
	p, err = h.Load(2)
	c.Check(err, check.IsNil)
	c.Check(p, check.FitsTypeOf, new(snappy.SnapPart))
	c.Check(p.IsActive(), check.Equals, false)
	c.Check(p.Version(), check.Equals, "119")

	c.Check(h.IsInstalled(3), check.Equals, false)
	p, err = h.Load(3)
	c.Check(err, check.IsNil)
	c.Check(p, check.FitsTypeOf, new(removed.Removed))
	c.Check(p.Version(), check.Equals, "12a1")

	_, err = h.Load(42)
	c.Check(err, check.Equals, ErrBadVersionIndex)

}

func (s *huskSuite) TestLoadApp(c *check.C) {
	h0 := ByName("foo", "bar")
	h1 := ByName("foo", "baz")

	c.Check(h0.QualifiedName(), check.Equals, "foo.bar")
	c.Check(h0.Versions, check.DeepEquals, []string{"1.0", "0.9"})
	c.Check(h0.Type, check.Equals, pkg.TypeApp)
	c.Check(h0.ActiveIndex(), check.Equals, 0)

	c.Check(h1.QualifiedName(), check.Equals, "foo.baz")
	c.Check(h1.Versions, check.DeepEquals, []string{"0.8"})
	c.Check(h1.Type, check.Equals, pkg.TypeApp)
	c.Check(h1.ActiveIndex(), check.Equals, -1)

	c.Check(h0.IsInstalled(0), check.Equals, true)
	p, err := h0.Load(0)
	c.Check(err, check.IsNil)
	c.Check(p, check.FitsTypeOf, new(snappy.SnapPart))
	c.Check(p.IsActive(), check.Equals, true)
	c.Check(p.Version(), check.Equals, "1.0")

	c.Check(h0.IsInstalled(1), check.Equals, false)
	p, err = h0.Load(1)
	c.Check(err, check.IsNil)
	c.Check(p, check.FitsTypeOf, new(removed.Removed))
	c.Check(p.IsActive(), check.Equals, false)
	c.Check(p.Version(), check.Equals, "0.9")

	c.Check(h1.IsInstalled(0), check.Equals, false)
	p, err = h1.Load(0)
	c.Check(err, check.IsNil)
	c.Check(p, check.FitsTypeOf, new(removed.Removed))
	c.Check(p.IsActive(), check.Equals, false)
	c.Check(p.Version(), check.Equals, "0.8")
}

func (s *huskSuite) TestLoadOem(c *check.C) {
	oem := ByName("oem", "whatever")
	c.Assert(oem, check.NotNil)
	c.Check(oem.Versions, check.DeepEquals, []string{"3"})
	c.Check(oem.Type, check.Equals, pkg.TypeOem)

	c.Check(oem.IsInstalled(0), check.Equals, true)
	c.Check(oem.ActiveIndex(), check.Equals, -1)
	p, err := oem.Load(0)
	c.Check(err, check.IsNil)
	c.Check(p, check.FitsTypeOf, new(snappy.SnapPart))
	c.Check(p.Version(), check.Equals, "3")
}

type mockrepo struct{ p snappy.Part }

func (r mockrepo) All() ([]snappy.Part, error) {
	return []snappy.Part{r.p}, nil
}

func (s *huskSuite) TestLoadCore(c *check.C) {
	core := ByName(snappy.SystemImagePartName, snappy.SystemImagePartOrigin)
	c.Assert(core, check.NotNil)
	c.Check(core.Versions, check.DeepEquals, []string{"1"})

	c.Check(core.IsInstalled(0), check.Equals, true)
	c.Check(core.ActiveIndex(), check.Equals, 0)
	p, err := core.Load(0)
	c.Check(err, check.IsNil)
	c.Check(p.Version(), check.Equals, "1")
}

func (s *huskSuite) TestAll(c *check.C) {
	all := All()

	c.Check(all, check.HasLen, 6) // 2 fmk, 2 app, 1 oem, 1 core
}
