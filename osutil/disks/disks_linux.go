// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package disks

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

var _ = Disk(&disk{})

var (
	// this regexp is for the DM_UUID udev property, or equivalently the dm/uuid
	// sysfs entry for a luks2 device mapper volume dynamically created by
	// systemd-cryptsetup when unlocking
	// the actual value that is returned also has "-some-name" appended to this
	// pattern, but we delete that from the string before matching with this
	// regexp to prevent issues like a mapper volume name that has CRYPT-LUKS2-
	// in the name and thus we might accidentally match it
	// see also the comments in DiskFromMountPoint about this value
	luksUUIDPatternRe = regexp.MustCompile(`^CRYPT-LUKS2-([0-9a-f]{32})$`)
)

// diskFromMountPoint is exposed for mocking from other tests via
// MockMountPointDisksToPartitionMapping, but we can't just assign
// diskFromMountPointImpl to diskFromMountPoint due to signature differences,
// the former returns a *disk, the latter returns a Disk
var diskFromMountPoint = func(mountpoint string, opts *Options) (Disk, error) {
	return diskFromMountPointImpl(mountpoint, opts)
}

func parseDeviceMajorMinor(s string) (int, int, error) {
	errMsg := fmt.Errorf("invalid device number format: (expected <int>:<int>)")
	devNums := strings.SplitN(s, ":", 2)
	if len(devNums) != 2 {
		return 0, 0, errMsg
	}
	maj, err := strconv.Atoi(devNums[0])
	if err != nil {
		return 0, 0, errMsg
	}
	min, err := strconv.Atoi(devNums[1])
	if err != nil {
		return 0, 0, errMsg
	}
	return maj, min, nil
}

var udevadmProperties = func(typeOpt, device string) ([]byte, error) {
	// TODO: maybe combine with gadget interfaces hotplug code where the udev
	// db is parsed?
	cmd := exec.Command("udevadm", "info", "--query", "property", typeOpt, device)
	return cmd.CombinedOutput()
}

func udevProperties(typeOpt, device string) (map[string]string, error) {
	out, err := udevadmProperties(typeOpt, device)
	if err != nil {
		return nil, osutil.OutputErr(out, err)
	}
	r := bytes.NewBuffer(out)

	return parseUdevProperties(r)
}

func udevPropertiesForPath(devicePath string) (map[string]string, error) {
	return udevProperties("--path", devicePath)
}

func udevPropertiesForName(deviceName string) (map[string]string, error) {
	return udevProperties("--name", deviceName)
}

func parseUdevProperties(r io.Reader) (map[string]string, error) {
	m := make(map[string]string)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		strs := strings.SplitN(scanner.Text(), "=", 2)
		if len(strs) != 2 {
			// bad udev output?
			continue
		}
		m[strs[0]] = strs[1]
	}

	return m, scanner.Err()
}

func diskFromUdevProps(deviceIdentifier string, deviceIDType string, props map[string]string) (Disk, error) {
	major, err := strconv.Atoi(props["MAJOR"])
	if err != nil {
		return nil, fmt.Errorf("cannot find disk with %s %q: malformed udev output", deviceIDType, deviceIdentifier)
	}
	minor, err := strconv.Atoi(props["MINOR"])
	if err != nil {
		return nil, fmt.Errorf("cannot find disk with %s %q: malformed udev output", deviceIDType, deviceIdentifier)
	}

	// ensure that the device has DEVTYPE=disk, if not then we were not given a
	// disk name
	devType := props["DEVTYPE"]
	if devType != "disk" {
		return nil, fmt.Errorf("device %q is not a disk, it has DEVTYPE of %q", deviceIdentifier, devType)
	}

	devname := props["DEVNAME"]
	if devname == "" {
		return nil, fmt.Errorf("cannot find disk with %s %q: malformed udev output missing property \"DEVNAME\"", deviceIDType, deviceIdentifier)
	}

	devpath := props["DEVPATH"]
	if devpath == "" {
		return nil, fmt.Errorf("cannot find disk with %s %q: malformed udev output missing property \"DEVPATH\"", deviceIDType, deviceIdentifier)
	}
	// create the full path by pre-pending /sys, since udev doesn't include /sys
	devpath = filepath.Join(dirs.SysfsDir, devpath)

	// check if the device has partitions by attempting to actually search for
	// them in /sys with the DEVPATH and DEVNAME

	paths, err := filepath.Glob(filepath.Join(devpath, filepath.Base(devname)+"*"))
	if err != nil {
		return nil, fmt.Errorf("internal error with glob pattern: %v", err)
	}

	return &disk{
		major:         major,
		minor:         minor,
		devname:       devname,
		devpath:       devpath,
		hasPartitions: len(paths) != 0,
	}, nil
}

