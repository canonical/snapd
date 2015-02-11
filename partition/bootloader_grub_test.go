package partition

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "launchpad.net/gocheck"
)

func (s *PartitionTestSuite) makeFakeGrubEnv(c *C) {
	bootloaderGrubDir = filepath.Join(s.tempdir, "boot", "grub")
	err := os.MkdirAll(bootloaderGrubDir, 0755)
	c.Assert(err, IsNil)
	// this file just needs to exist
	bootloaderGrubConfigFile := filepath.Join(bootloaderGrubDir, "grub.cfg")
	err = ioutil.WriteFile(bootloaderGrubConfigFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	bootloaderGrubEnvFile := filepath.Join(bootloaderGrubDir, "grubenv")
	err = ioutil.WriteFile(bootloaderGrubEnvFile, []byte(""), 0644)
	c.Assert(err, IsNil)
}

func (s *PartitionTestSuite) TestNewGrubNoGrubReturnsNil(c *C) {
	bootloaderGrubConfigFile = "no-such-dir"

	partition := New()
	g := NewGrub(partition)
	c.Assert(g, IsNil)
}

func (s *PartitionTestSuite) TestNewGrub(c *C) {
	s.makeFakeGrubEnv(c)

	partition := New()
	g := NewGrub(partition)
	c.Assert(g, NotNil)
}
