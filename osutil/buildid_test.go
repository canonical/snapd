// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package osutil_test

import (
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/snapcore/snapd/osutil"

	. "gopkg.in/check.v1"
)

type buildIDSuite struct{}

var _ = Suite(&buildIDSuite{})

var truePath = osutil.LookPathDefault("true", "/bin/true")
var falsePath = osutil.LookPathDefault("false", "/bin/false")
var gccPath = osutil.LookPathDefault("gcc", "/usr/bin/gcc")

func buildID(c *C, fname string) string {
	output, err := exec.Command("file", fname).CombinedOutput()
	c.Assert(err, IsNil)

	re := regexp.MustCompile(`BuildID\[.*\]=([a-f0-9]+)`)
	matches := re.FindStringSubmatch(string(output))
	c.Assert(matches, HasLen, 2)

	return matches[1]
}

func (s *buildIDSuite) TestReadBuildID(c *C) {
	for _, fname := range []string{truePath, falsePath} {

		id, err := osutil.ReadBuildID(fname)
		c.Assert(err, IsNil)
		c.Assert(id, Equals, buildID(c, fname), Commentf("executable: %s", fname))
	}
}

func (s *buildIDSuite) TestReadBuildIDNoID(c *C) {
	stripedTruth := filepath.Join(c.MkDir(), "true")
	osutil.CopyFile(truePath, stripedTruth, 0)
	output, err := exec.Command("strip", "-R", ".note.gnu.build-id", stripedTruth).CombinedOutput()
	c.Assert(string(output), Equals, "")
	c.Assert(err, IsNil)

	id, err := osutil.ReadBuildID(stripedTruth)
	c.Assert(err, Equals, osutil.ErrNoBuildID)
	c.Assert(id, Equals, "")
}

func (s *buildIDSuite) TestReadBuildIDmd5(c *C) {
	if !osutil.FileExists(gccPath) {
		c.Skip("No gcc found")
	}

	md5Truth := filepath.Join(c.MkDir(), "true")
	err := ioutil.WriteFile(md5Truth+".c", []byte(`int main(){return 0;}`), 0644)
	c.Assert(err, IsNil)
	output, err := exec.Command(gccPath, "-Wl,-build-id=md5", "-xc", md5Truth+".c", "-o", md5Truth).CombinedOutput()
	c.Assert(string(output), Equals, "")
	c.Assert(err, IsNil)

	id, err := osutil.ReadBuildID(md5Truth)
	c.Assert(err, IsNil)
	c.Assert(id, Equals, buildID(c, md5Truth))
}

func (s *buildIDSuite) TestMyBuildID(c *C) {
	restore := osutil.MockOsReadlink(func(string) (string, error) {
		return truePath, nil
	})
	defer restore()

	id, err := osutil.MyBuildID()
	c.Assert(err, IsNil)
	c.Check(id, Equals, buildID(c, truePath))
}
