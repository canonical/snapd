//--------------------------------------------------------------------
// Copyright (c) 2014 Canonical Ltd.
//--------------------------------------------------------------------
// TODO:
//
// - XXX: implement apt_pkg.version_compare() !?!
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
// The main callables are UpdateBootLoader() and GetOtherVersion().
package partition

import (
	"container/list"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

// FIXME: leverage u-d-f once it's part of snappy
const DEVICE_TYPE = "generic_amd64"

var debug bool = false

var signal_handler_registered bool = false

// Name of writable user data partition label as created by
// ubuntu-device-flash(1).
const WRITABLE_PARTITION_LABEL = "writable"

const (
	SYSTEM_TYPE_SINGLE_ROOT = iota // in-place upgrades
	SYSTEM_TYPE_DUAL_ROOT          // A/B partitions
	systemTypeCount
)

// Name of primary root filesystem partition label as created by
// ubuntu-device-flash(1).
const ROOTFS_A_LABEL = "system-a"

// Name of primary root filesystem partition label as created by
// ubuntu-device-flash(1). Note that this partition will
// only be present if this is an A/B upgrade system.
const ROOTFS_B_LABEL = "system-b"

// name of boot partition label as created by ubuntu-device-flash(1).
const BOOT_PARTITION_LABEL = "boot"

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

//--------------------------------------------------------------------
// Globals

// list of current mounts that this module has created
var mounts *list.List

// list of current bindmounts this module has created
var bindMounts *list.List

//--------------------------------------------------------------------

type Partition struct {
	// all partitions
	partitions []BlockDevice

	// just root partitions
	roots []string

	// whether the rootfs that is to be modified be mounted writable
	ReadOnlyRoot bool

	MountTarget string

	SystemType int
}

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

type SystemImageVersion struct {
	// Currently, system-image versions are revision numbers.
	// However this is soon to changes so wrap the representation in a
	// struct.
	Version int
}

func init() {
	if os.Getenv("SNAPPY_DEBUG") != "" {
		debug = true
	}

	if signal_handler_registered == false {
		setup_signal_handler()
		signal_handler_registered = true
	}

	mounts = list.New()
	bindMounts = list.New()
}

// Remove specified element from the specified list
func listRemove(l *list.List, victim string) {

	var toZap *list.Element = nil

	for e := l.Front(); e != nil; e = e.Next() {

		if e.Value == victim {
			toZap = e
			break
		}
	}

	if toZap != nil {
		l.Remove(toZap)
	}
}

func listAdd(l *list.List, value string) {
	l.PushBack(value)
}

// Unmount all mounts created by this program
func undoMounts(mounts *list.List) (err error) {
	var targets []string

	// Convert the list back into a slice. Iterate backwards since we
	// want a reverse-sorted list of mounts to ensure we can unmount in
	// order.
	for e := mounts.Back(); e != nil; e = e.Prev() {
		if target, ok := e.Value.(string); ok {
			targets = append(targets, target)
		}
	}

	for _, target := range targets {
		err := Unmount(target)
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

// Constructor
func New() *Partition {
	p := new(Partition)

	p.ReadOnlyRoot = false
	p.MountTarget = p.getMountTarget()
	p.getPartitionDetails()

	return p
}

// Returns the full path to the cache directory, which is used as a
// scratch pad, for downloading new images to and bind mounting the
// rootfs.
func (p *Partition) CacheDir() string {
	return DEFAULT_CACHE_DIR
}

// Get the full path to the mount target directory
func (p *Partition) getMountTarget() string {
	return path.Join(p.CacheDir(), MOUNT_TARGET)
}

// Returns a list of root filesystem partition labels
func (p *Partition) RootPartitionLabels() []string {
	return []string{ROOTFS_A_LABEL, ROOTFS_B_LABEL}
}

// Returns a list of all recognised partition labels
func (p *Partition) AllPartitionLabels() []string {
	var labels []string

	labels = p.RootPartitionLabels()
	labels = append(labels, BOOT_PARTITION_LABEL)
	labels = append(labels, WRITABLE_PARTITION_LABEL)

	return labels
}

func (p *Partition) getPartitionDetails() {
	if p.partitions != nil {
		return
	}

	err := p.loadPartitionDetails()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to query partitions")
		os.Exit(1)
	}

	p.determineSystemType()
}

func (p *Partition) determineSystemType() {
	if p.DualRootPartitions() == true {
		p.SystemType = SYSTEM_TYPE_DUAL_ROOT
	} else if p.SinglelRootPartition() == true {
		p.SystemType = SYSTEM_TYPE_SINGLE_ROOT
	} else {
		panic("Unrecognised system type")
	}
}

// Return array of BlockDevices representing available root partitions
func (p *Partition) RootPartitions() []BlockDevice {
	var roots []BlockDevice

	for _, part := range p.partitions {
		ok := stringInSlice(p.RootPartitionLabels(), part.name)
		if ok == true {
			roots = append(roots, part)
		}
	}

	return roots
}

// Return true if system has dual root partitions configured in the
// expected manner for a snappy system.
func (p *Partition) DualRootPartitions() bool {
	return len(p.RootPartitions()) == 2
}

// Return true if system has a single root partition configured in the
// expected manner for a snappy system.
func (p *Partition) SinglelRootPartition() bool {
	return len(p.RootPartitions()) == 1
}

// Return pointer to BlockDevice representing writable partition
func (p *Partition) WritablePartition() (result *BlockDevice) {
	for _, part := range p.partitions {
		if part.name == WRITABLE_PARTITION_LABEL {
			return &part
		}
	}

	return result
}

// Return pointer to BlockDevice representing boot partition (if any)
func (p *Partition) BootPartition() (result *BlockDevice) {
	for _, part := range p.partitions {
		if part.name == BOOT_PARTITION_LABEL {
			return &part
		}
	}

	return result
}

// Return pointer to BlockDevice representing currently mounted root
// filesystem
func (p *Partition) RootPartition() (result *BlockDevice) {
	for _, part := range p.RootPartitions() {
		if part.mountpoint == "/" {
			return &part
		}
	}

	return result
}

// Return pointer to BlockDevice representing the "other" root
// filesystem (which is not currently mounted)
func (p *Partition) OtherRootPartition() (result *BlockDevice) {
	for _, part := range p.RootPartitions() {
		if part.mountpoint != "/" {
			return &part
		}
	}

	return result
}

// Returns a minimal list of mounts required for running grub-install
// within a chroot.
func (p *Partition) GetRequiredChrootMounts() []string {
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

func Mount(source, target, options string) (err error) {
	var args []string

	args = append(args, "/bin/mount")

	if options != "" {
		args = append(args, fmt.Sprintf("-o%s", options))
	}

	args = append(args, source)
	args = append(args, target)

	err = RunCommand(args)

	if err == nil {
		listAdd(mounts, target)
	}

	return err
}

func Unmount(target string) (err error) {
	var args []string

	args = append(args, "/bin/umount")
	args = append(args, target)

	err = RunCommand(args)

	if err == nil {
		listRemove(mounts, target)
	}

	return err
}

func BindMount(source, target string) (err error) {
	err = Mount(source, target, "bind")

	if err == nil {
		listAdd(bindMounts, target)
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

// Returns true if value existing in the specfied slice
func stringInSlice(slice []string, value string) bool {
	for _, s := range slice {
		if s == value {
			return true
		}
	}

	return false
}

// Determine details of the recognised disk partitions
// available on the system.
func (p *Partition) loadPartitionDetails() (err error) {
	var args []string

	var recognised []string = p.AllPartitionLabels()

	args = append(args, "/bin/lsblk")
	args = append(args, "--ascii")
	args = append(args, "--output-all")
	args = append(args, "--pairs")

	lines, err := GetCommandStdout(args)
	if err != nil {
		return err
	}

	pattern := regexp.MustCompile(`(?:[^\s"]|"(?:[^"])*")+`)

	for _, line := range lines {
		var fields map[string]string = make(map[string]string)

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

		ok = stringInSlice(recognised, name)
		if ok == false {
			// ignore unrecognised partitions
			continue
		}

		// reconstruct full path to disk partition device
		device := fmt.Sprintf("/dev/%s", fields["NAME"])

		if err := FileExists(device); err != nil {
			continue
		}

		// reconstruct full path to entire disk device
		disk := fmt.Sprintf("/dev/%s", fields["PKNAME"])

		if err := FileExists(disk); err != nil {
			continue
		}

		bd := BlockDevice{
			name:       fields["LABEL"],
			device:     device,
			mountpoint: fields["MOUNTPOINT"],
			parentName: disk,
		}

		p.partitions = append(p.partitions, bd)
	}

	return nil
}

func (p *Partition) MakeMountPoint() (err error) {
	if p.ReadOnlyRoot == true {
		// A system update "owns" the default mount_target directory.
		// So in read-only mode, use a temporary mountpoint name to
		// avoid colliding with a running system update.
		p.MountTarget = fmt.Sprintf("%s-ro-%d",
		p.MountTarget,
		os.Getpid())
	}

	return os.MkdirAll(p.MountTarget, DIR_MODE)
}

// Mount the "other" root filesystem
func (p *Partition) MountOtherRootfs() (err error) {
	var other *BlockDevice

	p.MakeMountPoint()

	other = p.OtherRootPartition()

	if p.ReadOnlyRoot == true {
		err = Mount(other.device, p.MountTarget, "ro")
	} else {
		err = Fsck(other.device)
		if err != nil {
			return err
		}
		err = Mount(other.device, p.MountTarget, "")
	}

	return err
}

func (p *Partition) UnmountOtherRootfs() (err error) {
	return Unmount(p.MountTarget)
}

// The bootloader requires a few filesystems to be mounted when
// run from within a chroot.
func (p *Partition) BindmountRequiredFilesystems() (err error) {
	var boot *BlockDevice

	for _, fs := range p.GetRequiredChrootMounts() {
		target := path.Clean(fmt.Sprintf("%s/%s", p.MountTarget, fs))

		err := BindMount(fs, target)
		if err != nil {
			return err
		}
	}

	boot = p.BootPartition()
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
	err = BindMount(boot.mountpoint, target)
	if err != nil {
		return err
	}

	return err
}

// Undo the effects of BindmountRequiredFilesystems()
func (p *Partition) UnmountRequiredFilesystems() (err error) {
	return undoMounts(bindMounts)
}

// Run the commandline specified by the args array chrooted to the
// new root filesystem.
//
// Errors are fatal.
func (p *Partition) RunInChroot(args []string) (err error) {
	var fullArgs []string

	fullArgs = append(fullArgs, "/usr/sbin/chroot")
	fullArgs = append(fullArgs, p.MountTarget)

	fullArgs = append(fullArgs, args...)

	return RunCommand(fullArgs)
}

func (p *Partition) HandleBootloader() (err error) {
	bootloader := DetermineBootLoader(p)

	// FIXME: use logger
	fmt.Printf("FIXME: HandleBootloader: bootloader=%s\n", bootloader.Name())

	return bootloader.ToggleRootFS()
}

func (p *Partition) GetOtherVersion() (version SystemImageVersion, err error) {

	// XXX: note that we mount read-only here
	p.ReadOnlyRoot = true
	err = p.MountOtherRootfs()

	if err != nil {
		return version, err
	}

	// Unmount
	defer func() {
		err = p.UnmountOtherRootfs()
	}()

	// Remount mountpoint
	defer func() {
		err = os.Remove(p.MountTarget)
	}()

	// construct full path to other partitions system-image master
	// config file
	file := path.Clean(fmt.Sprintf("%s/%s", p.MountTarget, SYSTEM_IMAGE_CONFIG))

	if err = FileExists(file); err != nil {
		return version, err
	}

	var args []string

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

	value, err2 := strconv.Atoi(revision)

	if err2 != nil {
		return version, err2
	}

	version.Version = value

	return version, err
}

// Currently, system-image-cli(1) does not provide 'version_string'
// in its verbatim form, hence grope for it for now.
func (p *Partition) GetOtherVersionDetail() (detail string, err error) {
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

func (p *Partition) UpdateBootloader() (err error) {
	switch p.SystemType {
	case SYSTEM_TYPE_SINGLE_ROOT:
		// NOP
		return nil
	case SYSTEM_TYPE_DUAL_ROOT:
		return p.ToggleBootloaderRootfs()
	default: panic("BUG: unhandled SystemType")
	}
}

func (p *Partition) ToggleBootloaderRootfs() (err error) {

	if p.DualRootPartitions() != true {
		return errors.New("System is not dual root")
	}

	if err = p.MountOtherRootfs(); err != nil {
		return err
	}

	if err = p.BindmountRequiredFilesystems(); err != nil {
		return err
	}

	if err = p.HandleBootloader(); err != nil {
		return err
	}

	if err = p.UnmountRequiredFilesystems(); err != nil {
		return err
	}

	if err = p.UnmountOtherRootfs(); err != nil {
		return err
	}

	return err
}
