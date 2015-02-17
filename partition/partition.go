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

// Package partition manipulate disk partitions.
//
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
	"strings"
	"syscall"

	yaml "launchpad.net/goyaml"
)

var debug = false

var signalHandlerRegistered = false

// Name of writable user data partition label as created by
// ubuntu-device-flash(1).
const writablePartitionLabel = "writable"

// Name of primary root filesystem partition label as created by
// ubuntu-device-flash(1).
const rootfsAlabel = "system-a"

// Name of primary root filesystem partition label as created by
// ubuntu-device-flash(1). Note that this partition will
// only be present if this is an A/B upgrade system.
const rootfsBlabel = "system-b"

// name of boot partition label as created by ubuntu-device-flash(1).
const bootPartitionLabel = "system-boot"

// FIXME: Should query system-image-cli (see bug LP:#1380574).
const defaultCacheDir = "/writable/cache"

// Directory to mount writable root filesystem below the cache
// diretory.
const mountTarget = "system"

// File creation mode used when any directories are created
const dirMode = 0750

var (
	// ErrBootloader is returned if the bootloader can not be determined
	ErrBootloader = errors.New("Unable to determine bootloader")

	// ErrPartitionDetection is returned if the partition type can not
	// be detected
	ErrPartitionDetection = errors.New("Failed to detect system type")

	// ErrNoDualPartition is returned if you try to use a dual
	// partition feature on a single partition
	ErrNoDualPartition = errors.New("No dual partition")
)

// Declarative specification of the type of system which specifies such
// details as:
//
// - the location of initrd+kernel within the system-image archive.
// - the location of hardware-specific .dtb files within the
//   system-image archive.
// - the type of bootloader that should be used for this system.
// - expected system partition layout (single or dual rootfs's).
const hardwareSpecFile = "hardware.yaml"

// Directory that _may_ get automatically created on unpack that
// contains updated hardware-specific boot assets (such as initrd, kernel)
const assetsDir = "assets"

// Directory that _may_ get automatically created on unpack that
// contains updated hardware-specific assets that require flashing
// to the disk (such as uBoot, MLO)
const flashAssetsDir = "flashtool-assets"

//--------------------------------------------------------------------
// FIXME: Globals

// list of current mounts that this module has created
var mounts []string

// list of current bindmounts this module has created
var bindMounts []string

//--------------------------------------------------------------------

// MountOption represents how the partition should be mounted, currently
// RO (read-only) and RW (read-write) are supported
type MountOption int

const (
	// RO mounts the partition read-only
	RO MountOption = iota
	// RW mounts the partition read-only
	RW
)

// Interface provides the interface to interact with a partition
type Interface interface {
	UpdateBootloader() (err error)
	MarkBootSuccessful() (err error)
	// FIXME: could we make SyncBootloaderFiles part of UpdateBootloader
	//        to expose even less implementation details?
	SyncBootloaderFiles() (err error)
	IsNextBootOther() bool

	// run the function f with the otherRoot mounted
	RunWithOther(rw MountOption, f func(otherRoot string) (err error)) (err error)
}

// Partition is the type to interact with the partition
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

// Representation of HARDWARE_SPEC_FILE
type hardwareSpecType struct {
	Kernel          string         `yaml:"kernel"`
	Initrd          string         `yaml:"initrd"`
	DtbDir          string         `yaml:"dtbs"`
	PartitionLayout string         `yaml:"partition-layout"`
	Bootloader      bootloaderName `yaml:"bootloader"`
}

func init() {
	if os.Getenv("SNAPPY_DEBUG") != "" {
		debug = true
	}

	if !signalHandlerRegistered {
		setupSignalHandler()
		signalHandlerRegistered = true
	}
}

func undoMounts(mounts []string) (err error) {
	// Iterate backwards since we want a reverse-sorted list of
	// mounts to ensure we can unmount in order.
	for i := range mounts {
		if err := unmountAndRemoveFromGlobalMountList(mounts[len(mounts)-i-1]); err != nil {
			return err
		}
	}

	return err
}

func signalHandler(sig os.Signal) {
	err := undoMounts(mounts)
	if err != nil {
		// FIXME: use logger
		fmt.Fprintf(os.Stderr, "ERROR: failed to unmount: %s", err)
	}
}