// DiskFromDeviceName finds a matching Disk using the specified path in the
// kernel's sysfs, such as /sys/devices/pci0000:00/0000:00:04.0/virtio2/block/vdb.
func DiskFromDevicePath(devicePath string) (Disk, error) {
	return diskFromDevicePath(devicePath)
}

// diskFromDevicePath is exposed for mocking from other tests via
// MockDeviceNameDisksToPartitionMapping.
var diskFromDevicePath = func(devicePath string) (Disk, error) {
	// query for the disk props using udev with --path
	props, err := udevPropertiesForPath(devicePath)
	if err != nil {
		return nil, err
	}

	return diskFromUdevProps(devicePath, "path", props)
}

// DiskFromDeviceName finds a matching Disk using the specified name, such as
// vda, or mmcblk0, etc.
func DiskFromDeviceName(deviceName string) (Disk, error) {
	return diskFromDeviceName(deviceName)
}

// diskFromDeviceName is exposed for mocking from other tests via
// MockDeviceNameDisksToPartitionMapping.
var diskFromDeviceName = func(deviceName string) (Disk, error) {
	// query for the disk props using udev with --name
	props, err := udevPropertiesForName(deviceName)
	if err != nil {
		return nil, err
	}

	return diskFromUdevProps(deviceName, "name", props)
}

func mountPointsForPartitionRoot(part Partition, mountOptsMatching map[string]string) ([]string, error) {
	mounts, err := osutil.LoadMountInfo()
	if err != nil {
		return nil, err
	}

	mountpoints := []string{}
mountLoop:
	for _, mnt := range mounts {
		if mnt.DevMajor == part.Major && mnt.DevMinor == part.Minor && mnt.Root == "/" {
			// check if mount opts match
			for key, val := range mountOptsMatching {
				candVal, ok := mnt.MountOptions[key]
				if !ok || candVal != val {
					// either the option is missing from this mount or it has a
					// different value
					continue mountLoop
				}
			}
			mountpoints = append(mountpoints, mnt.MountDir)
		}
	}

	return mountpoints, nil
}

func (d *disk) Partitions() ([]Partition, error) {
	if !d.hasPartitions {
		// for i.e. device mapper disks which don't have partitions
		return nil, nil
	}
	if err := d.populatePartitions(); err != nil {
		return nil, err
	}

	return d.partitions, nil
}

// DiskFromMountPoint finds a matching Disk for the specified mount point.
func DiskFromMountPoint(mountpoint string, opts *Options) (Disk, error) {
	// call the unexported version that may be mocked by tests
	return diskFromMountPoint(mountpoint, opts)
}

type disk struct {
	major int
	minor int

	// devname is the DEVNAME property for the disk device like /dev/sda
	devname string

	// devpath is the DEVPATH property for the disk device like
	// /sys/devices/pci0000:00/0000:00:04.0/virtio2/block/vdb.
	devpath string

	// partitions is the set of discovered partitions for the disk, each
	// partition must have a partition uuid, but may or may not have either a
	// partition label or a filesystem label
	partitions []Partition

	// whether the disk device has partitions, and thus is of type "disk", or
	// whether the disk device is a volume that is not a physical disk
	hasPartitions bool
}

