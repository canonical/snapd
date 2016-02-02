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

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/remote"
	"github.com/ubuntu-core/snappy/snap/removed"
	"github.com/ubuntu-core/snappy/snappy"
)

type lightweightSuite struct {
	d string
}

func Test(t *testing.T) { check.TestingT(t) }

var _ = check.Suite(&lightweightSuite{})

func (s *lightweightSuite) SetUpTest(c *check.C) {
	s.d = c.MkDir()
	dirs.SetRootDir(s.d)

	s.MkInstalled(c, snap.TypeApp, dirs.SnapSnapsDir, "foo", "bar", "1.0", true)
	s.MkRemoved(c, "foo.bar", "0.9")
	s.MkRemoved(c, "foo.baz", "0.8")

	s.MkInstalled(c, snap.TypeFramework, dirs.SnapSnapsDir, "fmk", "", "123", false)
	s.MkInstalled(c, snap.TypeFramework, dirs.SnapSnapsDir, "fmk", "", "120", true)
	s.MkInstalled(c, snap.TypeFramework, dirs.SnapSnapsDir, "fmk", "", "119", false)
	s.MkRemoved(c, "fmk", "12a1")

	s.MkRemoved(c, "fmk2", "4.2.0ubuntu1")

	s.MkInstalled(c, snap.TypeGadget, dirs.SnapSnapsDir, "a-gadget", "", "3", false)
}

func (s *lightweightSuite) MkInstalled(c *check.C, _type snap.Type, appdir, name, origin, version string, active bool) {
	qn := name
	if origin != "" {
		qn += "." + origin
	}

	s.MkRemoved(c, qn, version)

	apath := filepath.Join(appdir, qn, version, "meta")
	yaml := fmt.Sprintf("name: %s\nversion: %s\ntype: %s\n", name, version, _type)
	c.Check(os.MkdirAll(apath, 0755), check.IsNil)
	c.Check(ioutil.WriteFile(filepath.Join(apath, "snap.yaml"), []byte(yaml), 0644), check.IsNil)
	c.Check(ioutil.WriteFile(filepath.Join(apath, "icon.png"), nil, 0644), check.IsNil)

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
	bag := PartBagByName("fmk", "")
	m := bag.Map(nil)
	c.Check(m["installed_size"], check.FitsTypeOf, int64(0))
	delete(m, "installed_size")
	c.Check(m, check.DeepEquals, map[string]interface{}{
		"name":               "fmk",
		"origin":             "sideload",
		"status":             "active",
		"version":            "120",
		"icon":               filepath.Join(s.d, "snaps", "fmk", "120", "meta/icon.png"),
		"type":               "framework",
		"vendor":             "",
		"download_size":      int64(-1),
		"description":        "",
		"rollback_available": "119",
	})
}

func (s *lightweightSuite) TestMapRemovedFmkNoPart(c *check.C) {
	bag := PartBagByName("fmk2", "")
	m := bag.Map(nil)
	c.Check(m, check.DeepEquals, map[string]interface{}{
		"name":           "fmk2",
		"origin":         "",
		"status":         "removed",
		"version":        "4.2.0ubuntu1",
		"icon":           "",
		"type":           "framework",
		"vendor":         "",
		"installed_size": int64(-1),
		"download_size":  int64(-1),
		"description":    "",
	})
}

