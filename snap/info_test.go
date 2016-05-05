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

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/snaptest"
	"github.com/ubuntu-core/snappy/snap/squashfs"
)

type infoSuite struct{}

var _ = Suite(&infoSuite{})

func (s *infoSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *infoSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *infoSuite) TestSideInfoOverrides(c *C) {
	info := &snap.Info{
		SuggestedName:       "name",
		OriginalSummary:     "summary",
		OriginalDescription: "desc",
	}

	info.SideInfo = snap.SideInfo{
		OfficialName:      "newname",
		EditedSummary:     "fixed summary",
		EditedDescription: "fixed desc",
		Revision:          1,
		SnapID:            "snapidsnapidsnapidsnapidsnapidsn",
	}

	c.Check(info.Name(), Equals, "newname")
	c.Check(info.Summary(), Equals, "fixed summary")
	c.Check(info.Description(), Equals, "fixed desc")
	c.Check(info.Revision, Equals, 1)
	c.Check(info.SnapID, Equals, "snapidsnapidsnapidsnapidsnapidsn")
}

func (s *infoSuite) TestAppInfoSecurityTag(c *C) {
	appInfo := &snap.AppInfo{Snap: &snap.Info{SuggestedName: "http"}, Name: "GET"}
	c.Check(appInfo.SecurityTag(), Equals, "snap.http.GET")
}

func (s *infoSuite) TestAppInfoWrapperPath(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`name: foo
apps:
   foo:
   bar:
`))
	c.Assert(err, IsNil)

	c.Check(info.Apps["bar"].WrapperPath(), Equals, filepath.Join(dirs.SnapBinariesDir, "foo.bar"))
	c.Check(info.Apps["foo"].WrapperPath(), Equals, filepath.Join(dirs.SnapBinariesDir, "foo"))
}

func (s *infoSuite) TestAppInfoLauncherCommand(c *C) {
	dirs.SetRootDir("")

	info, err := snap.InfoFromSnapYaml([]byte(`name: foo
apps:
   foo:
     command: foo-bin
   bar:
     command: bar-bin -x
`))
	c.Assert(err, IsNil)
	info.Revision = 42

	c.Check(info.Apps["bar"].LauncherCommand(), Equals, "/usr/bin/ubuntu-core-launcher snap.foo.bar snap.foo.bar /snap/foo/42/bar-bin -x")
	c.Check(info.Apps["foo"].LauncherCommand(), Equals, "/usr/bin/ubuntu-core-launcher snap.foo.foo snap.foo.foo /snap/foo/42/foo-bin")
}

const sampleYaml = `
name: sample
version: 1
apps:
 app:
   command: foo
`

func (s *infoSuite) TestReadInfo(c *C) {
	si := &snap.SideInfo{Revision: 42, EditedSummary: "esummary"}

	snapInfo1 := snaptest.MockSnap(c, sampleYaml, si)

	snapInfo2, err := snap.ReadInfo("sample", si)
	c.Assert(err, IsNil)

	c.Check(snapInfo2.Name(), Equals, "sample")
	c.Check(snapInfo2.Revision, Equals, 42)
	c.Check(snapInfo2.Summary(), Equals, "esummary")

	c.Check(snapInfo2.Apps["app"].Command, Equals, "foo")

	c.Check(snapInfo2, DeepEquals, snapInfo1)
}

func makeTestSnap(c *C, yaml string) string {
	tmp := c.MkDir()
	snapSource := filepath.Join(tmp, "snapsrc")

	err := os.MkdirAll(filepath.Join(snapSource, "meta"), 0755)

	// our regular snap.yaml
	err = ioutil.WriteFile(filepath.Join(snapSource, "meta", "snap.yaml"), []byte(yaml), 0644)
	c.Assert(err, IsNil)

	dest := filepath.Join(tmp, "foo.snap")
	snap := squashfs.New(dest)
	err = snap.Build(snapSource)
	c.Assert(err, IsNil)

	return dest
}

func (s *infoSuite) TestReadInfoFromSnapFile(c *C) {
	yaml := `name: foo
version: 1.0
type: app`
	snapPath := makeTestSnap(c, yaml)

	snapf, err := snap.Open(snapPath)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, IsNil)
	c.Check(info.Name(), Equals, "foo")
	c.Check(info.Version, Equals, "1.0")
	c.Check(info.Type, Equals, snap.TypeApp)
	c.Check(info.Revision, Equals, 0)
}

func (s *infoSuite) TestReadInfoFromSnapFileWithSideInfo(c *C) {
	yaml := `name: foo
version: 1.0
type: app`
	snapPath := makeTestSnap(c, yaml)

	snapf, err := snap.Open(snapPath)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, &snap.SideInfo{
		OfficialName: "baz",
		Revision:     42,
	})
	c.Assert(err, IsNil)
	c.Check(info.Name(), Equals, "baz")
	c.Check(info.Version, Equals, "1.0")
	c.Check(info.Type, Equals, snap.TypeApp)
	c.Check(info.Revision, Equals, 42)
}

func (s *infoSuite) TestReadInfoFromSnapFileValidates(c *C) {
	yaml := `name: foo.bar
version: 1.0
type: app`
	snapPath := makeTestSnap(c, yaml)

	snapf, err := snap.Open(snapPath)
	c.Assert(err, IsNil)

	_, err = snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, ErrorMatches, "invalid snap name.*")
}
