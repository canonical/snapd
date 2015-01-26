//--------------------------------------------------------------------
// Copyright (c) 2014-2015 Canonical Ltd.
//--------------------------------------------------------------------
// TODO:
//
// - logging
// - SNAPPY_DEBUG
// - locking (sync.Mutex)
//--------------------------------------------------------------------

//--------------------------------------------------------------------
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU General Public License version 3, as published
// by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful, but
// WITHOUT ANY WARRANTY; without even the implied warranties of
// MERCHANTABILITY, SATISFACTORY QUALITY, or FITNESS FOR A PARTICULAR
// PURPOSE.  See the GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License along
// with this program.  If not, see <http://www.gnu.org/licenses/>.
//--------------------------------------------------------------------

// partition - manipulate disk partitions.
// The main callables are UpdateBootLoader()
package partition

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"regexp"
	"strings"
	"syscall"

	"gopkg.in/yaml.v2"
)

var debug bool = false

var signal_handler_registered bool = false

// Name of writable user data partition label as created by
// ubuntu-device-flash(1).
const WRITABLE_PARTITION_LABEL = "writable"

// Name of primary root filesystem partition label as created by
// ubuntu-device-flash(1).
const ROOTFS_A_LABEL = "system-a"

// Name of primary root filesystem partition label as created by
// ubuntu-device-flash(1). Note that this partition will
// only be present if this is an A/B upgrade system.
const ROOTFS_B_LABEL = "system-b"

// name of boot partition label as created by ubuntu-device-flash(1).
const BOOT_PARTITION_LABEL = "system-boot"

// FIXME: Should query system-image-cli (see bug LP:#1380574).
const DEFAULT_CACHE_DIR = "/writable/cache"

// Directory to mount writable root filesystem below the cache
// diretory.
const MOUNT_TARGET = "system"

// File creation mode used when any directories are created
const DIR_MODE = 0750

// Name of system-image's master configuration file. Used to query
// the system-image version on the other partition.
const SYSTEM_IMAGE_CONFIG = "/etc/system-image/client.ini"

var (
	bootloaderError = errors.New("Unable to determine bootloader")

	PartitionQueryError     = errors.New("Failed to query partitions")
	PartitionDetectionError = errors.New("Failed to detect system type")
)

// Declarative specification of the type of system which specifies such
// details as:
//
// - the location of initrd+kernel within the system-image archive.
// - the location of hardware-specific .dtb files within the
//   system-image archive.
// - the type of bootloader that should be used for this system.
// - expected system partition layout (single or dual rootfs's).
const HARDWARE_SPEC_FILE = "hardware.yaml"

// Directory that _may_ get automatically created on unpack that
// contains updated hardware-specific boot assets (such as initrd, kernel)
const ASSETS_DIR = "assets"

// Directory that _may_ get automatically created on unpack that
// contains updated hardware-specific assets that require flashing
// to the disk (such as uBoot, MLO)
const FLASH_ASSETS_DIR = "flashtool-assets"

//--------------------------------------------------------------------
// Globals

// list of current mounts that this module has created
var mounts []string

// list of current bindmounts this module has created
var bindMounts []string

//--------------------------------------------------------------------

type PartitionInterface interface {
	UpdateBootloader() (err error)
	MarkBootSuccessful() (err error)
	// FIXME: could we make SyncBootloaderFiles part of UpdateBootloader
	//        to expose even less implementation details?
	SyncBootloaderFiles() (err error)
	NextBootIsOther() bool

	// run the function f with the otherRoot mounted
	RunWithOther(f func(otherRoot string) (err error)) (err error)
}

type Partition struct {
	// all partitions
	partitions []BlockDevice

	// just root partitions
	roots []string

	MountTarget string

	hardwareSpecFile string
}

// FIXME: can we make this private(?)
type BlockDevice struct {
	// label for partition
	name string

	// full path to device on which partition exists
	// (for example "/dev/sda3")
	device string

	// full path to disk device (for example "/dev/sda")
	parentName string

	// mountpoint (or nil if not mounted)
	mountpoint string
}

// Representation of HARDWARE_SPEC_FILE
type HardwareSpecType struct {
	Kernel          string `yaml:"kernel"`
	Initrd          string `yaml:"initrd"`
	DtbDir          string `yaml:"dtbs"`
	PartitionLayout string `yaml:"partition-layout"`
	Bootloader      string `yaml:"bootloader"`
}