func (s *lightweightSuite) TestMapRemovedFmkNoPartButStoreMeta(c *check.C) {
	snap := remote.Snap{
		Name:         "fmk2",
		Origin:       "fmk2origin",
		Version:      "4.2.0ubuntu1",
		Type:         snap.TypeFramework,
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
	c.Check(m, check.DeepEquals, map[string]interface{}{
		"name":           "fmk2",
		"origin":         "fmk2origin",
		"status":         "removed",
		"version":        "4.2.0ubuntu1",
		"icon":           "http://example.com/icon",
		"type":           "framework",
		"vendor":         "",
		"installed_size": int64(-1),
		"download_size":  int64(42),
		"description":    "",
	})
}

func (s *lightweightSuite) TestMapAppNoPart(c *check.C) {
	bag := PartBagByName("foo", "bar")
	m := bag.Map(nil)
	c.Check(m["installed_size"], check.FitsTypeOf, int64(0))
	delete(m, "installed_size")
	c.Check(m, check.DeepEquals, map[string]interface{}{
		"name":          "foo",
		"origin":        "bar",
		"status":        "active",
		"version":       "1.0",
		"icon":          filepath.Join(s.d, "snaps", "foo.bar", "1.0", "meta/icon.png"),
		"type":          "app",
		"vendor":        "",
		"download_size": int64(-1),
		"description":   "",
	})
}

func (s *lightweightSuite) TestMapAppWithPart(c *check.C) {
	snap := remote.Snap{
		Name:         "foo",
		Origin:       "bar",
		Version:      "2",
		Type:         snap.TypeApp,
		IconURL:      "http://example.com/icon",
		DownloadSize: 42,
	}
	part := snappy.NewRemoteSnapPart(snap)

	bag := PartBagByName("foo", "bar")
	m := bag.Map(part)
	c.Check(m["installed_size"], check.FitsTypeOf, int64(0))
	delete(m, "installed_size")
	c.Check(m, check.DeepEquals, map[string]interface{}{
		"name":             "foo",
		"origin":           "bar",
		"status":           "active",
		"version":          "1.0",
		"icon":             filepath.Join(s.d, "snaps", "foo.bar", "1.0", "meta/icon.png"),
		"type":             "app",
		"vendor":           "",
		"download_size":    int64(42),
		"description":      "",
		"update_available": "2",
	})
}

func (s *lightweightSuite) TestMapAppNoPartBag(c *check.C) {
	snap := remote.Snap{
		Name:         "foo",
		Origin:       "bar",
		Version:      "2",
		Type:         snap.TypeApp,
		IconURL:      "http://example.com/icon",
		Publisher:    "example.com",
		DownloadSize: 42,
	}
	part := snappy.NewRemoteSnapPart(snap)

	m := (*PartBag)(nil).Map(part)
	c.Check(m, check.DeepEquals, map[string]interface{}{
		"name":           "foo",
		"origin":         "bar",
		"status":         "not installed",
		"version":        "2",
		"icon":           snap.IconURL,
		"type":           "app",
		"vendor":         "",
		"installed_size": int64(-1),
		"download_size":  int64(42),
		"description":    "",
	})

}

func (s *lightweightSuite) TestMapRemovedAppNoPart(c *check.C) {
	bag := PartBagByName("foo", "baz")
	m := bag.Map(nil)
	c.Check(m, check.DeepEquals, map[string]interface{}{
		"name":           "foo",
		"origin":         "baz",
		"status":         "removed",
		"version":        "0.8",
		"icon":           "",
		"type":           "app",
		"vendor":         "",
		"installed_size": int64(-1),
		"download_size":  int64(-1),
		"description":    "",
	})
}

func (s *lightweightSuite) TestMapInactiveGadgetNoPart(c *check.C) {
	bag := PartBagByName("a-gadget", "canonical")
	m := bag.Map(nil)
	c.Check(m["installed_size"], check.FitsTypeOf, int64(0))
	delete(m, "installed_size")
	c.Check(m, check.DeepEquals, map[string]interface{}{
		"name":          "a-gadget",
		"origin":        "sideload", // best guess
		"status":        "installed",
		"version":       "3",
		"icon":          filepath.Join(s.d, "snaps", "a-gadget", "3", "meta/icon.png"),
		"type":          "gadget",
		"vendor":        "",
		"download_size": int64(-1),
		"description":   "",
	})
}

func (s *lightweightSuite) TestLoadBadApp(c *check.C) {
	s.MkRemoved(c, "quux.blah", "1")
	// an unparsable snap.yaml:
	c.Check(os.MkdirAll(filepath.Join(dirs.SnapSnapsDir, "quux.blah", "1", "meta", "snap.yaml"), 0755), check.IsNil)

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
	c.Check(bag.Type, check.Equals, snap.TypeFramework)
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
	c.Check(bag0.Type, check.Equals, snap.TypeApp)
	c.Check(bag0.ActiveIndex(), check.Equals, 0)

	c.Check(bag1.QualifiedName(), check.Equals, "foo.baz")
	c.Check(bag1.Versions, check.DeepEquals, []string{"0.8"})
	c.Check(bag1.Type, check.Equals, snap.TypeApp)
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

func (s *lightweightSuite) TestLoadGadget(c *check.C) {
	gadget := PartBagByName("a-gadget", "whatever")
	c.Assert(gadget, check.NotNil)
	c.Check(gadget.Versions, check.DeepEquals, []string{"3"})
	c.Check(gadget.Type, check.Equals, snap.TypeGadget)

	c.Check(gadget.IsInstalled(0), check.Equals, true)
	c.Check(gadget.ActiveIndex(), check.Equals, -1)
	p, err := gadget.Load(0)
	c.Check(err, check.IsNil)
	c.Check(p, check.FitsTypeOf, new(snappy.SnapPart))
	c.Check(p.Version(), check.Equals, "3")
}

type mockrepo struct{ p snappy.Part }

func (r mockrepo) All() ([]snappy.Part, error) {
	return []snappy.Part{r.p}, nil
}

func (s *lightweightSuite) TestAll(c *check.C) {
	all := AllPartBags()

	type expectedT struct {
		typ  snap.Type
		idx  int
		inst bool
	}

	expected := map[string]expectedT{
		"foo.bar":  {typ: snap.TypeApp, idx: 0, inst: true},
		"foo.baz":  {typ: snap.TypeApp, idx: -1, inst: false},
		"fmk":      {typ: snap.TypeFramework, idx: 1, inst: true},
		"fmk2":     {typ: snap.TypeFramework, idx: -1, inst: false},
		"a-gadget": {typ: snap.TypeGadget, idx: -1, inst: true},
	}

	for k, x := range expected {
		c.Assert(all[k], check.NotNil, check.Commentf(k))
		c.Check(all[k].Type, check.Equals, x.typ, check.Commentf(k))
		c.Check(all[k].ActiveIndex(), check.Equals, x.idx, check.Commentf(k))
	}

	c.Check(all, check.HasLen, len(expected))
}