func (d *disk) KernelDeviceNode() string {
	return d.devname
}

func (d *disk) KernelDevicePath() string {
	return d.devpath
}

// diskFromMountPointImpl returns a Disk for the underlying mount source of the
// specified mount point. For mount points which have sources that are not
// partitions, and thus are a part of a disk, the returned Disk refers to the
// volume/disk of the mount point itself.
func diskFromMountPointImpl(mountpoint string, opts *Options) (*disk, error) {
	// first get the mount entry for the mountpoint
	mounts, err := osutil.LoadMountInfo()
	if err != nil {
		return nil, err
	}
	var d *disk
	var partMountPointSource string
	// loop over the mount entries in reverse order to prevent shadowing of a
	// particular mount on top of another one
	for i := len(mounts) - 1; i >= 0; i-- {
		if mounts[i].MountDir == mountpoint {
			d = &disk{
				major: mounts[i].DevMajor,
				minor: mounts[i].DevMinor,
			}
			partMountPointSource = mounts[i].MountSource
			break
		}
	}
	if d == nil {
		return nil, fmt.Errorf("cannot find mountpoint %q", mountpoint)
	}

	// now we have the partition for this mountpoint, we need to tie that back
	// to a disk with a major minor, so query udev with the mount source path
	// of the mountpoint for properties
	props, err := udevPropertiesForName(partMountPointSource)
	if err != nil && props == nil {
		// only fail here if props is nil, if it's available we validate it
		// below
		return nil, fmt.Errorf("cannot find disk for partition %s: %v", partMountPointSource, err)
	}

	if opts != nil && opts.IsDecryptedDevice {
		// verify that the mount point is indeed a mapper device, it should:
		// 1. have DEVTYPE == disk from udev
		// 2. have dm files in the sysfs entry for the maj:min of the device
		if props["DEVTYPE"] != "disk" {
			// not a decrypted device
			return nil, fmt.Errorf("mountpoint source %s is not a decrypted device: devtype is not disk (is %s)", partMountPointSource, props["DEVTYPE"])
		}

		// TODO:UC20: currently, we effectively parse the DM_UUID env variable
		//            that is set for the mapper device volume, but doing so is
		//            actually wrong, since the value of DM_UUID is an
		//            implementation detail that depends on the subsystem
		//            "owner" of the device such that the prefix is considered
		//            the owner and the suffix is private data owned by the
		//            subsystem. In our case, in UC20 initramfs, we have the
		//            device "owned" by systemd-cryptsetup, so we should ideally
		//            parse that the first part of DM_UUID matches CRYPT- and
		//            then use `cryptsetup status` (since CRYPT indicates it is
		//            "owned" by cryptsetup) to get more information on the
		//            device sufficient for our purposes to find the encrypted
		//            device/partition underneath the mapper.
		//            However we don't currently have cryptsetup in the initrd,
		//            so we can't do that yet :-(

		// TODO:UC20: these files are also likely readable through udev env
		//            properties, but it's unclear if reading there is reliable
		//            or not, given that these variables have been observed to
		//            be missing from the initrd previously, and are not
		//            available at all during userspace on UC20 for some reason
		errFmt := "mountpoint source %s is not a decrypted device: could not read device mapper metadata: %v"

		dmDir := filepath.Join(dirs.SysfsDir, "dev", "block", d.Dev(), "dm")
		dmUUID, err := ioutil.ReadFile(filepath.Join(dmDir, "uuid"))
		if err != nil {
			return nil, fmt.Errorf(errFmt, partMountPointSource, err)
		}

		dmName, err := ioutil.ReadFile(filepath.Join(dmDir, "name"))
		if err != nil {
			return nil, fmt.Errorf(errFmt, partMountPointSource, err)
		}

		// trim the suffix of the dm name from the dm uuid to safely match the
		// regex - the dm uuid contains the dm name, and the dm name is user
		// controlled, so we want to remove that and just use the luks pattern
		// to match the device uuid
		// we are extra safe here since the dm name could be hypothetically user
		// controlled via an external USB disk with LVM partition names, etc.
		dmUUIDSafe := bytes.TrimSuffix(
			bytes.TrimSpace(dmUUID),
			append([]byte("-"), bytes.TrimSpace(dmName)...),
		)
		matches := luksUUIDPatternRe.FindSubmatch(dmUUIDSafe)
		if len(matches) != 2 {
			// the format of the uuid is different - different luks version
			// maybe?
			return nil, fmt.Errorf("cannot verify disk: partition %s does not have a valid luks uuid format: %s", d.Dev(), dmUUIDSafe)
		}

		// the uuid is the first and only submatch, but it is not in the same
		// format exactly as we want to use, namely it is missing all of the "-"
		// characters in a typical uuid, i.e. it is of the form:
		// ae6e79de00a9406f80ee64ba7c1966bb but we want it to be like:
		// ae6e79de-00a9-406f-80ee-64ba7c1966bb so we need to add in 4 "-"
		// characters
		compactUUID := string(matches[1])
		canonicalUUID := fmt.Sprintf(
			"%s-%s-%s-%s-%s",
			compactUUID[0:8],
			compactUUID[8:12],
			compactUUID[12:16],
			compactUUID[16:20],
			compactUUID[20:],
		)

		// now finally, we need to use this uuid, which is the device uuid of
		// the actual physical encrypted partition to get the path, which will
		// be something like /dev/vda4, etc.
		byUUIDPath := filepath.Join("/dev/disk/by-uuid", canonicalUUID)
		props, err = udevPropertiesForName(byUUIDPath)
		if err != nil {
			return nil, fmt.Errorf("cannot get udev properties for encrypted partition %s: %v", byUUIDPath, err)
		}
	}

	// ID_PART_ENTRY_DISK will give us the major and minor of the disk that this
	// partition originated from if this mount point is indeed for a partition
	if majorMinor, ok := props["ID_PART_ENTRY_DISK"]; ok {
		maj, min, err := parseDeviceMajorMinor(majorMinor)
		if err != nil {
			// bad udev output?
			return nil, fmt.Errorf("cannot find disk for partition %s, bad udev output: %v", partMountPointSource, err)
		}
		d.major = maj
		d.minor = min

		// now go find the devname and devpath for this major/minor pair since
		// we will need that later - note that the props variable at this point
		// is for the partition, not the parent disk itself, hence the
		// additional lookup
		realDiskProps, err := udevPropertiesForName(filepath.Join("/dev/block/", majorMinor))
		if err != nil {
			return nil, fmt.Errorf("cannot find disk for partition %s: %v", partMountPointSource, err)
		}

		if realDiskProps["DEVNAME"] == "" {
			return nil, fmt.Errorf("cannot find disk for partition %s: incomplete udev output missing required property \"DEVNAME\"", partMountPointSource)
		}

		d.devname = realDiskProps["DEVNAME"]

		if realDiskProps["DEVPATH"] == "" {
			return nil, fmt.Errorf("cannot find disk for partition %s: incomplete udev output missing required property \"DEVPATH\"", partMountPointSource)
		}
		// the DEVPATH is given as relative to /sys, so for simplicity's sake
		// add /sys to the path we save as we return it later
		d.devpath = filepath.Join(dirs.SysfsDir, realDiskProps["DEVPATH"])

		// since the mountpoint device has a disk, the mountpoint source itself
		// must be a partition from a disk, thus the disk has partitions
		d.hasPartitions = true
		return d, nil
	}

	// if we don't have ID_PART_ENTRY_DISK, the partition is probably a mapped
	// volume or other non-physical disk, so confirm that DEVTYPE == disk and
	// return the maj/min for it
	if devType, ok := props["DEVTYPE"]; ok {
		if devType == "disk" {
			return d, nil
		}
		// unclear what other DEVTYPE's we should support for this function
		return nil, fmt.Errorf("unsupported DEVTYPE %q for mount point source %s", devType, partMountPointSource)
	}

	return nil, fmt.Errorf("cannot find disk for partition %s, incomplete udev output", partMountPointSource)
}

