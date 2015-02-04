package partition

import (
	"io/ioutil"
	"os"
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

var bootPartition = "/dev/sda2"
var currentRootPartition= "/dev/sda3"
var otherRootPartition= "/dev/sda4"
var writablePartition = "/dev/sda5"

func getBaseMounts() (mounts []mntEnt) {
        m := mntEnt{
            Device:      bootPartition,
            MountPoint:  "/boot/efi",
            Type:        "vfat",
            Options:     []string{"rw", "relatime", "fmask=0022",
	    "dmask=0022", "codepage=437", "iocharset=iso8859-1",
	    "shortname=mixed", "errors=remount-ro"},
            DumpFreq:    0,
            FsckPassNo:  0,
        }
        mounts = append(mounts, m)

        m = mntEnt{
            Device:      bootPartition,
            MountPoint:  "/boot/grub",
            Type:        "vfat",
            Options:     []string{"rw", "relatime", "fmask=0022",
	    "dmask=0022", "codepage=437", "iocharset=iso8859-1",
	    "shortname=mixed", "errors=remount-ro"},
            DumpFreq:    0,
            FsckPassNo:  0,
        }
        mounts = append(mounts, m)

        m = mntEnt{
            Device:      currentRootPartition,
            MountPoint:  "/root",
            Type:        "ext4",
            Options:     []string{"ro", "relatime", "data=ordered"},
            DumpFreq:    0,
            FsckPassNo:  0,
        }
        mounts = append(mounts, m)

        m = mntEnt{
            Device:      currentRootPartition,
            MountPoint:  "/",
            Type:        "ext4",
            Options:     []string{"ro", "relatime", "data=ordered"},
            DumpFreq:    0,
            FsckPassNo:  0,
        }
        mounts = append(mounts, m)

        m = mntEnt{
            Device:      writablePartition,
            MountPoint:  "/writable",
            Type:        "ext4",
            Options:     []string{"ro", "relatime", "discard", "data=ordered"},
            DumpFreq:    0,
            FsckPassNo:  0,
        }
        mounts = append(mounts, m)

	return mounts
}

func mockGetSingleRootMounts() (mounts []mntEnt, err error) {
	return getBaseMounts(), err
}

func mockGetDualRootMounts() (mounts []mntEnt, err error) {

	single, err := mockGetSingleRootMounts()
	if err != nil {
		return mounts, err
	}

	mounts = append(mounts, single...)

	m := mntEnt{
            Device:      otherRootPartition,
            MountPoint:  "/writable/cache/system",
            Type:        "ext4",
            Options:     []string{"ro", "relatime", "data=ordered"},
            DumpFreq:    0,
            FsckPassNo:  0,
        }
        mounts = append(mounts, m)

	return mounts, err
}

func mockGetSingleRootPartitions() (m map[string]string, err error) {
	m = make(map[string]string)

	m["system-boot"] = bootPartition
	m["system-a"] = currentRootPartition
	m["writable"] = writablePartition

	return m, err
}

func mockGetDualRootPartitions() (m map[string]string, err error) {

	m, err = mockGetSingleRootPartitions()

	if err != nil {
		return nil, err
	}

	m["system-b"] = otherRootPartition

	return m, err
}

func (s *PartitionTestSuite) TestSnappyDualRoot(c *C) {

	getMounts = mockGetDualRootMounts
	getPartitions = mockGetDualRootPartitions

	p := New()
	c.Assert(p.dualRootPartitions(), Equals, true)
	c.Assert(p.singleRootPartition(), Equals, false)

	rootPartitions := p.rootPartitions()

	// XXX: getPartitions() returns a map, and iteration is random.
	// Hence, we cannot rely on the order of the array returned by
	// rootPartitions() so take a sniff first...
	var aIndex int
	var bIndex int

	if rootPartitions[0].name == "system-a" {
		aIndex = 0
		bIndex = 1
	} else {
		aIndex = 1
		bIndex = 0
	}

	c.Assert(rootPartitions[aIndex].name, Equals, "system-a")
	c.Assert(rootPartitions[aIndex].device, Equals, "/dev/sda3")
	c.Assert(rootPartitions[aIndex].parentName, Equals, "/dev/sda")

	c.Assert(rootPartitions[bIndex].name, Equals, "system-b")
	c.Assert(rootPartitions[bIndex].device, Equals, "/dev/sda4")
	c.Assert(rootPartitions[bIndex].parentName, Equals, "/dev/sda")

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
	getMounts = mockGetDualRootMounts
	getPartitions = mockGetDualRootPartitions

	p := New()
	reportedRoot := ""
	err := p.RunWithOther(RO, func(otherRoot string) (err error) {
		reportedRoot = otherRoot
		return nil
	})
	c.Assert(err, IsNil)
	c.Assert(reportedRoot, Equals, (&Partition{}).MountTarget())
}

func (s *PartitionTestSuite) TestRunWithOtherSingleParitionRO(c *C) {
	getMounts = mockGetSingleRootMounts
	getPartitions = mockGetSingleRootPartitions

	p := New()
	err := p.RunWithOther(RO, func(otherRoot string) (err error) {
		return nil
	})
	c.Assert(err, Equals, NoDualPartitionError)
}

func (s *PartitionTestSuite) TestSnappySingleRoot(c *C) {
	getMounts = mockGetSingleRootMounts
	getPartitions = mockGetSingleRootPartitions

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

func mockRunCommand(args ...string) (err error) {
	return err
}

func (s *PartitionTestSuite) TestMountUnmountTracking(c *C) {

	getMounts = mockGetDualRootMounts
	getPartitions = mockGetDualRootPartitions

	// FIXME: there should be a generic
	//        mockFunc(func) (restorer func())
	savedRunCommand := runCommand
	defer func() {
		runCommand = savedRunCommand
	}()
	runCommand = mockRunCommand

	p := New()

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
	getMounts = mockGetDualRootMounts
	getPartitions = mockGetDualRootPartitions

	// FIXME: there should be a generic
	//        mockFunc(func) (restorer func())
	savedRunCommand := runCommand
	defer func() {
		runCommand = savedRunCommand
	}()
	runCommand = mockRunCommand

	p := New()

	// FIXME: mounts is global
	c.Assert(mounts, DeepEquals, []string{})
	p.bindmountRequiredFilesystems()

	// +1 because requiredChrootMounts() only returns the minimum
	// set - if the system has a boot partition we expect one more.
	c.Assert(len(mounts), Equals, len(requiredChrootMounts())+1)

	// check expected values
	c.Assert(stringInSlice(mounts, "/writable/cache/system/dev"), Not(Equals), -1)
	c.Assert(stringInSlice(mounts, "/writable/cache/system/proc"), Not(Equals), -1)
	c.Assert(stringInSlice(mounts, "/writable/cache/system/sys"), Not(Equals), -1)
	c.Assert(stringInSlice(mounts, "/writable/cache/system/boot/efi"), Not(Equals), -1)

	p.unmountRequiredFilesystems()
	c.Assert(mounts, DeepEquals, []string{})
}
