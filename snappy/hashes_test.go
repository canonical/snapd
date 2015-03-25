package snappy

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "launchpad.net/gocheck"
)

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
  mode: "020000000755"
- name: bin/bar
  size: 4
  sha512: cc06808cbbee0510331aa97974132e8dc296aeb795be229d064bae784b0a87a5cf4281d82e8c99271b75db2148f08a026c1a60ed9cabdb8cac6d24242dac4063
  mode: "0644"
- name: broken-link
  mode: "01000000777"
- name: foo
  size: 0
  sha512: cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e
  mode: "0644"
`)
}
