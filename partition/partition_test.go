package partition

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"

	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

// partition specific testsuite
type PartitionTestSuite struct {
}

var _ = Suite(&PartitionTestSuite{})

func makeHardwareYaml() (tmp *os.File, err error) {
	tmp, err = ioutil.TempFile("", "hw-")
	if err != nil {
		return tmp, err
	}
	tmp.Write([]byte(`
kernel: assets/vmlinuz
initrd: assets/initrd.img
dtbs: assets/dtbs
partition-layout: system-AB
bootloader: uboot
`))
	return tmp, err
}

func (s *PartitionTestSuite) TestHardwareSpec(c *C) {
	p := New()
	c.Assert(p, NotNil)

	tmp, err := makeHardwareYaml()
	defer func() {
		os.Remove(tmp.Name())
	}()

	p.hardwareSpecFile = tmp.Name()
	hw, err := p.hardwareSpec()
	c.Assert(err, IsNil)
	c.Assert(hw.Kernel, Equals, "assets/vmlinuz")
	c.Assert(hw.Initrd, Equals, "assets/initrd.img")
	c.Assert(hw.DtbDir, Equals, "assets/dtbs")
	c.Assert(hw.PartitionLayout, Equals, "system-AB")
	c.Assert(hw.Bootloader, Equals, "uboot")
}

func mockRunLsblkDualSnappy() (output []string, err error) {
	dualData := `
NAME="sda" LABEL="" PKNAME="" MOUNTPOINT=""
NAME="sda1" LABEL="" PKNAME="sda" MOUNTPOINT=""
NAME="sda2" LABEL="system-boot" PKNAME="sda" MOUNTPOINT=""
NAME="sda3" LABEL="system-a" PKNAME="sda" MOUNTPOINT="/"
NAME="sda4" LABEL="system-b" PKNAME="sda" MOUNTPOINT=""
NAME="sda5" LABEL="writable" PKNAME="sda" MOUNTPOINT="/writable"
NAME="sr0" LABEL="" PKNAME="" MOUNTPOINT=""
`
	return strings.Split(dualData, "\n"), err
}

func (s *PartitionTestSuite) TestSnappyDualRoot(c *C) {
	runLsblk = mockRunLsblkDualSnappy

	p := New()
	c.Assert(p.dualRootPartitions(), Equals, true)
	c.Assert(p.singleRootPartition(), Equals, false)

	rootPartitions := p.rootPartitions()
	c.Assert(rootPartitions[0].name, Equals, "system-a")
	c.Assert(rootPartitions[0].device, Equals, "/dev/sda3")
	c.Assert(rootPartitions[0].parentName, Equals, "/dev/sda")
	c.Assert(rootPartitions[1].name, Equals, "system-b")
	c.Assert(rootPartitions[1].device, Equals, "/dev/sda4")
	c.Assert(rootPartitions[1].parentName, Equals, "/dev/sda")

	wp := p.writablePartition()
	c.Assert(wp.name, Equals, "writable")
	c.Assert(wp.device, Equals, "/dev/sda5")
	c.Assert(wp.parentName, Equals, "/dev/sda")

	boot := p.bootPartition()
	c.Assert(boot.name, Equals, "system-boot")
	c.Assert(boot.device, Equals, "/dev/sda2")
	c.Assert(boot.parentName, Equals, "/dev/sda")

	root := p.rootPartition()
	c.Assert(root.name, Equals, "system-a")
	c.Assert(root.device, Equals, "/dev/sda3")
	c.Assert(root.parentName, Equals, "/dev/sda")

	other := p.otherRootPartition()
	c.Assert(other.name, Equals, "system-b")
	c.Assert(other.device, Equals, "/dev/sda4")
	c.Assert(other.parentName, Equals, "/dev/sda")
}

func mockRunLsblkSingleRootSnappy() (output []string, err error) {
	dualData := `
NAME="sda" LABEL="" PKNAME="" MOUNTPOINT=""
NAME="sda1" LABEL="" PKNAME="sda" MOUNTPOINT=""
NAME="sda2" LABEL="system-boot" PKNAME="sda" MOUNTPOINT=""
NAME="sda3" LABEL="system-a" PKNAME="sda" MOUNTPOINT="/"
NAME="sda5" LABEL="writable" PKNAME="sda" MOUNTPOINT="/writable"
`
	return strings.Split(dualData, "\n"), err
}
func (s *PartitionTestSuite) TestSnappySingleRoot(c *C) {
	runLsblk = mockRunLsblkSingleRootSnappy

	p := New()
	c.Assert(p.dualRootPartitions(), Equals, false)
	c.Assert(p.singleRootPartition(), Equals, true)

	root := p.rootPartition()
	c.Assert(root.name, Equals, "system-a")
	c.Assert(root.device, Equals, "/dev/sda3")
	c.Assert(root.parentName, Equals, "/dev/sda")

	other := p.otherRootPartition()
	c.Assert(other, IsNil)

	rootPartitions := p.rootPartitions()
	c.Assert(&rootPartitions[0], DeepEquals, root)
}

func mockRunCommand(args []string) (err error) {
	return err
}

func (s *PartitionTestSuite) TestMountUnmountTracking(c *C) {
	runLsblk = mockRunLsblkDualSnappy

	// FIXME: there should be a generic
	//        mockFunc(func) (restorer func())
	savedRunCommand := runCommand
	defer func() {
		runCommand = savedRunCommand
	}()
	runCommand = mockRunCommand

	p := New()

	p.mountOtherRootfs(false)
	c.Assert(mounts, DeepEquals, []string{p.MountTarget})
	p.unmountOtherRootfs()
	c.Assert(mounts, DeepEquals, []string{})
}