func init() {
	if os.Getenv("SNAPPY_DEBUG") != "" {
		debug = true
	}

	if signal_handler_registered == false {
		setup_signal_handler()
		signal_handler_registered = true
	}
}

func undoMounts(mounts []string) (err error) {
	// Iterate backwards since we want a reverse-sorted list of
	// mounts to ensure we can unmount in order.
	for i, _ := range mounts {
		err := unmount(mounts[len(mounts)-i])
		if err != nil {
			return err
		}
	}

	return err
}

func signal_handler(sig os.Signal) {
	err := undoMounts(mounts)
	if err != nil {
		// FIXME: use logger
		fmt.Fprintf(os.Stderr, "ERROR: failed to unmount: %s", err)
	}
}

func setup_signal_handler() {
	ch := make(chan os.Signal, 1)

	// add the signals we care about
	signal.Notify(ch, os.Interrupt)
	signal.Notify(ch, syscall.SIGTERM)

	go func() {
		// block waiting for a signal
		sig := <-ch

		// handle it
		signal_handler(sig)
		os.Exit(1)
	}()
}

// Returns a list of root filesystem partition labels
func rootPartitionLabels() []string {
	return []string{ROOTFS_A_LABEL, ROOTFS_B_LABEL}
}

// Returns a list of all recognised partition labels
func allPartitionLabels() []string {
	var labels []string

	labels = rootPartitionLabels()
	labels = append(labels, BOOT_PARTITION_LABEL)
	labels = append(labels, WRITABLE_PARTITION_LABEL)

	return labels
}

// Returns a minimal list of mounts required for running grub-install
// within a chroot.
func requiredChrootMounts() []string {
	return []string{"/dev", "/proc", "/sys"}
}

// Run the command specified by args
// FIXME: put into utils package
func RunCommand(args []string) (err error) {
	if len(args) == 0 {
		return errors.New("ERROR: no command specified")
	}

	// FIXME: use logger
	/*
		if debug == true {

			log.debug('running: {}'.format(args))
		}
	*/

	if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
		cmdline := strings.Join(args, " ")
		return errors.New(fmt.Sprintf("Failed to run command '%s': %s (%s)",
			cmdline,
			out,
			err))
	}
	return nil
}

// Run command specified by args and return array of output lines.
// FIXME: put into utils package
func GetCommandStdout(args []string) (output []string, err error) {

	// FIXME: use logger
	/*
		if debug == true {

			log.debug('running: {}'.format(args))
		}
	*/

	bytes, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil {
		return output, err
	}

	output = strings.Split(string(bytes), "\n")

	return output, err
}

// Return nil if given path exists.
// FIXME: put into utils package
func FileExists(path string) (err error) {
	_, err = os.Stat(path)

	return err
}

func IsDirectory(path string) bool {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return false
	}

	return fileInfo.IsDir()
}

func mount(source, target, options string) (err error) {
	var args []string

	args = append(args, "/bin/mount")

	if options != "" {
		args = append(args, fmt.Sprintf("-o%s", options))
	}

	args = append(args, source)
	args = append(args, target)

	err = RunCommand(args)

	if err == nil {
		mounts = append(mounts, target)
	}

	return err
}

// FIXME2: would it make sense to rename to something like
//         "UmountAndRemoveFromMountList" to indicate it has side-effects?
func unmount(target string) (err error) {
	var args []string

	args = append(args, "/bin/umount")
	args = append(args, target)

	err = RunCommand(args)

	if err == nil {
		// FIXME: so this is golang slice remove?!?! really?
		pos := stringInSlice(mounts, target)
		if pos >= 0 {
			mounts = append(mounts[:pos], mounts[pos+1:]...)
		}
	}

	return err
}

func bindmount(source, target string) (err error) {
	err = mount(source, target, "bind")

	if err == nil {
		bindMounts = append(bindMounts, target)
	}

	return err
}

// Run fsck(8) on specified device.
func Fsck(device string) (err error) {
	var args []string

	args = append(args, "/sbin/fsck")

	// Paranoia - don't fsck if already mounted
	args = append(args, "-M")

	args = append(args, "-av")
	args = append(args, device)

	return RunCommand(args)
}

// Returns the position of the string in the given slice or -1 if its not found
func stringInSlice(slice []string, value string) int {
	for i, s := range slice {
		if s == value {
			return i
		}
	}

	return -1
}

