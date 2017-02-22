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

func (s *buildIDSuite) TestString(c *C) {
	id1 := osutil.BuildID([]byte{0xef, 0xbf, 0xc, 0xe8, 0xdd, 0x96, 0x17, 0xc8, 0x90, 0xa0, 0x54, 0x7c, 0xe5, 0xa1, 0xa6, 0x7, 0x3f, 0x58, 0x67, 0xaf})
	c.Assert(id1.String(), Equals, "BuildID[sha1]=efbf0ce8dd9617c890a0547ce5a1a6073f5867af")

	id2 := osutil.BuildID([]byte{0xde, 0xad, 0xbe, 0xef})
	c.Assert(id2.String(), Equals, "BuildID[???]=deadbeef")
}

func buildID(c *C, fname string) string {
	output, err := exec.Command("file", fname).CombinedOutput()
	c.Assert(err, IsNil)

	re := regexp.MustCompile(`(BuildID\[.*\]=[a-f0-9]+)`)
	matches := re.FindStringSubmatch(string(output))
	c.Assert(matches, HasLen, 2)

	return matches[1]
}

func (s *buildIDSuite) TestGetBuildID(c *C) {
	for _, fname := range []string{"/bin/true", "/bin/false"} {

		id, err := osutil.GetBuildID(fname)
		c.Assert(err, IsNil)
		c.Assert(id.String(), Equals, buildID(c, fname), Commentf("executable: %s", fname))
	}
}

func (s *buildIDSuite) TestGetBuildIDNoID(c *C) {
	stripedTruth := filepath.Join(c.MkDir(), "true")
	osutil.CopyFile("/bin/true", stripedTruth, 0)
	output, err := exec.Command("strip", "-R", ".note.gnu.build-id", stripedTruth).CombinedOutput()
	c.Assert(string(output), Equals, "")
	c.Assert(err, IsNil)

	id, err := osutil.GetBuildID(stripedTruth)
	c.Assert(err, Equals, osutil.ErrNoBuildID)
	c.Assert(id, IsNil)
}

func (s *buildIDSuite) TestGetBuildIDmd5(c *C) {
	if !osutil.FileExists("/usr/bin/gcc") {
		c.Skip("No gcc found")
	}

	md5Truth := filepath.Join(c.MkDir(), "true")
	err := ioutil.WriteFile(md5Truth+".c", []byte(`int main(){return 0;}`), 0644)
	c.Assert(err, IsNil)
	output, err := exec.Command("gcc", "-Wl,-build-id=md5", "-xc", md5Truth+".c", "-o", md5Truth).CombinedOutput()
	c.Assert(string(output), Equals, "")
	c.Assert(err, IsNil)

	id, err := osutil.GetBuildID(md5Truth)
	c.Assert(err, IsNil)
	c.Assert(id.String(), Equals, buildID(c, md5Truth))
}
