package partition

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "launchpad.net/gocheck"
)

const fakeUbootEnvData = `
`

func (s *PartitionTestSuite) makeFakeUbootEnv(c *C) {
	bootloaderUbootDir = filepath.Join(s.tempdir, "boot", "uboot")
	err := os.MkdirAll(bootloaderUbootDir, 0755)
	c.Assert(err, IsNil)
	// this file just needs to exist
	bootloaderUbootConfigFile = filepath.Join(bootloaderUbootDir, "uEnv.txt")
	err = ioutil.WriteFile(bootloaderUbootConfigFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	bootloaderUbootEnvFile = filepath.Join(bootloaderUbootDir, "uEnv.txt")
	err = ioutil.WriteFile(bootloaderUbootEnvFile, []byte(fakeUbootEnvData), 0644)
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

func (s *PartitionTestSuite) TestUbootToggleRootFS(c *C) {
	s.makeFakeUbootEnv(c)

	partition := New()
	u := NewUboot(partition)
	c.Assert(u, NotNil)
	err := u.ToggleRootFS()
	c.Assert(err, IsNil)

	nextBoot, err := u.GetBootVar("snappy_ab")
	c.Assert(err, IsNil)
	c.Assert(nextBoot, Equals, "system-b")
}
