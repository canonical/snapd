package partition

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "launchpad.net/gocheck"
)

// Hook up gocheck into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

// partition specific testsuite
type PartitionTestSuite struct {
	tempdir string
}

var _ = Suite(&PartitionTestSuite{})

func mockRunCommand(args ...string) (err error) {
	return err
}

func (s *PartitionTestSuite) SetUpTest(c *C) {
	s.tempdir = c.MkDir()
	runLsblk = mockRunLsblkDualSnappy

	bootloaderGrubDir = filepath.Join(s.tempdir, "boot", "grub")
	bootloaderGrubConfigFile = filepath.Join(bootloaderGrubDir, "grub.cfg")
	bootloaderGrubEnvFile = filepath.Join(bootloaderGrubDir, "grubenv")
	bootloaderGrubInstallCmd = filepath.Join(s.tempdir, "grub-install")
	bootloaderGrubUpdateCmd = filepath.Join(s.tempdir, "update-grub")
}

func (s *PartitionTestSuite) TearDownTest(c *C) {
	os.RemoveAll(s.tempdir)

	// always restore what we might have mocked away
	runCommand = runCommandImpl
	defaultCacheDir = realDefaultCacheDir
	getBootloader = getBootloaderImpl

	bootloaderGrubConfigFile = bootloaderGrubConfigFileReal
	bootloaderGrubEnvFile = bootloaderGrubEnvFileReal
	bootloaderGrubInstallCmd = bootloaderGrubInstallCmdReal
	bootloaderGrubUpdateCmd = bootloaderGrubUpdateCmdReal
}

func makeHardwareYaml(c *C, hardwareYaml string) (outPath string) {
	tmp, err := ioutil.TempFile(c.MkDir(), "hw-")
	c.Assert(err, IsNil)
	defer tmp.Close()

	if hardwareYaml == "" {
		hardwareYaml = `
kernel: assets/vmlinuz
initrd: assets/initrd.img
dtbs: assets/dtbs
partition-layout: system-AB
bootloader: u-boot
`
	}
	_, err = tmp.Write([]byte(hardwareYaml))
	c.Assert(err, IsNil)

	return tmp.Name()
}

func (s *PartitionTestSuite) TestHardwareSpec(c *C) {
	p := New()
	c.Assert(p, NotNil)

	p.hardwareSpecFile = makeHardwareYaml(c, "")
	hw, err := p.hardwareSpec()
	c.Assert(err, IsNil)
	c.Assert(hw.Kernel, Equals, "assets/vmlinuz")
	c.Assert(hw.Initrd, Equals, "assets/initrd.img")
	c.Assert(hw.DtbDir, Equals, "assets/dtbs")
	c.Assert(hw.PartitionLayout, Equals, bootloaderSystemAB)
	c.Assert(hw.Bootloader, Equals, bootloaderNameUboot)
}

func mockRunLsblkDualSnappy() (output []string, err error) {
	dualData := `
NAME="sda" LABEL="" PKNAME="" MOUNTPOINT=""
NAME="sda1" LABEL="" PKNAME="sda" MOUNTPOINT=""
NAME="sda2" LABEL="system-boot" PKNAME="sda" MOUNTPOINT="/boot/efi"
NAME="sda3" LABEL="system-a" PKNAME="sda" MOUNTPOINT="/"
NAME="sda4" LABEL="system-b" PKNAME="sda" MOUNTPOINT=""
NAME="sda5" LABEL="writable" PKNAME="sda" MOUNTPOINT="/writable"
NAME="sr0" LABEL="" PKNAME="" MOUNTPOINT=""
`
	return strings.Split(dualData, "\n"), err
}

func (s *PartitionTestSuite) TestSnappyDualRoot(c *C) {
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

func (s *PartitionTestSuite) TestRunWithOtherDualParitionRO(c *C) {
	p := New()
	reportedRoot := ""
	err := p.RunWithOther(RO, func(otherRoot string) (err error) {
		reportedRoot = otherRoot
		return nil
	})
	c.Assert(err, IsNil)
	c.Assert(reportedRoot, Equals, (&Partition{}).MountTarget())
}

func (s *PartitionTestSuite) TestRunWithOtherDualParitionRWFuncErr(c *C) {
	runCommand = mockRunCommand

	p := New()
	err := p.RunWithOther(RW, func(otherRoot string) (err error) {
		return errors.New("canary")
	})

	// ensure we actually got the right error
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, "canary")

	// ensure cleanup happend

	// FIXME: mounts is global
	c.Assert(mounts, DeepEquals, []string{})
}

