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

package partition

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
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
	diskFromMountPoint = diskFromMountPointImplWrapper
	mockedMountPoints  = make(map[string]*mockDisk)
	isSnapdTest        = len(os.Args) > 0 && strings.HasSuffix(os.Args[0], ".test")

	luksUUIDPatternRe = regexp.MustCompile(`(?m)CRYPT-LUKS2-([0-9a-f]{32})`)

	totalMockedDisks = 0
)

// Options is a set of options used when querying information about
// partition and disk devices.
type Options struct {
	IsDecryptedDevice bool
}

// Disk is a single physical disk device that contains partitions.
type Disk interface {
	// FindMatchingPartitionUUID finds the partition uuid for a partition matching
	// the specified label on the disk. Note that for non-ascii labels like
	// "Some label", the label should be encoded using \x<hex> for potentially
	// non-safe characters like in "Some\x20Label".
	FindMatchingPartitionUUID(string) (string, error)

	// MountPointIsFromDisk returns whether the specified mountpoint corresponds
	// to a partition on the disk.
	MountPointIsFromDisk(string, *Options) (bool, error)

	// String returns a representation of the disk that may not be unique, it
	// is meant for testing/debugging.
	String() string
}

type partition struct {
	major    int
	minor    int
	label    string
	partuuid string
	path     string
}

