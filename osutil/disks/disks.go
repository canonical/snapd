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
// the former returns a *disk, the latter returns a Disk, and as such they can't
// be assigned to each other
var diskFromMountPoint = func(mountpoint string, opts *Options) (Disk, error) {
	return diskFromMountPointImpl(mountpoint, opts)
}

// Options is a set of options used when querying information about
// partition and disk devices.
type Options struct {
	// IsDecryptedDevice indicates that the mountpoint is referring to a
	// decrypted device.
	IsDecryptedDevice bool
}

// Disk is a single physical disk device that contains partitions.
// TODO:UC20: add function to get some properties like an associated /dev node
//            for a disk for better user error reporting, i.e. /dev/vda3 is much
//            more helpful than 252:3
type Disk interface {
	// FindMatchingPartitionUUID finds the partition uuid for a partition
	// matching the specified filesystem label on the disk. Note that for
	// non-ascii labels like "Some label", the label will be encoded using
	// \x<hex> for potentially non-safe characters like in "Some\x20Label".
	// If the filesystem label was not found on the disk, and no other errors
	// were encountered, a FilesystemLabelNotFoundError will be returned.
	FindMatchingPartitionUUID(string) (string, error)

	// MountPointIsFromDisk returns whether the specified mountpoint corresponds
	// to a partition on the disk. Note that this only considers partitions
	// and mountpoints found when the disk was identified with
	// DiskFromMountPoint.
	// TODO:UC20: make this function return what a Disk of where the mount point
	//            is actually from if it is not from the same disk for better
	//            error reporting
	MountPointIsFromDisk(string, *Options) (bool, error)

	// Dev returns the string "major:minor" number for the disk device.
	Dev() string

	// HasPartitions returns whether the disk has partitions or not. A physical
	// disk will have partitions, but a mapper device will just be a volume that
	// does not have partitions for example.
	HasPartitions() bool
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

var udevadmProperties = func(device string) ([]byte, error) {
	// TODO: maybe combine with gadget interfaces hotplug code where the udev
	// db is parsed?
	cmd := exec.Command("udevadm", "info", "--query", "property", "--name", device)
	return cmd.CombinedOutput()
}

func udevProperties(device string) (map[string]string, error) {
	out, err := udevadmProperties(device)
	if err != nil {
		return nil, osutil.OutputErr(out, err)
	}
	r := bytes.NewBuffer(out)

	return parseUdevProperties(r)
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

// DiskFromMountPoint finds a matching Disk for the specified mount point.
func DiskFromMountPoint(mountpoint string, opts *Options) (Disk, error) {
	// call the unexported version that may be mocked by tests
	return diskFromMountPoint(mountpoint, opts)
}

type disk struct {
	major int
	minor int
	// fsLabelToPartUUID is a map of filesystem label -> partition uuid for now
	// eventually this may be expanded to be more generally useful
	fsLabelToPartUUID map[string]string

	// whether the disk device has partitions, and thus is of type "disk", or
	// whether the disk device is a volume that is not a physical disk
	hasPartitions bool
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
	props, err := udevProperties(partMountPointSource)
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
		props, err = udevProperties(byUUIDPath)
		if err != nil {
			return nil, fmt.Errorf("cannot get udev properties for encrypted partition %s: %v", byUUIDPath, err)
		}
	}

	// ID_PART_ENTRY_DISK will give us the major and minor of the disk that this
	// partition originated from
	if majorMinor, ok := props["ID_PART_ENTRY_DISK"]; ok {
		maj, min, err := parseDeviceMajorMinor(majorMinor)
		if err != nil {
			// bad udev output?
			return nil, fmt.Errorf("cannot find disk for partition %s, bad udev output: %v", partMountPointSource, err)
		}
		d.major = maj
		d.minor = min

		// since the mountpoint device has a disk, the mountpoint source itself
		// must be a partition from a disk, thus the disk has partitions
		d.hasPartitions = true
		return d, nil
	}

	// if we don't have ID_PART_ENTRY_DISK, the partition is probably a volume
	// or other non-physical disk, so confirm that DEVTYPE == disk and return
	// the maj/min for it
	if devType, ok := props["DEVTYPE"]; ok {
		if devType == "disk" {
			return d, nil
		}
		// unclear what other DEVTYPE's we should support for this function
		return nil, fmt.Errorf("unsupported DEVTYPE %q for mount point source %s", devType, partMountPointSource)
	}

	return nil, fmt.Errorf("cannot find disk for partition %s, incomplete udev output", partMountPointSource)
}

// FilesystemLabelNotFoundError is an error where the specified label was not
// found on the disk.
type FilesystemLabelNotFoundError struct {
	Label string
}

var (
	_ = error(FilesystemLabelNotFoundError{})
)

func (e FilesystemLabelNotFoundError) Error() string {
	return fmt.Sprintf("filesystem label %q not found", e.Label)
}

func (d *disk) FindMatchingPartitionUUID(label string) (string, error) {
	encodedLabel := BlkIDEncodeLabel(label)
	// if we haven't found the partitions for this disk yet, do that now
	if d.fsLabelToPartUUID == nil {
		d.fsLabelToPartUUID = make(map[string]string)

		// step 1. find the devpath for the disk, then glob for matching
		//         devices using the devname in that sysfs directory
		// step 2. iterate over all those devices and save all the ones that are
		//         partitions using the partition sysfs file
		// step 3. for all partition devices found, query udev to get the fs
		//         label and partition uuid

		udevProps, err := udevProperties(filepath.Join("/dev/block", d.Dev()))
		if err != nil {
			return "", err
		}

		// get the base device name
		devName := udevProps["DEVNAME"]
		if devName == "" {
			return "", fmt.Errorf("cannot get udev properties for device %s, missing udev property \"DEVNAME\"", d.Dev())
		}
		// the DEVNAME as returned by udev includes the /dev/mmcblk0 path, we
		// just want mmcblk0 for example
		devName = filepath.Base(devName)

		// get the device path in sysfs
		devPath := udevProps["DEVPATH"]
		if devPath == "" {
			return "", fmt.Errorf("cannot get udev properties for device %s, missing udev property \"DEVPATH\"", d.Dev())
		}

		// glob for /sys/${devPath}/${devName}*
		paths, err := filepath.Glob(filepath.Join(dirs.SysfsDir, devPath, devName+"*"))
		if err != nil {
			return "", fmt.Errorf("internal error getting udev properties for device %s: %v", err, d.Dev())
		}

		// Glob does not sort, so sort manually to have consistent tests
		sort.Strings(paths)

		for _, path := range paths {
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
			udevProps, err := udevProperties(partDev)
			if err != nil {
				continue
			}

			partUUID := udevProps["ID_PART_ENTRY_UUID"]
			if partUUID == "" {
				return "", fmt.Errorf("cannot get udev properties for device %s (a partition of %s), missing udev property \"ID_PART_ENTRY_UUID\"", partDev, d.Dev())
			}

			fsLabelEnc := udevProps["ID_FS_LABEL_ENC"]
			if fsLabelEnc == "" {
				// it is valid for there to be a partition without a fs
				// label - such as the bios-boot partition on amd64 pc
				// gadget systems
				// in this case just skip this, since we are only matching
				// by filesystem labels, obviously we cannot ever match to
				// a partition which does not have a filesystem
				continue
			}

			// TODO: maybe save ID_PART_ENTRY_NAME here too, which is the name
			//       of the partition. this may be useful if this function gets
			//       used in the gadget update code

			// we always overwrite the fsLabelEnc with the last one, this
			// has the result that the last partition with a given
			// filesystem label will be set/found
			// this matches what udev does with the symlinks in /dev
			d.fsLabelToPartUUID[fsLabelEnc] = partUUID
		}
	}

	// if we didn't find any partitions from above then return an error
	if len(d.fsLabelToPartUUID) == 0 {
		return "", fmt.Errorf("no partitions found for disk %s", d.Dev())
	}

	if partuuid, ok := d.fsLabelToPartUUID[encodedLabel]; ok {
		return partuuid, nil
	}

	return "", FilesystemLabelNotFoundError{Label: label}
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
	return d.hasPartitions
}
