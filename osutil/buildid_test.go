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
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type buildIDSuite struct {
	testutil.BaseTest
}

var _ = Suite(&buildIDSuite{})

var truePath = osutil.LookPathDefault("true", "/bin/true")
var falsePath = osutil.LookPathDefault("false", "/bin/false")
var gccPath = osutil.LookPathDefault("gcc", "/usr/bin/gcc")

func (s *buildIDSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func buildID(c *C, fname string) string {
	// XXX host's 'file' command may be too old to know about Go BuildID or
	// hexstring GNU BuildID, use with caution
	output, err := exec.Command("file", fname).CombinedOutput()
	c.Assert(err, IsNil)

	c.Logf("file output: %q", string(output))

	// BuildID can look like:
	//  BuildID[sha1]=443877f9ec13c82365478130fc95cb5ff5181912
	//  BuildID[md5/uuid]=ae38cdf243d2111064dfee99dfc30013
	//  Go BuildID=YDAw4RLIEKpyxl90JbFQ/s9mld--03zAIIQ1tGb_5/aL-yPp ...
	re := regexp.MustCompile(`BuildID(\[.*\]|)=([a-zA-Z0-9/_-]+)`)
	matches := re.FindStringSubmatch(string(output))

	c.Assert(matches, HasLen, 3)

	return matches[2]
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
	err := os.WriteFile(md5Truth+".c", []byte(`int main(){return 0;}`), 0644)
	c.Assert(err, IsNil)
	output, err := exec.Command(gccPath, "-Wl,--build-id=md5", "-xc", md5Truth+".c", "-o", md5Truth).CombinedOutput()
	c.Assert(string(output), Equals, "")
	c.Assert(err, IsNil)

	id, err := osutil.ReadBuildID(md5Truth)
	c.Assert(err, IsNil)
	c.Assert(id, Equals, buildID(c, md5Truth))
}

func (s *buildIDSuite) TestReadBuildIDFixedELF(c *C) {
	if !osutil.FileExists(gccPath) {
		c.Skip("No gcc found")
	}

	md5Truth := filepath.Join(c.MkDir(), "true")
	err := os.WriteFile(md5Truth+".c", []byte(`int main(){return 0;}`), 0644)
	c.Assert(err, IsNil)
	output, err := exec.Command(gccPath, "-Wl,--build-id=0xdeadcafe", "-xc", md5Truth+".c", "-o", md5Truth).CombinedOutput()
	c.Assert(string(output), Equals, "")
	c.Assert(err, IsNil)

	id, err := osutil.ReadBuildID(md5Truth)
	c.Assert(err, IsNil)
	// XXX cannot call buildID() as the host's 'file' command may be too old
	// to know about hexstring format of GNU BuildID
	c.Assert(id, Equals, "deadcafe")
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

func (s *buildIDSuite) TestReadBuildGo(c *C) {
	if os.Getenv("DH_GOPKG") != "" {
		// Failure reason is unknown but only reproducible
		// inside the any 21.04+ sbuild/pbuilder build
		// environment during the build (with dh-golang).
		//
		// Not reproducible outside of dpkg-buildpackage.
		c.Skip("This `go build` fails inside the dpkg-buildpackage environment with `loadinternal: cannot find runtime/cgo`")
	}

	tmpdir := c.MkDir()
	goTruth := filepath.Join(tmpdir, "true")
	err := os.WriteFile(goTruth+".go", []byte(`package main; func main(){}`), 0644)
	c.Assert(err, IsNil)
	// force specific Go BuildID
	cmd := exec.Command("go", "build", "-o", goTruth, "-ldflags=-buildid=foobar", goTruth+".go")
	// set custom homedir to ensure tests work in an sbuild environment
	// that force a non-existing homedir
	cmd.Env = append(os.Environ(), "HOME="+tmpdir)
	cmd.Dir = tmpdir
	output, err := cmd.CombinedOutput()
	c.Assert(string(output), Equals, "")
	c.Assert(err, IsNil)

	id, err := osutil.ReadBuildID(goTruth)
	c.Assert(err, IsNil)

	// ReadBuildID returns a hex encoded string, however buildID()
	// returns the "raw" string so we need to decode first
	decoded, err := hex.DecodeString(id)
	c.Assert(err, IsNil)
	// XXX cannot call buildID() as the host's 'file' command may be too old
	// to know about Go BuildID
	c.Assert(string(decoded), Equals, "foobar")
}