type disk struct {
	major      int
	minor      int
	partitions []*partition
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

var udevProperties = func(device string) (map[string]string, error) {
	// now we have the partition for this mountpoint, we need to tie that back
	// to a disk with a major minor, so query udev with the mount source path
	// of the mountpoint for properties
	cmd := exec.Command("udevadm", "info", "--name", device, "--query", "property")
	out, err := cmd.CombinedOutput()
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

// the wrapper function just calls the implementation, but has the right
// signature that returns a Disk, such that diskFromMountPointImplWrapper can
// be assigned to diskFromMountPoint
// the main reason we do this is so that we can call diskFromMountPointImpl from
// other functions without type casting anything
func diskFromMountPointImplWrapper(mountpoint string, opts *Options) (Disk, error) {
	return diskFromMountPointImpl(mountpoint, opts)
}

func diskFromMountPointImpl(mountpoint string, opts *Options) (*disk, error) {
	// first get the mount entry for the mountpoint
	mounts, err := osutil.LoadMountInfo()
	if err != nil {
		return nil, err
	}
	found := false
	d := &disk{}
	mountpointPart := partition{}
	for _, mount := range mounts {
		if mount.MountDir == mountpoint {
			mountpointPart.major = mount.DevMajor
			mountpointPart.minor = mount.DevMinor
			mountpointPart.path = mount.MountSource
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("cannot find mountpoint %q", mountpoint)
	}

	majorMinor := fmt.Sprintf("%d:%d", mountpointPart.major, mountpointPart.minor)

	if opts != nil && opts.IsDecryptedDevice {
		// if the device is an decrypted device, the partition we got will be a
		// virtual partition and a dm device, so we need to map that back to the
		// actual encrypted physical partition, then find the disk for that
		// partition

		// first we use the major minor for the mount point we got and verify
		// that there is a "dm" subdir in the sysfs dev/block entry for the disk
		// and in the dm subdir there are "uuid" and "name" files as we need
		// those to properly match the partition id contained in the dm uuid

		dmNameFile := filepath.Join(dirs.SysfsDir, "dev/block/", majorMinor, "dm/name")
		dmName, err := ioutil.ReadFile(dmNameFile)
		if err != nil && os.IsNotExist(err) {
			// not a dm device
			return nil, fmt.Errorf("disk %s is not a decrypted device", majorMinor)
		} else if err != nil {
			return nil, fmt.Errorf("cannot read dm name for disk %s: %v", majorMinor, err)
		}

		dmUUIDFile := filepath.Join(dirs.SysfsDir, "dev/block/", majorMinor, "dm/uuid")
		dmUUID, err := ioutil.ReadFile(dmUUIDFile)
		if err != nil && os.IsNotExist(err) {
			// not a dm device
			return nil, fmt.Errorf("disk %s is not a decrypted device", majorMinor)
		} else if err != nil {
			return nil, fmt.Errorf("cannot read dm uuid for disk %s: %v", majorMinor, err)
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
			// the format of the uuid is different - new luks version maybe?
			return nil, fmt.Errorf(
				"cannot verify disk: partition %s does not have a valid luks uuid format",
				majorMinor,
			)
		}

		// the uuid is the first and only submatch, but it is not in the same
		// format exactly as we want to use, namely it is missing all of the "-"
		// characters in a typical uuid, i.e. it is of the form:
		// ae6e79de00a9406f80ee64ba7c1966bb but we want it to be like:
		// ae6e79de-00a9-406f-80ee-64ba7c1966bb so we need to add in 4 "-"
		// characters
		fullUUID := string(matches[1])
		realUUID := fmt.Sprintf(
			"%s-%s-%s-%s-%s",
			fullUUID[0:8],
			fullUUID[8:12],
			fullUUID[12:16],
			fullUUID[16:20],
			fullUUID[20:],
		)

		// now finally, we need to use this uuid, which is the device uuid of
		// the actual physical encrypted partition to get the path, which will
		// be something like /dev/vda4, etc.
		props, err := udevProperties(filepath.Join("/dev/disk/by-uuid", realUUID))
		if err != nil {
			return nil, fmt.Errorf("cannot verify partition with uuid %s: %v", realUUID, err)
		}

		path, ok := props["DEVNAME"]
		if !ok {
			return nil, fmt.Errorf("cannot verify partition with uuid %s: incomplete udev information", realUUID)
		}

		// save it in the mountpointPart for the rest of the function to operate
		// normally on
		mountpointPart.path = path
	}

	// now we have the partition for this mountpoint, we need to tie that back
	// to a disk with a major minor, so query udev with the mount source path
	// of the mountpoint for properties
	props, err := udevProperties(mountpointPart.path)
	if err != nil && props == nil {
		// only fail here if props is nil, if it's available we validate it
		// below
		return nil, fmt.Errorf("cannot find disk for partition %s: %v", mountpointPart.path, err)
	}

	// ID_PART_ENTRY_DISK will give us the major and minor of the disk that this
	// partition originated from
	if majorMinor, ok := props["ID_PART_ENTRY_DISK"]; ok {
		maj, min, err := parseDeviceMajorMinor(majorMinor)
		if err != nil {
			// bad udev output?
			return nil, fmt.Errorf("cannot find disk for partition %s, bad udev output: %v", mountpointPart.path, err)
		}
		d.major = maj
		d.minor = min
	} else {
		// didn't find the property we need
		return nil, fmt.Errorf("cannot find disk for partition %s, incomplete udev output", mountpointPart.path)
	}

	// now we have the major and minor of the disk, and thus now have the
	// arduous task to identify all partitions that come from this disk using
	// sysfs
	// step 1. find all devices with a matching major number
	// step 2. start at the major + minor device for the disk, and iterate over
	//         all devices that have a partition attribute, starting with the
	//         device with major same as disk and minor equal to disk minor + 1
	// step 3. if we hit a device that does not have a partition attribute, then
	//         we hit another disk, and shall stop searching

	// TODO: are there devices that have structures on them that show up as
	//       contiguous devices but are _not_ partitions, i.e. some littlekernel
	//       devices?

	pattern := fmt.Sprintf("%s/dev/block/%d:*", dirs.SysfsDir, d.major)
	allDevices, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("internal error: %v", err)
	}

	// glob does not guarantee a sort, but we need our list of devices sorted
	sort.Strings(allDevices)

	// populate all the partitions from our candidate devices
	for _, dev := range allDevices {
		base := filepath.Base(dev)
		maj, min, err := parseDeviceMajorMinor(base)
		if err != nil {
			continue
		}

		// ignore any devices that have minor numbers less than the disk itself
		// the disk will have the same minor so ignore that too
		if min <= d.minor {
			continue
		}

		// now if there is a partition file, this is a partition for our disk
		if osutil.FileExists(filepath.Join(dev, "partition")) {
			// read the uevent file for this partition to get the devname
			p := &partition{
				major: maj,
				minor: min,
			}
			f, err := os.Open(filepath.Join(dev, "uevent"))
			if err != nil {
				continue
			}

			// now get the full set of udev properties for this partition
			eventProps, err := parseUdevProperties(f)
			if err != nil {
				continue
			}

			// get the name of the device path to call udevadm
			// note that here DEVNAME is coming from sysfs's uevent, and for
			// whatever reason it is just the basename a la vda3, but DEVNAME
			// when  requested from udevadm proper is the full path a la
			// /dev/vda3
			if name, ok := eventProps["DEVNAME"]; ok {
				p.path = filepath.Join("/dev", name)
			} else {
				continue
			}

			props, err := udevProperties(p.path)
			if err != nil && props == nil {
				// only error here if we didn't get a map, we validate the map
				// in the next steps
				continue
			}

			// get the label
			if labelEncoded, ok := props["ID_FS_LABEL_ENC"]; ok {
				p.label = labelEncoded
			} else {
				continue
			}

			// finally get the partition uuid
			if partuuid, ok := props["ID_PART_ENTRY_UUID"]; ok {
				p.partuuid = partuuid
			} else {
				continue
			}

			d.partitions = append(d.partitions, p)
		} else {
			// if there was not a partition file, we hit another disk and must
			// stop searching (the disk we are looking at will be ignored with
			// the minor number <= check above)
			break
		}
	}

	// if we didn't find any partitions from above then return an error
	if len(d.partitions) == 0 {
		return nil, fmt.Errorf("no partitions found for disk %s", majorMinor)
	}

	return d, nil
}

func (d *disk) FindMatchingPartitionUUID(label string) (string, error) {
	// iterate over the partitions looking for the specified label
	for _, part := range d.partitions {
		if part.label == label {
			return part.partuuid, nil
		}
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

func (d *disk) String() string {
	return fmt.Sprintf("%d:%d", d.major, d.minor)
}

// mockDisk is an implementation of Disk for mocking purposes, it is exported
// so that other packages can easily mock a specific disk layout without
// needing to mock the mount setup, sysfs, and udevadm commands just to test
// high level logic.
type mockDisk struct {
	s                string
	allMockedDisks   map[string]map[string]string
	mountpoint       string
	labelsToPartUUID map[string]string
}

func (d *mockDisk) FindMatchingPartitionUUID(label string) (string, error) {
	return d.labelsToPartUUID[label], nil
}

func (d *mockDisk) MountPointIsFromDisk(mountpoint string, opts *Options) (bool, error) {
	// TODO:UC20: support options here
	if otherPartitionsDisk, ok := d.allMockedDisks[mountpoint]; ok {
		// compare that the map of partitions between the two Disk's matches
		// as that's the only unique info that would be included in mocked tests
		for k, v := range d.labelsToPartUUID {
			if v2, ok := otherPartitionsDisk[k]; !ok || v2 != v {
				return false, nil
			}
		}
		for k, v := range otherPartitionsDisk {
			if v1, ok := d.labelsToPartUUID[k]; !ok || v1 != v {
				return false, nil
			}
		}
		return true, nil

	}
	return false, fmt.Errorf("mountpoint %s not mocked", mountpoint)
}

func (d *mockDisk) Equals(other Disk) bool {
	switch d2 := other.(type) {
	case *mockDisk:
		// compare that the map of partitions between the two Disk's matches
		// as that's the only unique info that would be included in mocked tests
		for k, v := range d.labelsToPartUUID {
			if v2, ok := d2.labelsToPartUUID[k]; !ok || v2 != v {
				return false
			}
		}
		for k, v := range d2.labelsToPartUUID {
			if v1, ok := d.labelsToPartUUID[k]; !ok || v1 != v {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (d *mockDisk) String() string {
	return d.s
}

// MockMountPointDisksToPartionMapping will mock DiskFromMountPoint such that
// the specified mapping is returned/used. Specifically, keys in the provided
// map are mountpoints, and the values for those keys are the partitions that
// are used to identify the disk.
func MockMountPointDisksToPartionMapping(mockedMountPoints map[string]map[string]string) (restore func()) {
	// only to be used in tests!!!!
	if !isSnapdTest {
		panic("mocking functions only to be used in tests!")
	}

	old := diskFromMountPoint

	mockedDiskNum := totalMockedDisks
	totalMockedDisks++

	diskFromMountPoint = func(mountpoint string, opts *Options) (Disk, error) {
		// TODO:UC20: support options here
		if partitions, ok := mockedMountPoints[mountpoint]; ok {
			return &mockDisk{
				s:                fmt.Sprintf("mocked-disk-%d", mockedDiskNum),
				allMockedDisks:   mockedMountPoints, // for MountPointIsFromDisk
				mountpoint:       mountpoint,
				labelsToPartUUID: partitions,
			}, nil
		}
		return nil, fmt.Errorf("mountpoint %s not mocked", mountpoint)
	}
	return func() {
		diskFromMountPoint = old
	}
}
