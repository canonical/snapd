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
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapdir"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type SnapdirTestSuite struct{}

var _ = Suite(&SnapdirTestSuite{})

func (s *SnapdirTestSuite) TestIsSnapDir(c *C) {
	d := c.MkDir()
	mylog.Check(os.MkdirAll(filepath.Join(d, "meta"), 0755))

	mylog.Check(os.WriteFile(filepath.Join(d, "meta/snap.yaml"), nil, 0644))


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
	mylog.Check(os.WriteFile(filepath.Join(d, "foo"), needle, 0644))


	sn := snapdir.New(d)
	content := mylog.Check2(sn.ReadFile("foo"))

	c.Assert(content, DeepEquals, needle)
}

func (s *SnapdirTestSuite) TestReadlink(c *C) {
	d := c.MkDir()
	c.Assert(os.Symlink("target", filepath.Join(d, "foo")), IsNil)

	sn := snapdir.New(d)
	target := mylog.Check2(sn.ReadLink("foo"))

	c.Assert(target, DeepEquals, "target")
}

func (s *SnapdirTestSuite) TestLstat(c *C) {
	d := c.MkDir()
	c.Assert(os.Symlink("target", filepath.Join(d, "symlink")), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d, "meta"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(d, "meta/snap.yaml"), nil, 0644), IsNil)

	sn := snapdir.New(d)
	for _, file := range []string{
		"symlink",
		"meta",
		"meta/snap.yaml",
	} {
		expectedInfo := mylog.Check2(os.Lstat(filepath.Join(d, file)))

		info := mylog.Check2(sn.Lstat(file))


		c.Check(info.Name(), Equals, expectedInfo.Name())
		c.Check(info.Mode(), Equals, expectedInfo.Mode())
		c.Check(info.Size(), Equals, expectedInfo.Size())
	}
}

func (s *SnapdirTestSuite) TestLstatErrNotExist(c *C) {
	sn := snapdir.New(c.MkDir())
	_ := mylog.Check2(sn.Lstat("meta/non-existent"))
	c.Check(errors.Is(err, os.ErrNotExist), Equals, true)
}

func (s *SnapdirTestSuite) TestRandomAccessFile(c *C) {
	d := c.MkDir()
	needle := []byte(`stuff`)
	mylog.Check(os.WriteFile(filepath.Join(d, "foo"), needle, 0644))


	sn := snapdir.New(d)
	r := mylog.Check2(sn.RandomAccessFile("foo"))

	defer r.Close()

	c.Assert(r.Size(), Equals, int64(5))

	b := make([]byte, 2)
	n := mylog.Check2(r.ReadAt(b, 2))

	c.Assert(n, Equals, 2)
	c.Check(string(b), Equals, "uf")
}

func (s *SnapdirTestSuite) TestListDir(c *C) {
	d := c.MkDir()
	mylog.Check(os.MkdirAll(filepath.Join(d, "test"), 0755))

	mylog.Check(os.WriteFile(filepath.Join(d, "test", "test1"), nil, 0644))

	mylog.Check(os.WriteFile(filepath.Join(d, "test", "test2"), nil, 0644))


	sn := snapdir.New(d)
	fileNames := mylog.Check2(sn.ListDir("test"))

	c.Assert(fileNames, HasLen, 2)
	c.Check(fileNames[0], Equals, "test1")
	c.Check(fileNames[1], Equals, "test2")
}

func (s *SnapdirTestSuite) TestInstall(c *C) {
	tryBaseDir := c.MkDir()
	sn := snapdir.New(tryBaseDir)

	varLibSnapd := c.MkDir()
	targetPath := filepath.Join(varLibSnapd, "foo_1.0.snap")
	didNothing := mylog.Check2(sn.Install(targetPath, "unused-mount-dir", nil))

	c.Check(didNothing, Equals, false)
	symlinkTarget := mylog.Check2(filepath.EvalSymlinks(targetPath))

	c.Assert(symlinkTarget, Equals, tryBaseDir)
}

func (s *SnapdirTestSuite) TestInstallMustNotCrossDevices(c *C) {
	tryBaseDir := c.MkDir()
	sn := snapdir.New(tryBaseDir)

	varLibSnapd := c.MkDir()
	targetPath := filepath.Join(varLibSnapd, "foo_1.0.snap")
	didNothing := mylog.Check2(sn.Install(targetPath, "unused-mount-dir", &snap.InstallOptions{MustNotCrossDevices: true}))

	c.Check(didNothing, Equals, false)
	// TODO:UC20: fix this test when snapdir Install() understands/does
	//            something with opts.MustNotCrossDevices
	c.Check(osutil.IsSymlink(targetPath), Equals, true)
}

func walkEqual(tryBaseDir, sub string, c *C) {
	fpw := map[string]os.FileInfo{}
	filepath.Walk(filepath.Join(tryBaseDir, sub), func(path string, info os.FileInfo, err error) error {
		path = mylog.Check2(filepath.Rel(tryBaseDir, path))

		fpw[path] = info
		return nil
	})

	sdw := map[string]os.FileInfo{}
	sn := snapdir.New(tryBaseDir)
	sn.Walk(sub, func(path string, info os.FileInfo, err error) error {
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
