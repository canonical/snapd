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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

// diskFromMountPoint is exposed for mocking from other tests via
// MockMountPointDisksToPartionMapping, but we can't just assign
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
type Disk interface {
	// FindMatchingPartitionUUID finds the partition uuid for a partition
	// matching the specified filesystem label on the disk. Note that for
	// non-ascii labels like "Some label", the label will be encoded using
	// \x<hex> for potentially non-safe characters like in "Some\x20Label".
	FindMatchingPartitionUUID(string) (string, error)

	// MountPointIsFromDisk returns whether the specified mountpoint corresponds
	// to a partition on the disk. Note that this only considers partitions
	// and mountpoints found when the disk was identified with
	// DiskFromMountPoint.
	MountPointIsFromDisk(string, *Options) (bool, error)

	// Dev returns the string "major:minor" number for the disk device.
	Dev() string
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
	found := false
	d := &disk{}
	var partMountPointSource string
	// loop over the mount entries in reverse order to prevent shadowing of a
	// particular mount on top of another one
	for i := len(mounts) - 1; i >= 0; i-- {
		if mounts[i].MountDir == mountpoint {
			d.major = mounts[i].DevMajor
			d.minor = mounts[i].DevMinor
			partMountPointSource = mounts[i].MountSource
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("cannot find mountpoint %q", mountpoint)
	}

	// TODO:UC20: if the mountpoint is of a decrypted mapper device, then we
	//            need to trace back from the decrypted mapper device through
	//            luks to find the real encrypted partition underneath the
	//            decrypted one and thus the disk device for that partition

	// now we have the partition for this mountpoint, we need to tie that back
	// to a disk with a major minor, so query udev with the mount source path
	// of the mountpoint for properties
	props, err := udevProperties(partMountPointSource)
	if err != nil && props == nil {
		// only fail here if props is nil, if it's available we validate it
		// below
		return nil, fmt.Errorf("cannot find disk for partition %s: %v", partMountPointSource, err)
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
	} else {
		// the partition is probably a volume or other non-physical disk, so
		// confirm that DEVTYPE == disk and return the maj/min for it
		if devType, ok := props["DEVTYPE"]; ok {
			if devType == "disk" {
				return d, nil
			}
			// unclear what other DEVTYPE's we should support for this
			return nil, fmt.Errorf("unsupported DEVTYPE %q for mount point source %s", devType, partMountPointSource)
		}

		return nil, fmt.Errorf("cannot find disk for partition %s, incomplete udev output", partMountPointSource)
	}

	return d, nil
}

func (d *disk) FindMatchingPartitionUUID(label string) (string, error) {
	encodedLabel := EncodeHexBlkIDFormat(label)
	// if we haven't found the partitions for this disk yet, do that now
	if d.fsLabelToPartUUID == nil {
		d.fsLabelToPartUUID = make(map[string]string)
		// step 1. find all devices with a matching major number
		// step 2. start at the major + minor device for the disk, and iterate over
		//         all devices that have a partition attribute, starting with the
		//         device with major same as disk and minor equal to disk minor + 1
		// step 3. if we hit a device that does not have a partition attribute, then
		//         we hit another disk, and shall stop searching

		// note that this code assumes that all contiguous major / minor devices
		// belong to the same physical device, even with MBR and
		// logical/extended partition nodes jumping to i.e. /dev/sd*5

		// start with the minor + 1, since the major + minor of the disk we have
		// itself is not a partition
		currentMinor := d.minor
		for {
			currentMinor++
			partMajMin := fmt.Sprintf("%d:%d", d.major, currentMinor)
			props, err := udevProperties(filepath.Join("/dev/block", partMajMin))
			if err != nil && strings.Contains(err.Error(), "Unknown device") {
				// the device doesn't exist, we hit the end of the disk
				break
			} else if err != nil {
				// some other error trying to get udev properties, we should fail
				return "", fmt.Errorf("cannot get udev properties for partition %s: %v", partMajMin, err)
			}

			if props["DEVTYPE"] != "partition" {
				// we ran into another disk, break out
				break
			}

			// TODO: maybe save ID_PART_ENTRY_NAME here too, which is the name
			//       of the partition. this may be useful if this function gets
			//       used in the gadget update code
			fsLabelEnc := props["ID_FS_LABEL_ENC"]
			if fsLabelEnc == "" {
				// this partition does not have a filesystem, and thus doesn't
				// have a filesystem label - this is not fatal, i.e. the
				// bios-boot partition does not have a filesystem label but it
				// is the first structure and so we should just skip it
				continue
			}

			partuuid := props["ID_PART_ENTRY_UUID"]
			if partuuid == "" {
				return "", fmt.Errorf("cannot get udev properties for partition %s, missing udev property \"ID_PART_ENTRY_UUID\"", partMajMin)
			}

			// we always overwrite the fsLabelEnc with the last one, this has
			// the result that the last partition with a given filesystem label
			// will be set/found
			// this matches what udev does with the symlinks in /dev
			d.fsLabelToPartUUID[fsLabelEnc] = partuuid
		}
	}

	// if we didn't find any partitions from above then return an error
	if len(d.fsLabelToPartUUID) == 0 {
		return "", fmt.Errorf("no partitions found for disk %s", d.Dev())
	}

	if partuuid, ok := d.fsLabelToPartUUID[encodedLabel]; ok {
		return partuuid, nil
	}

	return "", fmt.Errorf("couldn't find label %q", label)
}

func (d *disk) MountPointIsFromDisk(mountpoint string, opts *Options) (bool, error) {
	d2, err := diskFromMountPointImpl(mountpoint, opts)
	if err != nil {
		return false, err
	}

	// compare if the major/minor devices are the same
	return d.major == d2.major && d.minor == d2.minor, nil
}

func (d *disk) Dev() string {
	return fmt.Sprintf("%d:%d", d.major, d.minor)
}