func (s *PartitionTestSuite) TestRunWithOtherSingleParitionRO(c *C) {
	runLsblk = mockRunLsblkSingleRootSnappy
	p := New()
	err := p.RunWithOther(RO, func(otherRoot string) (err error) {
		return nil
	})
	c.Assert(err, Equals, ErrNoDualPartition)
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

func (s *PartitionTestSuite) TestMountUnmountTracking(c *C) {
	runCommand = mockRunCommand

	p := New()
	c.Assert(p, NotNil)

	p.mountOtherRootfs(false)
	c.Assert(mounts, DeepEquals, []string{p.MountTarget()})
	p.unmountOtherRootfs()
	c.Assert(mounts, DeepEquals, []string{})
}

func (s *PartitionTestSuite) TestStringSliceRemoveExisting(c *C) {
	haystack := []string{"one", "two", "three"}

	newSlice := stringSliceRemove(haystack, "two")
	c.Assert(newSlice, DeepEquals, []string{"one", "three"})

	// note here that haystack is no longer "valid", i.e. the
	// underlying array is modified (which is fine)
	c.Assert(haystack, DeepEquals, []string{"one", "three", "three"})
}

func (s *PartitionTestSuite) TestStringSliceRemoveNoexistingNoOp(c *C) {
	haystack := []string{"6", "28", "496", "8128"}
	newSlice := stringSliceRemove(haystack, "99")
	c.Assert(newSlice, DeepEquals, []string{"6", "28", "496", "8128"})
}

func (s *PartitionTestSuite) TestUndoMounts(c *C) {
	runCommand = mockRunCommand
	s.makeFakeGrubEnv(c)

	p := New()
	c.Assert(c, NotNil)

	// FIXME: mounts is global
	c.Assert(mounts, DeepEquals, []string{})
	p.bindmountRequiredFilesystems()
	c.Assert(mounts, DeepEquals, []string{
		p.MountTarget() + "/dev",
		p.MountTarget() + "/proc",
		p.MountTarget() + "/sys",
		p.MountTarget() + "/boot/efi",
		// this comes from the grub bootloader via AdditionalBindMounts
		p.MountTarget() + "/boot/grub",
	})
	p.unmountRequiredFilesystems()
	c.Assert(mounts, DeepEquals, []string{})
}

func mockRunLsblkNoSnappy() (output []string, err error) {
	dualData := `
NAME="sda" LABEL="" PKNAME="" MOUNTPOINT=""
NAME="sda1" LABEL="meep" PKNAME="sda" MOUNTPOINT="/"
NAME="sr0" LABEL="" PKNAME="" MOUNTPOINT=""
`
	return strings.Split(dualData, "\n"), err
}

func (s *PartitionTestSuite) TestSnappyNoSnappyPartitions(c *C) {
	runLsblk = mockRunLsblkNoSnappy

	p := New()
	err := p.getPartitionDetails()
	c.Assert(err, Equals, ErrPartitionDetection)

	c.Assert(p.dualRootPartitions(), Equals, false)
	c.Assert(p.singleRootPartition(), Equals, false)

	c.Assert(p.rootPartition(), IsNil)
	c.Assert(p.bootPartition(), IsNil)
	c.Assert(p.writablePartition(), IsNil)
	c.Assert(p.otherRootPartition(), IsNil)
}

// mock bootloader for the tests
type mockBootloader struct {
	ToggleRootFSCalled              bool
	HandleAssetsCalled              bool
	MarkCurrentBootSuccessfulCalled bool
	SyncBootFilesCalled             bool
}

func (b *mockBootloader) Name() bootloaderName {
	return "mocky"
}
func (b *mockBootloader) ToggleRootFS() error {
	b.ToggleRootFSCalled = true
	return nil
}
func (b *mockBootloader) SyncBootFiles() error {
	b.SyncBootFilesCalled = true
	return nil
}
func (b *mockBootloader) HandleAssets() error {
	b.HandleAssetsCalled = true
	return nil
}
func (b *mockBootloader) GetBootVar(name string) (string, error) {
	return "", nil
}
func (b *mockBootloader) GetRootFSName() string {
	return ""
}
func (b *mockBootloader) GetOtherRootFSName() string {
	return ""
}
func (b *mockBootloader) GetNextBootRootFSName() (string, error) {
	return "", nil
}
func (b *mockBootloader) MarkCurrentBootSuccessful() error {
	b.MarkCurrentBootSuccessfulCalled = true
	return nil
}
func (b *mockBootloader) AdditionalBindMounts() []string {
	return nil
}

func (s *PartitionTestSuite) TestToggleBootloaderRootfs(c *C) {
	runCommand = mockRunCommand
	b := &mockBootloader{}
	getBootloader = func(p *Partition) (bootLoader, error) {
		return b, nil
	}

	p := New()
	c.Assert(c, NotNil)

	err := p.toggleBootloaderRootfs()
	c.Assert(err, IsNil)
	c.Assert(b.ToggleRootFSCalled, Equals, true)
	c.Assert(b.HandleAssetsCalled, Equals, true)
}

func (s *PartitionTestSuite) TestMarkBootSuccessful(c *C) {
	runCommand = mockRunCommand
	b := &mockBootloader{}
	getBootloader = func(p *Partition) (bootLoader, error) {
		return b, nil
	}

	p := New()
	c.Assert(c, NotNil)

	err := p.MarkBootSuccessful()
	c.Assert(err, IsNil)
	c.Assert(b.MarkCurrentBootSuccessfulCalled, Equals, true)
}

func (s *PartitionTestSuite) TestSyncBootFiles(c *C) {
	runCommand = mockRunCommand
	b := &mockBootloader{}
	getBootloader = func(p *Partition) (bootLoader, error) {
		return b, nil
	}

	p := New()
	c.Assert(c, NotNil)

	err := p.SyncBootloaderFiles()
	c.Assert(err, IsNil)
	c.Assert(b.SyncBootFilesCalled, Equals, true)
}
