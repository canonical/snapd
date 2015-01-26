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

func mockRunLsblkDual() (output []string, err error) {
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

func (s *PartitionTestSuite) TestIsDual(c *C) {
	runLsblk = mockRunLsblkDual

	p := New()
	c.Assert(p.DualRootPartitions(), Equals, true)
}