func (d *disk) populatePartitions() error {
	if d.partitions == nil {
		d.partitions = []Partition{}

		// step 1. using the devpath for the disk, glob for matching devices
		//         using the devname in that sysfs directory
		// step 2. iterate over all those devices and save all the ones that are
		//         partitions using the partition sysfs file
		// step 3. for all partition devices found, query udev to get the labels
		//         of the partition and filesystem as well as the partition uuid
		//         and save for later

		// the DEVNAME as returned by udev includes the /dev/mmcblk0 path, we
		// just want mmcblk0 for example
		devName := filepath.Base(d.devname)

		// glob for d.devpath/${devName}*
		// note that d.devpath already has /sys in it from when the disk was
		// created
		paths, err := filepath.Glob(filepath.Join(d.devpath, devName+"*"))
		if err != nil {
			return fmt.Errorf("internal error getting udev properties for device %s: %v", err, d.Dev())
		}

		// Glob does not sort, so sort manually to have consistent tests
		sort.Strings(paths)

		for _, path := range paths {
			part := Partition{}

			// check if this device is a partition - note that the mere
			// existence of this file is sufficient to indicate that it is a
			// partition, the file is the partition number of the device, it
			// will be absent for pseudo sub-devices, such as the
			// /dev/mmcblk0boot0 disk device on the dragonboard which exists
			// under the /dev/mmcblk0 disk, but is not a partition and is
			// instead a proper disk
			_, err := ioutil.ReadFile(filepath.Join(path, "partition"))
			if err != nil {
				continue
			}

			// then the device is a partition, get the udev props for it
			partDev := filepath.Base(path)
			udevProps, err := udevPropertiesForName(partDev)
			if err != nil {
				continue
			}

			// the devpath should always be available
			devpath, ok := udevProps["DEVPATH"]
			if !ok {
				return fmt.Errorf("cannot get udev properties for device %s (a partition of %s), missing required udev property \"DEVPATH\"", partDev, d.Dev())
			}
			part.KernelDevicePath = filepath.Join(dirs.SysfsDir, devpath)

			devname, ok := udevProps["DEVNAME"]
			if !ok {
				return fmt.Errorf("cannot get udev properties for device %s (a partition of %s), missing required udev property \"DEVNAME\"", partDev, d.Dev())
			}
			part.KernelDeviceNode = devname

			// we should always have the partition uuid, and we may not have
			// either the partition label or the filesystem label, on GPT disks
			// the partition label is optional, and may or may not have a
			// filesystem on the partition, on MBR we will never have a
			// partition label, and we also may or may not have a filesystem on
			// the partition
			part.PartitionUUID = udevProps["ID_PART_ENTRY_UUID"]
			if part.PartitionUUID == "" {
				return fmt.Errorf("cannot get udev properties for device %s (a partition of %s), missing udev property \"ID_PART_ENTRY_UUID\"", partDev, d.Dev())
			}

			// we should also always have the device major/minor for this device
			part.Major, err = strconv.Atoi(udevProps["MAJOR"])
			if err != nil {
				return fmt.Errorf("cannot parse device major number format: %v", err)
			}

			part.Minor, err = strconv.Atoi(udevProps["MINOR"])
			if err != nil {
				return fmt.Errorf("cannot parse device major number format: %v", err)
			}

			// on MBR disks we may not have a partition label, so this may be
			// the empty string. Note that this value is encoded similarly to
			// libblkid and should be compared with normal Go strings that are
			// encoded using BlkIDEncodeLabel.
			part.PartitionLabel = udevProps["ID_PART_ENTRY_NAME"]

			// a partition doesn't need to have a filesystem, and such may not
			// have a filesystem label; the bios-boot partition in the amd64 pc
			// gadget is such an example of a partition GPT that does not have a
			// filesystem.
			// Note that this value is also encoded similarly to
			// ID_PART_ENTRY_NAME and thus should only be compared with normal
			// Go strings that are encoded with BlkIDEncodeLabel.
			part.FilesystemLabel = udevProps["ID_FS_LABEL_ENC"]

			// similar to above, this may be empty, but if non-empty is encoded
			part.FilesystemUUID = udevProps["ID_FS_UUID_ENC"]

			// prepend the partition to the front, this has the effect that if
			// two partitions have the same label (either filesystem or
			// partition though it is unclear whether you could actually in
			// practice create a disk partitioning scheme with the same
			// partition label for multiple partitions), then the one we look at
			// last while populating will be the one that the Find*()
			// functions locate first while iterating over the disk's partitions
			// this behavior matches what udev does
			// TODO: perhaps we should just explicitly not support disks with
			// non-unique filesystem labels or non-unique partition labels (or
			// even non-unique partition uuids)? then we would just error if we
			// encounter a duplicated value for a partition
			d.partitions = append([]Partition{part}, d.partitions...)
		}
	}

	// if we didn't find any partitions from above then return an error, this is
	// because all disks we search for partitions are expected to have some
	// partitions
	if len(d.partitions) == 0 {
		return fmt.Errorf("no partitions found for disk %s", d.Dev())
	}

	return nil
}

