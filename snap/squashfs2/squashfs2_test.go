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

package squashfs2_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/snap/squashfs2"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type squashfsSuite struct {
	testutil.BaseTest
}

var _ = Suite(&squashfsSuite{})

func makeSnap(c *C, manifest, data string) *squashfs.Snap {
	cur, _ := os.Getwd()
	return makeSnapInDir(c, cur, manifest, data)
}

func makeSnapContents(c *C, manifest, data string) string {
	tmp := c.MkDir()
	err := os.MkdirAll(filepath.Join(tmp, "meta", "hooks", "dir"), 0755)
	c.Assert(err, IsNil)

	// our regular snap.yaml
	err = ioutil.WriteFile(filepath.Join(tmp, "meta", "snap.yaml"), []byte(manifest), 0644)
	c.Assert(err, IsNil)

	// some hooks
	err = ioutil.WriteFile(filepath.Join(tmp, "meta", "hooks", "foo-hook"), nil, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(tmp, "meta", "hooks", "bar-hook"), nil, 0755)
	c.Assert(err, IsNil)
	// And a file in another directory in there, just for testing (not a valid
	// hook)
	err = ioutil.WriteFile(filepath.Join(tmp, "meta", "hooks", "dir", "baz"), nil, 0755)
	c.Assert(err, IsNil)

	// some empty directories
	err = os.MkdirAll(filepath.Join(tmp, "food", "bard", "bazd"), 0755)
	c.Assert(err, IsNil)

	// some data
	err = ioutil.WriteFile(filepath.Join(tmp, "data.bin"), []byte(data), 0644)
	c.Assert(err, IsNil)

	return tmp
}

func makeSnapInDir(c *C, dir, manifest, data string) *squashfs.Snap {
	snapType := "app"
	var m struct {
		Type string `yaml:"type"`
	}
	if err := yaml.Unmarshal([]byte(manifest), &m); err == nil && m.Type != "" {
		snapType = m.Type
	}

	tmp := makeSnapContents(c, manifest, data)
	// build it
	sn := squashfs.New(filepath.Join(dir, "foo.snap"))
	err := sn.Build(tmp, &squashfs.BuildOpts{SnapType: snapType})
	c.Assert(err, IsNil)

	return sn
}

func (s *squashfsSuite) SetUpTest(c *C) {
	d := c.MkDir()
	dirs.SetRootDir(d)
	err := os.Chdir(d)
	c.Assert(err, IsNil)

	restore := osutil.MockMountInfo("")
	s.AddCleanup(restore)
}

func (s *squashfsSuite) TestCanReadFromSquashFS(c *C) {
	sn := makeSnap(c, "name: test", "")
	sfs, err := squashfs2.SquashFsOpen(sn.Path())
	c.Assert(err, IsNil)
	c.Assert(sfs, NotNil)
	_, err = sfs.ReadFile("meta/snap.yaml")
	c.Assert(err, IsNil)
	defer sfs.Close()
}
