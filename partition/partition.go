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
	"os/signal"
	"path"
	"regexp"
	"strconv"
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

// Directory created by udev that allows a non-priv user to list the
// available disk partitions by partition label.
const diskDeviceDir = "/dev/disk/by-partlabel"

// Directory to mount writable root filesystem below the cache
// diretory.
const MOUNT_TARGET = "system"

// File creation mode used when any directories are created
const DIR_MODE = 0750

var (
	BootloaderError = errors.New("Unable to determine bootloader")

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
// FIXME: Globals

// list of current mounts that this module has created
var mounts []string

// list of current bindmounts this module has created
var bindMounts []string

//--------------------------------------------------------------------

type MountOption int

const (
	RO MountOption = iota
	RW
)

type PartitionInterface interface {
	UpdateBootloader() (err error)
	MarkBootSuccessful() (err error)
	// FIXME: could we make SyncBootloaderFiles part of UpdateBootloader
	//        to expose even less implementation details?
	SyncBootloaderFiles() (err error)
	NextBootIsOther() bool

	// run the function f with the otherRoot mounted
	RunWithOther(rw MountOption, f func(otherRoot string) (err error)) (err error)
}

type Partition struct {
	// all partitions
	partitions []blockDevice

	// just root partitions
	roots []string

	hardwareSpecFile string
}

type blockDevice struct {
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

// similar to a "struct mntent"
type mntEnt struct {
	Device     string
	MountPoint string
	Type       string
	Options    []string
	DumpFreq   int
	FsckPassNo int
}

// Return the mountpoint for the specified disk partition by inspecting the
// provided mount entries.
func getMountPoint(mounts []mntEnt, partition string) string {
	for _, entry := range mounts {
		// Note that we have to special-case the rootfs mount
		// since there are *two* entries in the mount table; one
		// for mountpoint "/root", the other for "/".
		if entry.Device == partition && entry.MountPoint != "/root" {
			return entry.MountPoint
		}
	}

	return ""
}

// Representation of HARDWARE_SPEC_FILE
type hardwareSpecType struct {
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

	if !signal_handler_registered {
		setup_signal_handler()
		signal_handler_registered = true
	}
}

func undoMounts(mounts []string) (err error) {
	// Iterate backwards since we want a reverse-sorted list of
	// mounts to ensure we can unmount in order.
	for i := range mounts {
		if err := unmount(mounts[len(mounts)-i-1]); err != nil {
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

// FIXME: would it make sense to rename to something like
//         "UmountAndRemoveFromMountList" to indicate it has side-effects?
// Mount the given directory and add it to the "mounts" slice
func mount(source, target, options string) (err error) {
	var args []string

	args = append(args, "/bin/mount")
	if options != "" {
		args = append(args, fmt.Sprintf("-o%s", options))
	}

	args = append(args, source)
	args = append(args, target)

	err = runCommand(args...)

	if err == nil {
		mounts = append(mounts, target)
	}

	return err
}

// Remove the given string from the string slice
func stringSliceRemove(slice []string, needle string) (res []string) {
	// FIXME: so this is golang slice remove?!?! really?
	if pos := stringInSlice(slice, needle); pos >= 0 {
		slice = append(slice[:pos], slice[pos+1:]...)
	}
	return slice
}

// FIXME: would it make sense to rename to something like
//         "UmountAndRemoveFromMountList" to indicate it has side-effects?
// Unmount the given directory and remove it from the global "mounts" slice
func unmount(target string) (err error) {
	err = runCommand("/bin/umount", target)
	if err == nil {
		mounts = stringSliceRemove(mounts, target)
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
func fsck(device string) (err error) {
	return runCommand(
		"/sbin/fsck",
		"-M", // Paranoia - don't fsck if already mounted
		"-av", device)
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

// Returns all current mounts
var getMounts = func() (mounts []mntEnt, err error) {
	lines, err := readLines("/proc/self/mounts")
	if err != nil {
		return mounts, err
	}

	for _, line := range lines {
		fields := strings.Split(line, " ")

		options := strings.Split(fields[3], ",")

		dumpFreq, err := strconv.Atoi(fields[4])
		if err != nil {
			return mounts, err
		}

		fsckPassNo, err := strconv.Atoi(fields[5])
		if err != nil {
			return mounts, err
		}

		m := mntEnt{
			Device:     fields[0],
			MountPoint: fields[1],
			Type:       fields[2],
			Options:    options,
			DumpFreq:   dumpFreq,
			FsckPassNo: fsckPassNo,
		}

		mounts = append(mounts, m)
	}

	return mounts, err
}

// Returns a hash of recognised partitions in the following form:
//
// key: name of recognised partition.
// value: full path to disk device for the partition.
var getPartitions = func() (m map[string]string, err error) {

	recognised := allPartitionLabels()

	m = make(map[string]string)

	entries, err := ioutil.ReadDir(diskDeviceDir)
	if err != nil {
		return m, err
	}

	for _, entry := range entries {
		pos := stringInSlice(recognised, entry.Name())
		if pos < 0 {
			// ignore unrecognised partitions
			continue
		}
		isSymLink := (entry.Mode() & os.ModeSymlink) == os.ModeSymlink

		fullPath := path.Join(diskDeviceDir, entry.Name())

		if isSymLink {
			dest, err := os.Readlink(fullPath)
			if err != nil {
				return m, err
			}

			var cleaned string

			if strings.HasPrefix(dest, "..") {
				cleaned = path.Clean(path.Join(diskDeviceDir, dest))
			} else {
				cleaned = path.Clean(dest)
			}

			fullPath = cleaned
		}

		m[entry.Name()] = fullPath
	}

	return m, err
}

// Determine details of the recognised disk partitions
// available on the system via lsblk
var loadPartitionDetails = func() (partitions []blockDevice, err error) {

	pm, err := getPartitions()
	if err != nil {
		return partitions, err
	}

	mounts, err := getMounts()

	diskPattern := regexp.MustCompile(`(/dev/[^\d]*)\d+`)

	for name, device := range pm {
		// convert partition device to parent disk device by stripping
		// off the numeric partition number.
		// FIXME: this is crude and probably fragile.
		matches := diskPattern.FindAllStringSubmatch(device, -1)

		if matches == nil {
			return partitions,
				errors.New(fmt.Sprintf("failed to find disk associated with partition %q", device))
		}

		disk := matches[0][1]

		if !fileExists(disk) {
			continue
		}

		mountPoint := getMountPoint(mounts, device)

		bd := blockDevice{
			name:       name,
			device:     device,
			mountpoint: mountPoint,
			parentName: disk,
		}

		partitions = append(partitions, bd)
	}

	return partitions, err
}

func (p *Partition) makeMountPoint() (err error) {

	return os.MkdirAll(p.MountTarget(), DIR_MODE)
}

// Constructor
func New() *Partition {
	p := new(Partition)

	p.getPartitionDetails()
	p.hardwareSpecFile = path.Join(p.cacheDir(), HARDWARE_SPEC_FILE)

	return p
}

var NoDualPartitionError = errors.New("No dual partition")

// Mount the other rootfs partition, execute the specified function and
// unmount "other" before returning. If "other" is mounted read-write,
// /proc, /sys and /dev will also be bind-mounted at the time the
// specified function is called.
func (p *Partition) RunWithOther(option MountOption, f func(otherRoot string) (err error)) (err error) {
	dual := p.dualRootPartitions()

	if !dual {
		return NoDualPartitionError
	}

	if option == RW {
		if err = p.remountOther(RW); err != nil {
			return err
		}

		defer func() {
			err = p.remountOther(RO)
		}()

		if err = p.bindmountRequiredFilesystems(); err != nil {
			return err
		}

		defer func() {
			err = p.unmountRequiredFilesystems()
		}()
	}

	return f(p.MountTarget())
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
		if b.Installed() {
			return b, err
		}
	}

	return nil, BootloaderError
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

func (p *Partition) hardwareSpec() (hardware hardwareSpecType, err error) {
	h := hardwareSpecType{}

	data, err := ioutil.ReadFile(p.hardwareSpecFile)
	if err != nil {
		return h, err
	}

	err = yaml.Unmarshal([]byte(data), &h)

	return h, err
}

// Return full path to the main assets directory
func (p *Partition) assetsDir() string {
	return path.Join(p.cacheDir(), ASSETS_DIR)
}

// Return the full path to the hardware-specific flash assets directory.
func (p *Partition) flashAssetsDir() string {
	return path.Join(p.cacheDir(), FLASH_ASSETS_DIR)
}

// Get the full path to the mount target directory
func (p *Partition) MountTarget() string {
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

	if p.dualRootPartitions() {
		// XXX: this will soon be handled automatically at boot by
		// initramfs-tools-ubuntu-core.
		return p.ensureOtherMountedRO()
	}

	return err
}

// Return array of blockDevices representing available root partitions
func (p *Partition) rootPartitions() (roots []blockDevice) {
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

// Return pointer to blockDevice representing writable partition
func (p *Partition) writablePartition() (result *blockDevice) {
	for _, part := range p.partitions {
		if part.name == WRITABLE_PARTITION_LABEL {
			return &part
		}
	}

	return result
}

// Return pointer to blockDevice representing boot partition (if any)
func (p *Partition) bootPartition() (result *blockDevice) {
	for _, part := range p.partitions {
		if part.name == BOOT_PARTITION_LABEL {
			return &part
		}
	}

	return result
}

// Return pointer to blockDevice representing currently mounted root
// filesystem
func (p *Partition) rootPartition() (result *blockDevice) {
	for _, part := range p.rootPartitions() {
		if part.mountpoint == "/" {
			return &part
		}
	}

	return result
}

// Return pointer to blockDevice representing the "other" root
// filesystem (which is not currently active)
func (p *Partition) otherRootPartition() (result *blockDevice) {
	for _, part := range p.rootPartitions() {
		if part.mountpoint != "/" {
			return &part
		}
	}

	return result
}

// Mount the "other" root filesystem
func (p *Partition) mountOtherRootfs(readOnly bool) (err error) {
	var other *blockDevice

	p.makeMountPoint()

	other = p.otherRootPartition()

	if readOnly {
		err = mount(other.device, p.MountTarget(), "ro")
	} else {
		err = fsck(other.device)
		if err != nil {
			return err
		}
		err = mount(other.device, p.MountTarget(), "")
	}

	return err
}

// Ensure the other partition is mounted read-only.
func (p *Partition) ensureOtherMountedRO() (err error) {
	mountpoint := p.MountTarget()

	if err = runCommand("/bin/mountpoint", mountpoint); err == nil {
		// already mounted
		return err
	}

	return p.mountOtherRootfs(true)
}

// Remount the already-mounted other partition. Whether the mount
// should become writable is specified by the writable argument.
//
// XXX: Note that in the case where writable=true, this isn't a simple
// toggle - if the partition is already mounted read-only, it needs to
// be unmounted, fsck(8)'d, then (re-)mounted read-write.
func (p *Partition) remountOther(option MountOption) (err error) {
	other := p.otherRootPartition()

	if option == RW {
		// r/o -> r/w: initially r/o, so no need to fsck before
		// switching to r/w.
		err = p.unmountOtherRootfs()
		if err != nil {
			return err
		}

		err = fsck(other.device)
		if err != nil {
			return err
		}

		return mount(other.device, p.MountTarget(), "")
	} else {
		// r/w -> r/o: no fsck required.
		return mount(other.device, p.MountTarget(), "remount,ro")
	}
}

func (p *Partition) unmountOtherRootfs() (err error) {
	return unmount(p.MountTarget())
}

// The bootloader requires a few filesystems to be mounted when
// run from within a chroot.
func (p *Partition) bindmountRequiredFilesystems() (err error) {
	var boot *blockDevice

	for _, fs := range requiredChrootMounts() {
		target := path.Join(p.MountTarget(), fs)

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

	target := path.Join(p.MountTarget(), boot.mountpoint)
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

func (p *Partition) toggleBootloaderRootfs() (err error) {

	if !p.dualRootPartitions() {
		return errors.New("System is not dual root")
	}

	bootloader, err := p.GetBootloader()
	if err != nil {
		return err
	}

	err = p.RunWithOther(RW, func(otherRoot string) (err error) {
		return bootloader.ToggleRootFS()
	})

	if err != nil {
		return err
	}

	return bootloader.HandleAssets()
}

// Run the commandline specified by the args array chrooted to the
// new root filesystem.
func (p *Partition) runInChroot(args []string) (err error) {
	fullArgs := []string{"/usr/sbin/chroot", p.MountTarget()}
	fullArgs = append(fullArgs, args...)

	return runCommand(fullArgs...)
}
