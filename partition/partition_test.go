package partition

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"

	. "launchpad.net/gocheck"
)

// Hook up gocheck into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

// partition specific testsuite
type PartitionTestSuite struct {
}

var _ = Suite(&PartitionTestSuite{})

// Create an empty file specified by path
func createEmptyFile(path string) (err error) {
	return ioutil.WriteFile(path, []byte(""), 0640)
}

// Run specified function from directory dir
func runChdir(dir string, f func() (err error)) (err error) {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	if err = os.Chdir(dir); err != nil {
		return err
	}
	defer func() {
		err = os.Chdir(cwd)
	}()

	return f()
}

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

// Create a fake "/proc/self/mounts"-format file containing only the entries
// we care about.
func makeMountsFile(replaceMap map[string]string) (filename string, err error) {
	template := []string{"/dev/sda2 /boot/efi vfat rw,relatime,fmask=0022,dmask=0022,codepage=437,iocharset=iso8859-1,shortname=mixed,errors=remount-ro 0 0",
		"/dev/sda2 /boot/grub vfat rw,relatime,fmask=0022,dmask=0022,codepage=437,iocharset=iso8859-1,shortname=mixed,errors=remount-ro 0 0",
		"/dev/sda3 /root ext4 ro,relatime,data=ordered 0 0",
		"/dev/sda3 / ext4 ro,relatime,data=ordered 0 0",
		"/dev/sda4 /writable/cache/system ext4 ro,relatime,data=ordered 0 0",
		"/dev/sda5 /writable ext4 rw,relatime,discard,data=ordered 0 0"}

	var lines []string

	for _, line := range template {
		for from, to := range replaceMap {
			line = strings.Replace(line, from, to, -1)
		}
		lines = append(lines, line)
	}

	tmp, err := ioutil.TempFile("", "mounts-")
	if err != nil {
		return "", err
	}

	for _, line := range lines {
		tmp.WriteString(fmt.Sprintf("%s\n", line))
	}

	tmp.Close()

	return tmp.Name(), err
}

// Create a fake directory tree that is similar to that created by udev
// as /dev/disk/by-partlabel/.
func makeDeviceDirs(disk string, deviceMap map[string]string) (parent string, linkDir string, err error) {

	parent, err = ioutil.TempDir("", "mounts-parent-")
	if err != nil {
		return "", "", err
	}

	devDir := path.Join(parent, "dev")
	linkDir = path.Join(devDir, "disk", "by-partlabel")
	os.MkdirAll(linkDir, 0755)

	// Create the fake overall disk device
	if err = createEmptyFile(path.Join(devDir, disk)); err != nil {
		return "", "", err
	}

	for name, label := range deviceMap {
		// Create the fake partition device
		if err = createEmptyFile(path.Join(devDir, name)); err != nil {
			return "", "", err
		}

		relativePath := fmt.Sprintf("../../%s", name)

		// create the symlink pointing to the fake device
		err = runChdir(linkDir, func() (err error) {
			cmd := exec.Command("/bin/ln", "-s", relativePath, label)
			return cmd.Run()
		})
		if err != nil {
			return "", "", err
		}
	}
	return parent, linkDir, nil
}

func makeMountsFileAndDevices(dualRootfs bool) (mountsFile string, parentDevDir string, devDir string, err error) {

	disk := "sda"
	devs := map[string]string{
		"sda1": "grub",
		"sda2": "system-boot",
		"sda3": "system-a",
		"sda5": "writable",
	}

	if dualRootfs {
		devs["sda4"] = "system-b"
	}

	parentDevDir, devDir, err = makeDeviceDirs(disk, devs)
	if err != nil {
		return "", "", "", err
	}

	replacements := make(map[string]string)

	for dev, _ := range devs {
		replacements[fmt.Sprintf("/dev/%s", dev)] =
			path.Join(parentDevDir, "/dev", dev)
	}

	if mountsFile, err = makeMountsFile(replacements); err != nil {
		return mountsFile, parentDevDir, devDir, err
	}

	return mountsFile, parentDevDir, devDir, err
}

func (s *PartitionTestSuite) TestHardwareSpec(c *C) {
	mockMountsFile, parentDevDir, deviceDir, err := makeMountsFileAndDevices(true)
	c.Assert(err, IsNil)
	defer func() {
		os.Remove(mockMountsFile)
		os.RemoveAll(parentDevDir)
	}()

	diskDeviceDir = deviceDir
	mountsFile = mockMountsFile

	p := New()
	c.Assert(p, NotNil)

	tmp2, err := makeHardwareYaml()
	defer func() {
		os.Remove(tmp2.Name())
	}()

	p.hardwareSpecFile = tmp2.Name()
	hw, err := p.hardwareSpec()
	c.Assert(err, IsNil)
	c.Assert(hw.Kernel, Equals, "assets/vmlinuz")
	c.Assert(hw.Initrd, Equals, "assets/initrd.img")
	c.Assert(hw.DtbDir, Equals, "assets/dtbs")
	c.Assert(hw.PartitionLayout, Equals, "system-AB")
	c.Assert(hw.Bootloader, Equals, "uboot")
}