func (d *disk) FindMatchingPartitionWithPartLabel(label string) (Partition, error) {
	// always encode the label
	encodedLabel := BlkIDEncodeLabel(label)

	if err := d.populatePartitions(); err != nil {
		return Partition{}, err
	}

	for _, p := range d.partitions {
		if p.PartitionLabel == encodedLabel {
			return p, nil
		}
	}

	return Partition{}, PartitionNotFoundError{
		SearchType:  "partition-label",
		SearchQuery: label,
	}
}

func (d *disk) FindMatchingPartitionWithFsLabel(label string) (Partition, error) {
	// always encode the label
	encodedLabel := BlkIDEncodeLabel(label)

	if err := d.populatePartitions(); err != nil {
		return Partition{}, err
	}

	for _, p := range d.partitions {
		if p.FilesystemLabel == encodedLabel {
			return p, nil
		}
	}

	return Partition{}, PartitionNotFoundError{
		SearchType:  "filesystem-label",
		SearchQuery: label,
	}
}

// compatibility methods
// TODO: eliminate these and use the more generic functions in callers
func (d *disk) FindMatchingPartitionUUIDWithFsLabel(label string) (string, error) {
	p, err := d.FindMatchingPartitionWithFsLabel(label)
	if err != nil {
		return "", err
	}
	return p.PartitionUUID, nil
}

func (d *disk) FindMatchingPartitionUUIDWithPartLabel(label string) (string, error) {
	p, err := d.FindMatchingPartitionWithPartLabel(label)
	if err != nil {
		return "", err
	}
	return p.PartitionUUID, nil
}

func (d *disk) MountPointIsFromDisk(mountpoint string, opts *Options) (bool, error) {
	d2, err := diskFromMountPointImpl(mountpoint, opts)
	if err != nil {
		return false, err
	}

	// compare if the major/minor devices are the same and if both devices have
	// partitions
	return d.major == d2.major &&
			d.minor == d2.minor &&
			d.hasPartitions == d2.hasPartitions,
		nil
}

func (d *disk) Dev() string {
	return fmt.Sprintf("%d:%d", d.major, d.minor)
}

func (d *disk) HasPartitions() bool {
	// TODO: instead of saving this value when we create/discover the disk, we
	//       could instead populate the partitions here and then return whether
	//       d.partitions is empty or not
	return d.hasPartitions
}
