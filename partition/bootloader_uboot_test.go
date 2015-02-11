package partition

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "launchpad.net/gocheck"
)

func (s *PartitionTestSuite) makeFakeUbootEnv(c *C) {
	bootloaderUbootDir = filepath.Join(s.tempdir, "boot", "uboot")
	err := os.MkdirAll(bootloaderUbootDir, 0755)
	c.Assert(err, IsNil)
	// this file just needs to exist
	bootloaderUbootConfigFile = filepath.Join(bootloaderUbootDir, "uEnv.txt")
	err = ioutil.WriteFile(bootloaderUbootConfigFile, []byte(""), 0644)
	c.Assert(err, IsNil)
}

func (s *PartitionTestSuite) TestNewUbootNoUbootReturnsNil(c *C) {
	partition := New()
	u := NewUboot(partition)
	c.Assert(u, IsNil)
}

func (s *PartitionTestSuite) TestNewUboot(c *C) {
	s.makeFakeUbootEnv(c)

	partition := New()
	u := NewUboot(partition)
	c.Assert(u, NotNil)
}