var runLsblk = func() (output []string, err error) {
	args := []string{}
	args = append(args, "/bin/lsblk")
	args = append(args, "--ascii")
	args = append(args, "--output=NAME,LABEL,PKNAME,MOUNTPOINT")
	args = append(args, "--pairs")
	return GetCommandStdout(args)
}

// Determine details of the recognised disk partitions
// available on the system via lsblk
func loadPartitionDetails() (partitions []BlockDevice, err error) {
	var recognised []string = allPartitionLabels()

	lines, err := runLsblk()
	if err != nil {
		return partitions, err
	}
	pattern := regexp.MustCompile(`(?:[^\s"]|"(?:[^"])*")+`)

	for _, line := range lines {
		fields := make(map[string]string)

		// split the line into 'NAME="quoted value"' fields
		matches := pattern.FindAllString(line, -1)

		for _, match := range matches {
			tmp := strings.Split(match, "=")
			name := tmp[0]

			// remove quotes
			value := strings.Trim(tmp[1], `"`)

			// store
			fields[name] = value
		}

		// Look for expected partition labels
		name, ok := fields["LABEL"]
		if ok == false {
			continue
		}

		pos := stringInSlice(recognised, name)
		if pos < 0 {
			// ignore unrecognised partitions
			continue
		}

		// reconstruct full path to disk partition device
		device := fmt.Sprintf("/dev/%s", fields["NAME"])

		// FIXME: we should have a way to mock the "/dev" dir
		//        or we skip this test lsblk never returns non-existing
		//        devices
		/*
			if err := FileExists(device); err != nil {
				continue
			}
		*/
		// reconstruct full path to entire disk device
		disk := fmt.Sprintf("/dev/%s", fields["PKNAME"])

		// FIXME: we should have a way to mock the "/dev" dir
		//        or we skip this test lsblk never returns non-existing
		//        files
		/*
			if err := FileExists(disk); err != nil {
				continue
			}
		*/
		bd := BlockDevice{
			name:       fields["LABEL"],
			device:     device,
			mountpoint: fields["MOUNTPOINT"],
			parentName: disk,
		}

		partitions = append(partitions, bd)
	}

	return partitions, nil
}

func (p *Partition) makeMountPoint(readOnlyMount bool) (err error) {
	if readOnlyMount == true {
		// A system update "owns" the default mount_target directory.
		// So in read-only mode, use a temporary mountpoint name to
		// avoid colliding with a running system update.
		p.MountTarget = fmt.Sprintf("%s-ro-%d",
			p.MountTarget,
			os.Getpid())
	} else {
		p.MountTarget = p.getMountTarget()
	}

	return os.MkdirAll(p.MountTarget, DIR_MODE)
}

// Constructor
func New() *Partition {
	p := new(Partition)

	p.getPartitionDetails()
	p.hardwareSpecFile = fmt.Sprint("%s/%s", p.cacheDir(), HARDWARE_SPEC_FILE)

	return p
}

func (p *Partition) RunWithOther(f func(otherRoot string) (err error)) (err error) {
	dual := p.dualRootPartitions()
	// FIXME: should we simply
	if !dual {
		return f("/")
	}

	// FIXME: why is this not a parameter of MountOtherRootfs()?
	err = p.MountOtherRootfs(true)
	if err != nil {
		return err
	}

	defer func() {
		err = p.UnmountOtherRootfsAndCleanup()
	}()

	return f(p.MountTarget)

}

func (p *Partition) SyncBootloaderFiles() (err error) {
	bootloader, err := p.GetBootloader()
	if err != nil {
		return err
	}
	return bootloader.SyncBootFiles()
}

func (p *Partition) UpdateBootloader() (err error) {
	if p.dualRootPartitions() {
		return p.toggleBootloaderRootfs()
	}
	return err
}

func (p *Partition) GetBootloader() (bootloader BootLoader, err error) {

	bootloaders := []BootLoader{NewUboot(p), NewGrub(p)}

	for _, b := range bootloaders {
		if b.Installed() == true {
			return b, err
		}
	}

	return nil, bootloaderError
}

func (p *Partition) MarkBootSuccessful() (err error) {
	bootloader, err := p.GetBootloader()
	if err != nil {
		return err
	}

	return bootloader.MarkCurrentBootSuccessful()
}