func (s *PartitionTestSuite) TestSnappyDualRoot(c *C) {
	mockMountsFile, parentDevDir, deviceDir, err := makeMountsFileAndDevices(true)
	c.Assert(err, IsNil)
	defer func() {
		os.Remove(mockMountsFile)
		os.RemoveAll(parentDevDir)
	}()
	diskDeviceDir = deviceDir
	mountsFile = mockMountsFile

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
	c.Assert(strings.HasSuffix(rootPartitions[aIndex].device, "/dev/sda3"), Equals, true)
	c.Assert(strings.HasSuffix(rootPartitions[aIndex].parentName, "/dev/sda"), Equals, true)

	c.Assert(rootPartitions[bIndex].name, Equals, "system-b")
	c.Assert(strings.HasSuffix(rootPartitions[bIndex].device, "/dev/sda4"), Equals, true)
	c.Assert(strings.HasSuffix(rootPartitions[bIndex].parentName, "/dev/sda"), Equals, true)

	wp := p.writablePartition()
	c.Assert(wp.name, Equals, "writable")

	c.Assert(strings.HasSuffix(wp.device, "/dev/sda5"), Equals, true)
	c.Assert(strings.HasSuffix(wp.parentName, "/dev/sda"), Equals, true)

	boot := p.bootPartition()
	c.Assert(boot.name, Equals, "system-boot")

	c.Assert(strings.HasSuffix(boot.device, "/dev/sda2"), Equals, true)
	c.Assert(strings.HasSuffix(boot.parentName, "/dev/sda"), Equals, true)

	root := p.rootPartition()
	c.Assert(root, Not(IsNil))
	c.Assert(root.name, Equals, "system-a")

	c.Assert(strings.HasSuffix(root.device, "/dev/sda3"), Equals, true)
	c.Assert(strings.HasSuffix(root.parentName, "/dev/sda"), Equals, true)

	other := p.otherRootPartition()
	c.Assert(other.name, Equals, "system-b")
	c.Assert(strings.HasSuffix(other.device, "/dev/sda4"), Equals, true)
	c.Assert(strings.HasSuffix(other.parentName, "/dev/sda"), Equals, true)
}

func (s *PartitionTestSuite) TestRunWithOtherDualParitionRO(c *C) {
	mockMountsFile, parentDevDir, deviceDir, err := makeMountsFileAndDevices(true)
	c.Assert(err, IsNil)
	defer func() {
		os.Remove(mockMountsFile)
		os.RemoveAll(parentDevDir)
	}()
	diskDeviceDir = deviceDir
	mountsFile = mockMountsFile

	p := New()
	reportedRoot := ""
	err = p.RunWithOther(RO, func(otherRoot string) (err error) {
		reportedRoot = otherRoot
		return nil
	})
	c.Assert(err, IsNil)
	c.Assert(reportedRoot, Equals, (&Partition{}).MountTarget())
}

func (s *PartitionTestSuite) TestRunWithOtherSingleParitionRO(c *C) {
	mockMountsFile, parentDevDir, deviceDir, err := makeMountsFileAndDevices(false)
	c.Assert(err, IsNil)
	defer func() {
		os.Remove(mockMountsFile)
		os.RemoveAll(parentDevDir)
	}()
	diskDeviceDir = deviceDir
	mountsFile = mockMountsFile

	p := New()
	err = p.RunWithOther(RO, func(otherRoot string) (err error) {
		return nil
	})
	c.Assert(err, Equals, NoDualPartitionError)
}

func (s *PartitionTestSuite) TestSnappySingleRoot(c *C) {
	mockMountsFile, parentDevDir, deviceDir, err := makeMountsFileAndDevices(false)
	c.Assert(err, IsNil)
	defer func() {
		os.Remove(mockMountsFile)
		os.RemoveAll(parentDevDir)
	}()
	diskDeviceDir = deviceDir
	mountsFile = mockMountsFile

	p := New()
	c.Assert(p.dualRootPartitions(), Equals, false)
	c.Assert(p.singleRootPartition(), Equals, true)

	root := p.rootPartition()
	c.Assert(root.name, Equals, "system-a")
	c.Assert(strings.HasSuffix(root.device, "/dev/sda3"), Equals, true)
	c.Assert(strings.HasSuffix(root.parentName, "/dev/sda"), Equals, true)

	other := p.otherRootPartition()
	c.Assert(other, IsNil)

	rootPartitions := p.rootPartitions()
	c.Assert(&rootPartitions[0], DeepEquals, root)
}

func mockRunCommand(args ...string) (err error) {
	return err
}

func (s *PartitionTestSuite) TestMountUnmountTracking(c *C) {
	mockMountsFile, parentDevDir, deviceDir, err := makeMountsFileAndDevices(true)
	c.Assert(err, IsNil)
	defer func() {
		os.Remove(mockMountsFile)
		os.RemoveAll(parentDevDir)
	}()
	diskDeviceDir = deviceDir
	mountsFile = mockMountsFile

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
	mockMountsFile, parentDevDir, deviceDir, err := makeMountsFileAndDevices(true)
	c.Assert(err, IsNil)
	defer func() {
		os.Remove(mockMountsFile)
		os.RemoveAll(parentDevDir)
	}()
	diskDeviceDir = deviceDir
	mountsFile = mockMountsFile

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

	// check expected values
	c.Assert(stringInSlice(mounts, "/writable/cache/system/dev"), Not(Equals), -1)
	c.Assert(stringInSlice(mounts, "/writable/cache/system/proc"), Not(Equals), -1)
	c.Assert(stringInSlice(mounts, "/writable/cache/system/sys"), Not(Equals), -1)
	c.Assert(stringInSlice(mounts, "/writable/cache/system/boot/efi"), Not(Equals), -1)

	p.unmountRequiredFilesystems()
	c.Assert(mounts, DeepEquals, []string{})
}
