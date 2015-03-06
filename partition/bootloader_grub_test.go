package partition

import (
	"fmt"
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
	bootloaderGrubConfigFile = filepath.Join(bootloaderGrubDir, "grub.cfg")
	err = ioutil.WriteFile(bootloaderGrubConfigFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	bootloaderGrubEnvFile = filepath.Join(bootloaderGrubDir, "grubenv")
	err = ioutil.WriteFile(bootloaderGrubEnvFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	// do not run commands for real
	runCommand = mockRunCommandWithCapture
}

func (s *PartitionTestSuite) TestNewGrubNoGrubReturnsNil(c *C) {
	bootloaderGrubConfigFile = "no-such-dir"

	partition := New()
	g := newGrub(partition)
	c.Assert(g, IsNil)
}

func (s *PartitionTestSuite) TestNewGrub(c *C) {
	s.makeFakeGrubEnv(c)

	partition := New()
	g := newGrub(partition)
	c.Assert(g, NotNil)
	c.Assert(g.Name(), Equals, bootloaderNameGrub)
}

func (s *PartitionTestSuite) TestNewGrubSinglePartition(c *C) {
	runLsblk = mockRunLsblkSingleRootSnappy
	s.makeFakeGrubEnv(c)

	partition := New()
	g := newGrub(partition)
	c.Assert(g, IsNil)
}

type singleCommand []string

var allCommands = []singleCommand{}

func mockRunCommandWithCapture(args ...string) (err error) {
	allCommands = append(allCommands, args)
	return nil
}

func (s *PartitionTestSuite) TestToggleRootFS(c *C) {
	s.makeFakeGrubEnv(c)
	allCommands = []singleCommand{}

	partition := New()
	g := newGrub(partition)
	c.Assert(g, NotNil)
	err := g.ToggleRootFS()
	c.Assert(err, IsNil)

	// this is always called
	mp := singleCommand{"/bin/mountpoint", "/writable/cache/system"}
	c.Assert(allCommands[0], DeepEquals, mp)

	expectedGrubInstall := singleCommand{"/usr/sbin/chroot", "/writable/cache/system", bootloaderGrubInstallCmd, "/dev/sda"}
	c.Assert(allCommands[1], DeepEquals, expectedGrubInstall)

	expectedGrubUpdate := singleCommand{"/usr/sbin/chroot", "/writable/cache/system", bootloaderGrubUpdateCmd}
	c.Assert(allCommands[2], DeepEquals, expectedGrubUpdate)

	expectedGrubSet := singleCommand{bootloaderGrubEnvCmd, bootloaderGrubEnvFile, "set", "snappy_mode=try"}
	c.Assert(allCommands[3], DeepEquals, expectedGrubSet)

	// the https://developer.ubuntu.com/en/snappy/porting guide says
	// we always use the short names
	expectedGrubSet = singleCommand{bootloaderGrubEnvCmd, bootloaderGrubEnvFile, "set", "snappy_ab=b"}
	c.Assert(allCommands[4], DeepEquals, expectedGrubSet)

}

func mockGrubEditenvList(cmd ...string) (string, error) {
	mockGrubEditenvOutput := fmt.Sprintf("%s=default", bootloaderBootmodeVar)
	return mockGrubEditenvOutput, nil
}

func (s *PartitionTestSuite) TestGetBootVer(c *C) {
	s.makeFakeGrubEnv(c)
	runCommandWithStdout = mockGrubEditenvList

	partition := New()
	g := newGrub(partition)

	v, err := g.GetBootVar(bootloaderBootmodeVar)
	c.Assert(err, IsNil)
	c.Assert(v, Equals, "default")
}

func (s *PartitionTestSuite) TestGetBootloaderWithGrub(c *C) {
	s.makeFakeGrubEnv(c)
	p := New()
	bootloader, err := getBootloader(p)
	c.Assert(err, IsNil)
	c.Assert(bootloader.Name(), Equals, bootloaderNameGrub)
}