// Return true if the next boot will use the other rootfs
// partition.
func (p *Partition) NextBootIsOther() bool {
	var value string
	var err error
	var label string

	bootloader, err := p.GetBootloader()
	if err != nil {
		return false
	}

	value, err = bootloader.GetBootVar(BOOTLOADER_BOOTMODE_VAR)
	if err != nil {
		return false
	}

	if value != BOOTLOADER_BOOTMODE_VAR_START_VALUE {
		return false
	}

	if label, err = bootloader.GetNextBootRootFSName(); err != nil {
		return false
	}

	if label == bootloader.GetOtherRootFSName() {
		return true
	}

	return false
}

// Returns the full path to the cache directory, which is used as a
// scratch pad, for downloading new images to and bind mounting the
// rootfs.
func (p *Partition) cacheDir() string {
	return DEFAULT_CACHE_DIR
}

func (p *Partition) hardwareSpec() (hardware HardwareSpecType, err error) {
	h := HardwareSpecType{}

	data, err := ioutil.ReadFile(p.hardwareSpecFile)
	if err != nil {
		return h, err
	}

	err = yaml.Unmarshal([]byte(data), &h)

	return h, err
}

// Return full path to the main assets directory
func (p *Partition) assetsDir() string {
	return fmt.Sprintf("%s/%s", p.cacheDir(), ASSETS_DIR)
}

// Return the full path to the hardware-specific flash assets directory.
func (p *Partition) flashAssetsDir() string {
	return fmt.Sprintf("%s/%s", p.cacheDir(), FLASH_ASSETS_DIR)
}

// Get the full path to the mount target directory
func (p *Partition) getMountTarget() string {
	return path.Join(p.cacheDir(), MOUNT_TARGET)
}

func (p *Partition) getPartitionDetails() (err error) {
	p.partitions, err = loadPartitionDetails()
	if err != nil {
		return err

	}
	if !p.dualRootPartitions() && !p.singleRootPartition() {
		return PartitionDetectionError
	}

	return err
}

// Return array of BlockDevices representing available root partitions
func (p *Partition) rootPartitions() (roots []BlockDevice) {
	for _, part := range p.partitions {
		pos := stringInSlice(rootPartitionLabels(), part.name)
		if pos >= 0 {
			roots = append(roots, part)
		}
	}

	return roots
}

// Return true if system has dual root partitions configured in the
// expected manner for a snappy system.
func (p *Partition) dualRootPartitions() bool {
	return len(p.rootPartitions()) == 2
}

// Return true if system has a single root partition configured in the
// expected manner for a snappy system.
func (p *Partition) singleRootPartition() bool {
	return len(p.rootPartitions()) == 1
}

// Return pointer to BlockDevice representing writable partition
func (p *Partition) writablePartition() (result *BlockDevice) {
	for _, part := range p.partitions {
		if part.name == WRITABLE_PARTITION_LABEL {
			return &part
		}
	}

	return result
}

// Return pointer to BlockDevice representing boot partition (if any)
func (p *Partition) bootPartition() (result *BlockDevice) {
	for _, part := range p.partitions {
		if part.name == BOOT_PARTITION_LABEL {
			return &part
		}
	}

	return result
}

// Return pointer to BlockDevice representing currently mounted root
// filesystem
func (p *Partition) rootPartition() (result *BlockDevice) {
	for _, part := range p.rootPartitions() {
		if part.mountpoint == "/" {
			return &part
		}
	}

	return result
}

// Return pointer to BlockDevice representing the "other" root
// filesystem (which is not currently mounted)
func (p *Partition) otherRootPartition() (result *BlockDevice) {
	for _, part := range p.rootPartitions() {
		if part.mountpoint != "/" {
			return &part
		}
	}

	return result
}

// Mount the "other" root filesystem
func (p *Partition) MountOtherRootfs(readOnlyMount bool) (err error) {
	var other *BlockDevice

	p.makeMountPoint(readOnlyMount)

	other = p.otherRootPartition()

	if readOnlyMount == true {
		err = mount(other.device, p.MountTarget, "ro")
	} else {
		err = Fsck(other.device)
		if err != nil {
			return err
		}
		err = mount(other.device, p.MountTarget, "")
	}

	return err
}

func (p *Partition) UnmountOtherRootfs() (err error) {
	return unmount(p.MountTarget)
}

func (p *Partition) UnmountOtherRootfsAndCleanup() (err error) {
	err = p.UnmountOtherRootfs()

	if err != nil {
		return err
	}

	return os.Remove(p.MountTarget)
}

