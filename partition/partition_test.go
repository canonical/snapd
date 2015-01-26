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
NAME="sda" KNAME="sda" MAJ:MIN="8:0" FSTYPE="" MOUNTPOINT="" LABEL="" UUID="" PARTTYPE="" PARTLABEL="" PARTUUID="" PARTFLAGS="" RA="128" RO="0" RM="0" MODEL="QEMU HARDDISK   " SERIAL="" SIZE="18.6G" STATE="running" OWNER="root" GROUP="disk" MODE="brw-rw----" ALIGNMENT="0" MIN-IO="512" OPT-IO="0" PHY-SEC="512" LOG-SEC="512" ROTA="1" SCHED="deadline" RQ-SIZE="128" TYPE="disk" DISC-ALN="0" DISC-GRAN="512B" DISC-MAX="2G" DISC-ZERO="0" WSAME="0B" WWN="" RAND="1" PKNAME="" HCTL="0:0:0:0" TRAN="ata" REV="2   " VENDOR="ATA     "
NAME="sda1" KNAME="sda1" MAJ:MIN="8:1" FSTYPE="" MOUNTPOINT="" LABEL="" UUID="" PARTTYPE="21686148-6449-6e6f-744e-656564454649" PARTLABEL="grub" PARTUUID="0a3121e5-04c0-40ff-931c-570706af9cb7" PARTFLAGS="" RA="128" RO="0" RM="0" MODEL="" SERIAL="" SIZE="4M" STATE="" OWNER="root" GROUP="disk" MODE="brw-rw----" ALIGNMENT="0" MIN-IO="512" OPT-IO="0" PHY-SEC="512" LOG-SEC="512" ROTA="1" SCHED="deadline" RQ-SIZE="128" TYPE="part" DISC-ALN="0" DISC-GRAN="512B" DISC-MAX="2G" DISC-ZERO="0" WSAME="0B" WWN="" RAND="1" PKNAME="sda" HCTL="" TRAN="" REV="" VENDOR=""
NAME="sda2" KNAME="sda2" MAJ:MIN="8:2" FSTYPE="vfat" MOUNTPOINT="" LABEL="system-boot" UUID="2D00-F2E5" PARTTYPE="c12a7328-f81f-11d2-ba4b-00a0c93ec93b" PARTLABEL="system-boot" PARTUUID="7b8baba0-211a-4559-92e4-9c57f66fbb02" PARTFLAGS="" RA="128" RO="0" RM="0" MODEL="" SERIAL="" SIZE="64M" STATE="" OWNER="root" GROUP="disk" MODE="brw-rw----" ALIGNMENT="0" MIN-IO="512" OPT-IO="0" PHY-SEC="512" LOG-SEC="512" ROTA="1" SCHED="deadline" RQ-SIZE="128" TYPE="part" DISC-ALN="0" DISC-GRAN="512B" DISC-MAX="2G" DISC-ZERO="0" WSAME="0B" WWN="" RAND="1" PKNAME="sda" HCTL="" TRAN="" REV="" VENDOR=""
NAME="sda3" KNAME="sda3" MAJ:MIN="8:3" FSTYPE="ext4" MOUNTPOINT="/" LABEL="system-a" UUID="a2607093-da92-4e70-81c9-6811ce7f74c1" PARTTYPE="0fc63daf-8483-4772-8e79-3d69d8477de4" PARTLABEL="system-a" PARTUUID="3c8e4b16-9cc3-49a3-b505-f067fe7c67c7" PARTFLAGS="" RA="128" RO="0" RM="0" MODEL="" SERIAL="" SIZE="1G" STATE="" OWNER="root" GROUP="disk" MODE="brw-rw----" ALIGNMENT="0" MIN-IO="512" OPT-IO="0" PHY-SEC="512" LOG-SEC="512" ROTA="1" SCHED="deadline" RQ-SIZE="128" TYPE="part" DISC-ALN="0" DISC-GRAN="512B" DISC-MAX="2G" DISC-ZERO="0" WSAME="0B" WWN="" RAND="1" PKNAME="sda" HCTL="" TRAN="" REV="" VENDOR=""
NAME="sda4" KNAME="sda4" MAJ:MIN="8:4" FSTYPE="ext4" MOUNTPOINT="" LABEL="system-b" UUID="5e04badd-f04b-46a1-8985-e4f3e55c9b05" PARTTYPE="0fc63daf-8483-4772-8e79-3d69d8477de4" PARTLABEL="system-b" PARTUUID="e1ab1327-0f51-4eb7-860e-26f32ab66ca3" PARTFLAGS="" RA="128" RO="0" RM="0" MODEL="" SERIAL="" SIZE="1G" STATE="" OWNER="root" GROUP="disk" MODE="brw-rw----" ALIGNMENT="0" MIN-IO="512" OPT-IO="0" PHY-SEC="512" LOG-SEC="512" ROTA="1" SCHED="deadline" RQ-SIZE="128" TYPE="part" DISC-ALN="0" DISC-GRAN="512B" DISC-MAX="2G" DISC-ZERO="0" WSAME="0B" WWN="" RAND="1" PKNAME="sda" HCTL="" TRAN="" REV="" VENDOR=""
NAME="sda5" KNAME="sda5" MAJ:MIN="8:5" FSTYPE="ext4" MOUNTPOINT="/writable" LABEL="writable" UUID="dbe57386-5e85-413e-b549-59c0b2030c33" PARTTYPE="0fc63daf-8483-4772-8e79-3d69d8477de4" PARTLABEL="writable" PARTUUID="96430331-de31-4ae1-a826-f76354008633" PARTFLAGS="" RA="128" RO="0" RM="0" MODEL="" SERIAL="" SIZE="16.6G" STATE="" OWNER="root" GROUP="disk" MODE="brw-rw----" ALIGNMENT="0" MIN-IO="512" OPT-IO="0" PHY-SEC="512" LOG-SEC="512" ROTA="1" SCHED="deadline" RQ-SIZE="128" TYPE="part" DISC-ALN="0" DISC-GRAN="512B" DISC-MAX="2G" DISC-ZERO="0" WSAME="0B" WWN="" RAND="1" PKNAME="sda" HCTL="" TRAN="" REV="" VENDOR=""
`
	return strings.Split(dualData, "\n"), err
}

func (s *PartitionTestSuite) TestIsDual(c *C) {
	runLsblk = mockRunLsblkDual

	p := New()
	c.Assert(p.DualRootPartitions(), Equals, true)
}
