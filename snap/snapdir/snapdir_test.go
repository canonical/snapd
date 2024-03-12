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

package snapdir_test

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapdir"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type SnapdirTestSuite struct {
}

var _ = Suite(&SnapdirTestSuite{})

func (s *SnapdirTestSuite) TestIsSnapDir(c *C) {
	d := c.MkDir()
	err := os.MkdirAll(filepath.Join(d, "meta"), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(d, "meta/snap.yaml"), nil, 0644)
	c.Assert(err, IsNil)

	c.Check(snapdir.IsSnapDir(d), Equals, true)
}

func (s *SnapdirTestSuite) TestNotIsSnapDir(c *C) {
	c.Check(snapdir.IsSnapDir("/not-existent"), Equals, false)
	c.Check(snapdir.IsSnapDir("/dev/null"), Equals, false)
	c.Check(snapdir.IsSnapDir(c.MkDir()), Equals, false)
}

func (s *SnapdirTestSuite) TestReadFile(c *C) {
	d := c.MkDir()
	needle := []byte(`stuff`)
	err := os.WriteFile(filepath.Join(d, "foo"), needle, 0644)
	c.Assert(err, IsNil)

	sn := snapdir.New(d)
	content, err := sn.ReadFile("foo")
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, needle)
}

func (s *SnapdirTestSuite) TestRandomAccessFile(c *C) {
	d := c.MkDir()
	needle := []byte(`stuff`)
	err := os.WriteFile(filepath.Join(d, "foo"), needle, 0644)
	c.Assert(err, IsNil)

	sn := snapdir.New(d)
	r, err := sn.RandomAccessFile("foo")
	c.Assert(err, IsNil)
	defer r.Close()

	c.Assert(r.Size(), Equals, int64(5))

	b := make([]byte, 2)
	n, err := r.ReadAt(b, 2)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 2)
	c.Check(string(b), Equals, "uf")
}

func (s *SnapdirTestSuite) TestListDir(c *C) {
	d := c.MkDir()

	err := os.MkdirAll(filepath.Join(d, "test"), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(d, "test", "test1"), nil, 0644)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(d, "test", "test2"), nil, 0644)
	c.Assert(err, IsNil)

	sn := snapdir.New(d)
	fileNames, err := sn.ListDir("test")
	c.Assert(err, IsNil)
	c.Assert(fileNames, HasLen, 2)
	c.Check(fileNames[0], Equals, "test1")
	c.Check(fileNames[1], Equals, "test2")
}

func (s *SnapdirTestSuite) TestInstall(c *C) {
	tryBaseDir := c.MkDir()
	sn := snapdir.New(tryBaseDir)

	varLibSnapd := c.MkDir()
	targetPath := filepath.Join(varLibSnapd, "foo_1.0.snap")
	didNothing, err := sn.Install(targetPath, "unused-mount-dir", nil)
	c.Assert(err, IsNil)
	c.Check(didNothing, Equals, false)
	symlinkTarget, err := filepath.EvalSymlinks(targetPath)
	c.Assert(err, IsNil)
	c.Assert(symlinkTarget, Equals, tryBaseDir)
}

func (s *SnapdirTestSuite) TestInstallMustNotCrossDevices(c *C) {
	tryBaseDir := c.MkDir()
	sn := snapdir.New(tryBaseDir)

	varLibSnapd := c.MkDir()
	targetPath := filepath.Join(varLibSnapd, "foo_1.0.snap")
	didNothing, err := sn.Install(targetPath, "unused-mount-dir", &snap.InstallOptions{MustNotCrossDevices: true})
	c.Assert(err, IsNil)
	c.Check(didNothing, Equals, false)
	// TODO:UC20: fix this test when snapdir Install() understands/does
	//            something with opts.MustNotCrossDevices
	c.Check(osutil.IsSymlink(targetPath), Equals, true)
}

func walkEqual(tryBaseDir, sub string, c *C) {
	fpw := map[string]os.FileInfo{}
	filepath.Walk(filepath.Join(tryBaseDir, sub), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		path, err = filepath.Rel(tryBaseDir, path)
		if err != nil {
			return err
		}
		fpw[path] = info
		return nil
	})

	sdw := map[string]os.FileInfo{}
	sn := snapdir.New(tryBaseDir)
	sn.Walk(sub, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		sdw[path] = info
		return nil
	})

	for k, v := range fpw {
		c.Check(os.SameFile(sdw[k], v), Equals, true, Commentf(k))
	}

	for k, v := range sdw {
		c.Check(os.SameFile(fpw[k], v), Equals, true, Commentf(k))
	}
}

func (s *SnapdirTestSuite) TestWalk(c *C) {
	// probably already done elsewhere, but just in case
	rand.Seed(time.Now().UTC().UnixNano())

	// https://en.wikipedia.org/wiki/Metasyntactic_variable
	ns := []string{
		"foobar", "foo", "bar", "baz", "qux", "quux", "quuz", "corge",
		"grault", "garply", "waldo", "fred", "plugh", "xyzzy", "thud",
		"wibble", "wobble", "wubble", "flob", "blep", "blah", "boop",
	}
	p := 1.0 / float32(len(ns))

	var f func(string, int)
	f = func(d string, n int) {
		for _, b := range ns {
			d1 := filepath.Join(d, fmt.Sprintf("%s%d", b, n))
			c.Assert(os.Mkdir(d1, 0755), IsNil)
			if n < 20 && rand.Float32() < p {
				f(d1, n+1)
			}
		}
	}

	subs := make([]string, len(ns)+3)
	// three ways of saying the same thing ¯\_(ツ)_/¯
	copy(subs, []string{"/", ".", ""})
	copy(subs[3:], ns)
	for i := 0; i < 10; i++ {
		tryBaseDir := c.MkDir()
		f(tryBaseDir, 1)

		for _, sub := range subs {
			walkEqual(tryBaseDir, sub, c)
		}

	}
}
