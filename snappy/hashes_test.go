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

package snappy

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"

	. "launchpad.net/gocheck"
)

var fileHashYaml = `name: foo
size: 10
mode: drw-r--r--
`

var size = int64(10)
var fileHashStruct = fileHash{
	Name: "foo",
	Size: &size,
	Mode: newYamlFileMode(0644 | os.ModeDir),
}

func (s *SnapTestSuite) TestHashesYamlUnmarshal(c *C) {
	var h fileHash
	err := yaml.Unmarshal([]byte(fileHashYaml), &h)
	c.Assert(err, IsNil)

	c.Assert(h, DeepEquals, fileHashStruct)
}

func (s *SnapTestSuite) TestHashesYamlMarshal(c *C) {
	y, err := yaml.Marshal(&fileHashStruct)
	c.Assert(err, IsNil)

	c.Assert(string(y), Equals, fileHashYaml)
}

func (s *SnapTestSuite) TestBuildCreateDebianHashesSimple(c *C) {
	tempdir := c.MkDir()

	// debian dir is ignored
	err := os.MkdirAll(filepath.Join(tempdir, "DEBIAN"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(tempdir, "DEBIAN", "bar"), []byte(""), 0644)

	// regular files are looked at
	err = ioutil.WriteFile(filepath.Join(tempdir, "foo"), []byte(""), 0644)
	c.Assert(err, IsNil)

	// normal subdirs are supported
	err = os.MkdirAll(filepath.Join(tempdir, "bin"), 0755)
	err = ioutil.WriteFile(filepath.Join(tempdir, "bin", "bar"), []byte("bar\n"), 0644)
	c.Assert(err, IsNil)

	// symlinks are supported
	err = os.Symlink("/dsafdsafsadf", filepath.Join(tempdir, "broken-link"))
	c.Assert(err, IsNil)

	// data.tar.gz
	dataTar := filepath.Join(c.MkDir(), "data.tar.gz")
	err = ioutil.WriteFile(dataTar, []byte(""), 0644)

	// TEST write the hashes
	err = writeHashes(tempdir, dataTar)
	c.Assert(err, IsNil)

	// check content
	content, err := ioutil.ReadFile(filepath.Join(tempdir, "DEBIAN", "hashes.yaml"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, `archive-sha512: cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e
files:
- name: bin
  mode: drwxr-xr-x
- name: bin/bar
  size: 4
  sha512: cc06808cbbee0510331aa97974132e8dc296aeb795be229d064bae784b0a87a5cf4281d82e8c99271b75db2148f08a026c1a60ed9cabdb8cac6d24242dac4063
  mode: frw-r--r--
- name: broken-link
  mode: lrwxrwxrwx
- name: foo
  size: 0
  sha512: cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e
  mode: frw-r--r--
`)
}