// The bootloader requires a few filesystems to be mounted when
// run from within a chroot.
func (p *Partition) bindmountRequiredFilesystems() (err error) {
	var boot *BlockDevice

	for _, fs := range requiredChrootMounts() {
		target := path.Clean(fmt.Sprintf("%s/%s", p.MountTarget, fs))

		err := bindmount(fs, target)
		if err != nil {
			return err
		}
	}

	boot = p.bootPartition()
	if boot == nil {
		// No separate boot partition
		return nil
	}

	if boot.mountpoint == "" {
		// Impossible situation
		return nil
	}

	target := path.Clean(fmt.Sprintf("%s/%s",
		p.MountTarget,
		boot.mountpoint))
	err = bindmount(boot.mountpoint, target)
	if err != nil {
		return err
	}

	return err
}

// Undo the effects of BindmountRequiredFilesystems()
func (p *Partition) unmountRequiredFilesystems() (err error) {
	return undoMounts(bindMounts)
}

// Run the commandline specified by the args array chrooted to the
// new root filesystem.
//
// Errors are fatal.
func (p *Partition) runInChroot(args []string) (err error) {
	var fullArgs []string

	fullArgs = append(fullArgs, "/usr/sbin/chroot")
	fullArgs = append(fullArgs, p.MountTarget)

	fullArgs = append(fullArgs, args...)

	return RunCommand(fullArgs)
}

func (p *Partition) handleBootloader() (err error) {
	bootloader, err := p.GetBootloader()

	if err != nil {
		return err
	}

	// FIXME: use logger
	fmt.Printf("FIXME: HandleBootloader: bootloader=%s\n", bootloader.Name())

	return bootloader.ToggleRootFS()
}

// FIXME: we don't use this right now, but we probably should and expand
//        it so that it also reads "version_details" and "channel_name"
//        like it does when s-i-dbus is called with Information()
func (p *Partition) GetOtherVersion() (version string, err error) {

	err = p.MountOtherRootfs(true)

	if err != nil {
		return version, err
	}

	// Unmount
	defer func() {
		err = p.UnmountOtherRootfs()
	}()

	// Remove mountpoint
	defer func() {
		err = os.Remove(p.MountTarget)
	}()

	file := p.MountTarget + SYSTEM_IMAGE_CONFIG

	if err = FileExists(file); err != nil {
		return version, err
	}

	var args []string

	// FXIME: system-image-cli should return the same info map
	//        as the dbus Information call
	args = append(args, "system-image-cli")
	args = append(args, "-C")
	args = append(args, file)
	args = append(args, "--info")

	lines, err := GetCommandStdout(args)
	if err != nil {
		return version, err
	}

	pattern := regexp.MustCompile(`version version: (\d+)`)

	var revision string

	for _, line := range lines {
		matches := pattern.FindAllStringSubmatch(line, -1)

		if len(matches) == 1 {
			revision = matches[0][1]
			break
		}
	}

	version = revision

	return version, err
}

// Currently, system-image-cli(1) does not provide 'version_string'
// in its verbatim form, hence grope for it for now.
func (p *Partition) getOtherVersionDetail() (detail string, err error) {
	var args []string

	args = append(args, "ubuntu-core-upgrade")
	args = append(args, "--show-other-details")

	lines, err := GetCommandStdout(args)
	if err != nil {
		return detail, err
	}

	pattern := regexp.MustCompile(`version_detail: (.*)`)

	for _, line := range lines {
		matches := pattern.FindAllStringSubmatch(line, -1)

		if len(matches) == 1 {
			detail = matches[0][1]
			break
		}
	}

	return detail, err
}

func (p *Partition) toggleBootloaderRootfs() (err error) {

	if p.dualRootPartitions() != true {
		return errors.New("System is not dual root")
	}

	if err = p.MountOtherRootfs(false); err != nil {
		return err
	}

	if err = p.bindmountRequiredFilesystems(); err != nil {
		return err
	}

	if err = p.handleBootloader(); err != nil {
		return err
	}

	if err = p.unmountRequiredFilesystems(); err != nil {
		return err
	}

	if err = p.UnmountOtherRootfs(); err != nil {
		return err
	}

	bootloader, err := p.GetBootloader()
	if err != nil {
		return err
	}

	return bootloader.HandleAssets()
}