func setupSignalHandler() {
	ch := make(chan os.Signal, 1)

	// add the signals we care about
	signal.Notify(ch, os.Interrupt)
	signal.Notify(ch, syscall.SIGTERM)

	go func() {
		// block waiting for a signal
		sig := <-ch

		// handle it
		signalHandler(sig)
		os.Exit(1)
	}()
}

// Returns a list of root filesystem partition labels
func rootPartitionLabels() []string {
	return []string{rootfsAlabel, rootfsBlabel}
}

// Returns a list of all recognised partition labels
func allPartitionLabels() []string {
	var labels []string

	labels = rootPartitionLabels()
	labels = append(labels, bootPartitionLabel)
	labels = append(labels, writablePartitionLabel)

	return labels
}

func mount(source, target, options string) (err error) {
	var args []string

	args = append(args, "/bin/mount")
	if options != "" {
		args = append(args, fmt.Sprintf("-o%s", options))
	}

	args = append(args, source)
	args = append(args, target)

	return runCommand(args...)
}

// Mount the given directory and add it to the "mounts" slice
func mountAndAddToGlobalMountList(source, target, options string) (err error) {
	err = mount(source, target, options)
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
func unmountAndRemoveFromGlobalMountList(target string) (err error) {
	err = runCommand("/bin/umount", target)
	if err == nil {
		mounts = stringSliceRemove(mounts, target)
	}

	return err
}

func bindmountAndAddToGlobalMountList(source, target string) (err error) {
	err = mountAndAddToGlobalMountList(source, target, "bind")

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

var runLsblk = func() (out []string, err error) {
	output, err := runCommandWithStdout(
		"/bin/lsblk",
		"--ascii",
		"--output=NAME,LABEL,PKNAME,MOUNTPOINT",
		"--pairs")
	if err != nil {
		return out, err
	}

	return strings.Split(output, "\n"), nil
}

// Determine details of the recognised disk partitions
// available on the system via lsblk
func loadPartitionDetails() (partitions []blockDevice, err error) {
	recognised := allPartitionLabels()

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
		if !ok {
			continue
		}

		if name == "" || name == "\"\"" {
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
		bd := blockDevice{
			name:       fields["LABEL"],
			device:     device,
			mountpoint: fields["MOUNTPOINT"],
			parentName: disk,
		}

		partitions = append(partitions, bd)
	}

	return partitions, nil
}

func (p *Partition) makeMountPoint() (err error) {

	return os.MkdirAll(p.MountTarget(), dirMode)
}

// New creates a new partition type
func New() *Partition {
	p := new(Partition)

	p.getPartitionDetails()
	p.hardwareSpecFile = path.Join(p.cacheDir(), hardwareSpecFile)

	return p
}

// RunWithOther mount the other rootfs partition, execute the
// specified function and unmount "other" before returning. If "other"
// is mounted read-write, /proc, /sys and /dev will also be
// bind-mounted at the time the specified function is called.
func (p *Partition) RunWithOther(option MountOption, f func(otherRoot string) (err error)) (err error) {
	dual := p.dualRootPartitions()

	if !dual {
		return ErrNoDualPartition
	}

	if option == RW {
		if err := p.remountOther(RW); err != nil {
			return err
		}

		defer func() {
			// we can't reuse err here as this will override
			// the error value we got from calling "f()"
			derr := p.remountOther(RO)
			if derr != nil && err == nil {
				err = derr
			}
		}()

		if err := p.bindmountRequiredFilesystems(); err != nil {
			return err
		}

		defer func() {
			// we can't reuse err here as this will override
			// the error value we got from calling "f()"
			derr := p.unmountRequiredFilesystems()
			if derr != nil && err == nil {
				err = derr
			}
		}()
	}

	err = f(p.MountTarget())
	return err
}

// SyncBootloaderFiles syncs the bootloader files
// FIXME: can we unexport this?
func (p *Partition) SyncBootloaderFiles() (err error) {
	bootloader, err := getBootloader(p)
	if err != nil {
		return err
	}
	return bootloader.SyncBootFiles()
}

// UpdateBootloader toggles the bootloader and should probably called
// ToggleBootloader
func (p *Partition) UpdateBootloader() (err error) {
	if p.dualRootPartitions() {
		return p.toggleBootloaderRootfs()
	}
	return err
}

// MarkBootSuccessful marks the boot as successful
func (p *Partition) MarkBootSuccessful() (err error) {
	bootloader, err := getBootloader(p)
	if err != nil {
		return err
	}

	return bootloader.MarkCurrentBootSuccessful()
}

// IsNextBootOther return true if the next boot will use the other rootfs
// partition.
func (p *Partition) IsNextBootOther() bool {
	bootloader, err := getBootloader(p)
	if err != nil {
		return false
	}
	return isNextBootOther(bootloader)
}

// Returns the full path to the cache directory, which is used as a
// scratch pad, for downloading new images to and bind mounting the
// rootfs.
func (p *Partition) cacheDir() string {
	return defaultCacheDir
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
	return path.Join(p.cacheDir(), assetsDir)
}

// Return the full path to the hardware-specific flash assets directory.
func (p *Partition) flashAssetsDir() string {
	return path.Join(p.cacheDir(), flashAssetsDir)
}

// MountTarget gets the full path to the mount target directory
func (p *Partition) MountTarget() string {
	return path.Join(p.cacheDir(), mountTarget)
}

func (p *Partition) getPartitionDetails() (err error) {
	p.partitions, err = loadPartitionDetails()
	if err != nil {
		return err
	}

	if !p.dualRootPartitions() && !p.singleRootPartition() {
		return ErrPartitionDetection
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
		if part.name == writablePartitionLabel {
			return &part
		}
	}

	return result
}

// Return pointer to blockDevice representing boot partition (if any)
func (p *Partition) bootPartition() (result *blockDevice) {
	for _, part := range p.partitions {
		if part.name == bootPartitionLabel {
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
// filesystem (which is not currently mounted)
func (p *Partition) otherRootPartition() (result *blockDevice) {
	for _, part := range p.rootPartitions() {
		if part.mountpoint != "/" {
			return &part
		}
	}

	return nil
}

// Mount the "other" root filesystem
func (p *Partition) mountOtherRootfs(readOnly bool) (err error) {
	var other *blockDevice

	p.makeMountPoint()

	other = p.otherRootPartition()

	if readOnly {
		err = mountAndAddToGlobalMountList(other.device, p.MountTarget(), "ro")
	} else {
		err = fsck(other.device)
		if err != nil {
			return err
		}
		err = mountAndAddToGlobalMountList(other.device, p.MountTarget(), "")
	}

	return err
}

// Create a read-only bindmount of the currently-mounted rootfs at the
// specified mountpoint location (which must already exist).
func (p *Partition) bindmountThisRootfsRO(target string) (err error) {
	return mountAndAddToGlobalMountList("/", target, "bind,ro")
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
	}
	// r/w -> r/o: no fsck required.
	return mount(other.device, p.MountTarget(), "remount,ro")
}

func (p *Partition) unmountOtherRootfs() (err error) {
	return unmountAndRemoveFromGlobalMountList(p.MountTarget())
}

// The bootloader requires a few filesystems to be mounted when
// run from within a chroot.
func (p *Partition) bindmountRequiredFilesystems() (err error) {

	// we always requires these
	requiredChrootMounts := []string{"/dev", "/proc", "/sys"}

	// if there is a boot partition we also bind-mount it
	boot := p.bootPartition()
	if boot != nil && boot.mountpoint != "" {
		requiredChrootMounts = append(requiredChrootMounts, boot.mountpoint)
	}

	for _, fs := range requiredChrootMounts {
		target := path.Join(p.MountTarget(), fs)

		err := bindmountAndAddToGlobalMountList(fs, target)
		if err != nil {
			return err
		}
	}

	// Grub also requires access to both rootfs's when run from
	// within a chroot (to allow it to create menu entries for
	// both), so bindmount the real rootfs.
	targetInChroot := path.Join(p.MountTarget(), p.MountTarget())

	// FIXME: we should really remove this after the unmount
	os.MkdirAll(targetInChroot, dirMode)

	return p.bindmountThisRootfsRO(targetInChroot)
}

// Undo the effects of BindmountRequiredFilesystems()
func (p *Partition) unmountRequiredFilesystems() (err error) {
	return undoMounts(bindMounts)
}

func (p *Partition) toggleBootloaderRootfs() (err error) {

	if !p.dualRootPartitions() {
		return errors.New("System is not dual root")
	}

	bootloader, err := getBootloader(p)
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
