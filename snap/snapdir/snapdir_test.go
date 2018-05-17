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
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snapcore/snapd/snap/snapdir"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type SnapdirTestSuite struct {
}

var _ = Suite(&SnapdirTestSuite{})

func (s *SnapdirTestSuite) TestReadFile(c *C) {
	d := c.MkDir()
	needle := []byte(`stuff`)
	err := ioutil.WriteFile(filepath.Join(d, "foo"), needle, 0644)
	c.Assert(err, IsNil)

	snap := snapdir.New(d)
	content, err := snap.ReadFile("foo")
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, needle)
}

func (s *SnapdirTestSuite) TestListDir(c *C) {
	d := c.MkDir()

	err := os.MkdirAll(filepath.Join(d, "test"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(d, "test", "test1"), nil, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(d, "test", "test2"), nil, 0644)
	c.Assert(err, IsNil)

	snap := snapdir.New(d)
	fileNames, err := snap.ListDir("test")
	c.Assert(err, IsNil)
	c.Assert(fileNames, HasLen, 2)
	c.Check(fileNames[0], Equals, "test1")
	c.Check(fileNames[1], Equals, "test2")
}

func (s *SnapdirTestSuite) TestInstall(c *C) {
	tryBaseDir := c.MkDir()
	snap := snapdir.New(tryBaseDir)

	varLibSnapd := c.MkDir()
	targetPath := filepath.Join(varLibSnapd, "foo_1.0.snap")
	err := snap.Install(targetPath, "unused-mount-dir")
	c.Assert(err, IsNil)
	symlinkTarget, err := filepath.EvalSymlinks(targetPath)
	c.Assert(err, IsNil)
	c.Assert(symlinkTarget, Equals, tryBaseDir)
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
	snap := snapdir.New(tryBaseDir)
	snap.Walk(sub, func(path string, info os.FileInfo, err error) error {
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
