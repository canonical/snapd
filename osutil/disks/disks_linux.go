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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

var (
	osutilDmIoctlTableStatus = osutil.DmIoctlTableStatus
)

var _ = Disk(&disk{})

// diskFromMountPoint is exposed for mocking from other tests via
// MockMountPointDisksToPartitionMapping, but we can't just assign
// diskFromMountPointImpl to diskFromMountPoint due to signature differences,
// the former returns a *disk, the latter returns a Disk
var diskFromMountPoint = func(mountpoint string, opts *Options) (Disk, error) {
	return diskFromMountPointImpl(mountpoint, opts)
}

var abstractCalculateLastUsableLBA = func(device string, diskSize uint64, sectorSize uint64) (uint64, error) {
	return CalculateLastUsableLBA(device, diskSize, sectorSize)
}

func MockUdevPropertiesForDevice(new func(string, string) (map[string]string, error)) (restore func()) {
	old := udevadmProperties
	// for better testing we mock the udevadm command output so that we still
	// test the parsing
	udevadmProperties = func(typeOpt, dev string) ([]byte, []byte, error) {
		props, err := new(typeOpt, dev)
		if err != nil {
			return []byte(err.Error()), []byte{}, err
		}
		// put it into udevadm format output, i.e. "KEY=VALUE\n"
		output := ""
		for k, v := range props {
			output += fmt.Sprintf("%s=%s\n", k, v)
		}
		return []byte(output), []byte{}, nil
	}
	return func() {
		udevadmProperties = old
	}
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

var udevadmProperties = func(typeOpt, device string) ([]byte, []byte, error) {
	// TODO: maybe combine with gadget interfaces hotplug code where the udev
	// db is parsed?
	return osutil.RunSplitOutput("udevadm", "info", "--query", "property", typeOpt, device)
}

func udevProperties(typeOpt, device string) (map[string]string, error) {
	out, stderr, err := udevadmProperties(typeOpt, device)
	if err != nil {
		return nil, osutil.OutputErrCombine(out, stderr, err)
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

type errNonPhysicalDisk struct {
	err string
}

func (e errNonPhysicalDisk) Error() string { return e.err }

const propNotFoundErrFmt = "property %q not found"

func requiredUDevPropUint(props map[string]string, name string) (uint64, error) {
	partIndex, ok := props[name]
	if !ok {
		return 0, fmt.Errorf(propNotFoundErrFmt, name)
	}
	v, err := strconv.ParseUint(partIndex, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse %q: %v", partIndex, err)
	}
	return v, nil
}

func diskFromUDevProps(deviceIdentifier string, deviceIDType string, props map[string]string) (Disk, error) {
	// all physical disks must have ID_PART_TABLE_TYPE defined as the schema for
	// the disk, so check for that first and if it's missing then we return a
	// specific NotAPhysicalDisk error
	schema := strings.ToLower(props["ID_PART_TABLE_TYPE"])
	if schema == "" {
		return nil, errNonPhysicalDisk{fmt.Sprintf("device with %s %q is not a physical disk", deviceIDType, deviceIdentifier)}
	}

	// for now we only support DOS and GPT schema disks
	if schema != "dos" && schema != "gpt" {
		return nil, fmt.Errorf("unsupported disk schema %q", props["ID_PART_TABLE_TYPE"])
	}

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

	partTableID := props["ID_PART_TABLE_UUID"]
	if partTableID == "" {
		return nil, fmt.Errorf("cannot find disk with %s %q: malformed udev output missing property \"ID_PART_TABLE_UUID\"", deviceIDType, deviceIdentifier)
	}

	// check if the device has partitions by attempting to actually search for
	// them in /sys with the DEVPATH and DEVNAME

	paths, err := filepath.Glob(filepath.Join(devpath, filepath.Base(devname)+"*"))
	if err != nil {
		return nil, fmt.Errorf("internal error with glob pattern: %v", err)
	}

	return &disk{
		schema:        schema,
		diskID:        partTableID,
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
// MockDevicePathToDiskMapping (which is yet to be added).
var diskFromDevicePath = func(devicePath string) (Disk, error) {
	// query for the disk props using udev with --path
	props, err := udevPropertiesForPath(devicePath)
	if err != nil {
		return nil, err
	}

	return diskFromUDevProps(devicePath, "path", props)
}

// DiskFromDeviceName finds a matching Disk using the specified name, such as
// vda, or mmcblk0, etc.
func DiskFromDeviceName(deviceName string) (Disk, error) {
	return diskFromDeviceName(deviceName)
}

// diskFromDeviceName is exposed for mocking from other tests via
// MockDeviceNameToDiskMapping.
var diskFromDeviceName = func(deviceName string) (Disk, error) {
	// query for the disk props using udev with --name
	props, err := udevPropertiesForName(deviceName)
	if err != nil {
		return nil, err
	}

	return diskFromUDevProps(deviceName, "name", props)
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

// DiskFromMountPoint finds a matching Disk for the specified mount point.
func DiskFromMountPoint(mountpoint string, opts *Options) (Disk, error) {
	// call the unexported version that may be mocked by tests
	return diskFromMountPoint(mountpoint, opts)
}

// DiskFromPartitionDeviceNode finds a matching Disk that the specified
// partition node resides on.
func DiskFromPartitionDeviceNode(node string) (Disk, error) {
	// TODO: support options such as IsDecryptedDevice
	return diskFromPartitionDeviceNode(node)
}

var diskFromPartitionDeviceNode = func(node string) (Disk, error) {
	// get the udev properties for this device node
	props, err := udevPropertiesForName(node)
	if err != nil {
		return nil, err
	}

	disk, err := diskFromPartUDevProps(props)
	if err != nil {
		return nil, fmt.Errorf("cannot find disk from partition device node %s: %v", node, err)
	}
	return disk, nil
}

type disk struct {
	major int
	minor int

	schema string

	diskID string

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

func (d *disk) DiskID() string {
	return d.diskID
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

// okay to use in initrd, uses blockdev command
func (d *disk) SectorSize() (uint64, error) {
	return blockDeviceSectorSize(d.devname)
}

// sfdiskDeviceDump represents the sfdisk --dump JSON output format.
type sfdiskDeviceDump struct {
	PartitionTable sfdiskPartitionTable `json:"partitiontable"`
}

type sfdiskPartitionTable struct {
	Unit     string `json:"unit"`
	FirstLBA uint64 `json:"firstlba"`
	LastLBA  uint64 `json:"lastlba"`
}

func (d *disk) SizeInBytes() (uint64, error) {
	// TODO: this could be implemented by reading the "size" file in sysfs
	// instead of using blockdev

	return blockDeviceSize(d.devname)
}

// TODO: remove this code in favor of abstractCalculateLastUsableLBA()
func lastLBAfromSFdisk(devname string) (uint64, error) {
	// TODO: this could also be accomplished by reading from the GPT headers
	// directly to get the last logical block address (LBA) instead of using
	// sfdisk, in which case this function could then be used in the initrd

	// otherwise for GPT, we need to use sfdisk on the device node
	output, err := exec.Command("sfdisk", "--json", devname).Output()
	if err != nil {
		return 0, err
	}

	var dump sfdiskDeviceDump
	if err := json.Unmarshal(output, &dump); err != nil {
		return 0, fmt.Errorf("cannot parse sfdisk output: %v", err)
	}

	// check that the unit is sectors
	if dump.PartitionTable.Unit != "sectors" {
		return 0, fmt.Errorf("cannot get size in sectors, sfdisk reported unknown unit %s", dump.PartitionTable.Unit)
	}

	// the last logical block address (LBA) is the location of the last
	// occupiable sector, so the end is 1 further (the end itself is not
	// included)

	// sfdisk always returns the sectors in native sector size, so we don't need
	// to do any conversion here
	return (dump.PartitionTable.LastLBA + 1), nil
}

// okay to use in initrd for dos disks since it uses blockdev command, but for
// gpt disks, we need to use sfdisk, which is not okay to use in the initrd
func (d *disk) UsableSectorsEnd() (uint64, error) {
	if d.schema == "dos" {
		// for DOS disks, it is sufficient to just get the size in bytes and
		// divide by the sector size
		byteSz, err := d.SizeInBytes()
		if err != nil {
			return 0, err
		}
		sectorSz, err := d.SectorSize()
		if err != nil {
			return 0, err
		}
		return byteSz / sectorSz, nil
	}

	// on UC20 (sfdisk 2.34) we use fdisk to determine the last LBA
	// to minimize the risk of moving to the new
	// abstractCalculateLastUsableLBA()
	//
	// TODO: remove this and use the abstractCalculateLastUsableLBA()
	//       everywhere (and also remove "lastLBAfromSFdisk" above)
	if output, err := exec.Command("sfdisk", "--version").Output(); err == nil {
		if strings.Contains(string(output), " 2.34") {
			return lastLBAfromSFdisk(d.devname)
		}
	}

	// calculated is last LBA
	byteSize, err := d.SizeInBytes()
	if err != nil {
		return 0, err
	}
	sectorSize, err := d.SectorSize()
	if err != nil {
		return 0, err
	}
	calculated, err := abstractCalculateLastUsableLBA(d.devname, byteSize, sectorSize)
	// end (or size) LBA is the last LBA + 1
	return calculated + 1, err
}

func (d *disk) Schema() string {
	return d.schema
}

// deviceNode returns the path of device node from the device path
// found in device mapper. It can either be <major>:<minor>, or a path
// starting with /dev.
//
// https://docs.kernel.org/admin-guide/device-mapper/verity.html
// and See https://docs.kernel.org/admin-guide/device-mapper/dm-crypt.html
func deviceNode(devicePath string) string {
	if strings.HasPrefix(devicePath, "/dev/") {
		return devicePath
	} else {
		return fmt.Sprintf("/dev/block/%s", devicePath)
	}
}

func parentPartitionPropsForOptions(props map[string]string) (map[string]string, error) {
	// verify that the mount point is indeed a mapper device, it should:
	// 1. have DEVTYPE == disk from udev
	// 2. have dm files in the sysfs entry for the maj:min of the device
	if props["DEVTYPE"] != "disk" {
		// not a decrypted device
		return nil, fmt.Errorf("not a decrypted device: devtype is not disk (is %s)", props["DEVTYPE"])
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
	errFmt := "not a decrypted device: could not read device mapper metadata: %v"

	if props["MAJOR"] == "" {
		return nil, fmt.Errorf("incomplete udev output missing required property \"MAJOR\"")
	}
	if props["MINOR"] == "" {
		return nil, fmt.Errorf("incomplete udev output missing required property \"MAJOR\"")
	}

	major, err := strconv.Atoi(props["MAJOR"])
	if err != nil {
		return nil, fmt.Errorf(errFmt, err)
	}
	minor, err := strconv.Atoi(props["MINOR"])
	if err != nil {
		return nil, fmt.Errorf(errFmt, err)
	}

	targets, err := osutilDmIoctlTableStatus(uint32(major), uint32(minor))
	if err != nil {
		return nil, fmt.Errorf(errFmt, err)
	}
	if len(targets) != 1 {
		return nil, fmt.Errorf("unexpected number of targets for device mapper: got %d", len(targets))
	}
	params := strings.Split(targets[0].Params, " ")
	var dataDevice string
	if targets[0].TargetType == "verity" {
		// Parameters:
		//   <version> <dev> <hash_dev>
		//   <data_block_size> <hash_block_size>
		//   <num_data_blocks> <hash_start_block>
		//   <algorithm> <digest> <salt>
		//   [<#opt_params> <opt_params>]
		//
		// See https://docs.kernel.org/admin-guide/device-mapper/verity.html
		if len(params) < 3 {
			return nil, fmt.Errorf("cannot find needed device mapper parameters")
		}
		dataDevice = deviceNode(params[1])
		//hashDevice := deviceNode(table[2])
	} else if targets[0].TargetType == "crypt" {
		// Parameters:
		//   <cipher> <key> <iv_offset> <device path> \
		//   <offset> [<#opt_params> <opt_params>]
		//
		// See https://docs.kernel.org/admin-guide/device-mapper/dm-crypt.html
		if len(params) < 4 {
			return nil, fmt.Errorf("cannot find needed device mapper parameters")
		}
		dataDevice = deviceNode(params[3])
	} else {
		return nil, fmt.Errorf("unknown device mapper type")
	}

	props, err = udevPropertiesForName(dataDevice)
	if err != nil {
		return nil, fmt.Errorf("cannot get udev properties for encrypted partition %s: %v", dataDevice, err)
	}

	return props, nil
}

func diskFromPartUDevProps(props map[string]string) (*disk, error) {
	// ID_PART_ENTRY_DISK will give us the major and minor of the disk that this
	// partition originated from if this mount point is indeed for a partition
	if props["ID_PART_ENTRY_DISK"] == "" {
		// TODO: there may be valid use cases for ID_PART_ENTRY_DISK being
		// missing, like where a mountpoint is for a decrypted mapper device,
		// and the physical backing device is a full disk and not a partition,
		// but we don't have such use cases right now so just error, this can
		// be revisited later on
		return nil, fmt.Errorf("incomplete udev output missing required property \"ID_PART_ENTRY_DISK\"")
	}

	majorMinor := props["ID_PART_ENTRY_DISK"]
	maj, min, err := parseDeviceMajorMinor(majorMinor)
	if err != nil {
		// bad udev output?
		return nil, fmt.Errorf("bad udev output: %v", err)
	}

	d := &disk{
		major: maj,
		minor: min,
	}

	// now go find the devname and devpath for this major/minor pair since
	// we will need that later - note that the props variable at this point
	// is for the partition, not the parent disk itself, hence the
	// additional lookup
	realDiskProps, err := udevPropertiesForName(filepath.Join("/dev/block/", majorMinor))
	if err != nil {
		return nil, err
	}

	if devtype := realDiskProps["DEVTYPE"]; devtype != "disk" {
		return nil, fmt.Errorf("unsupported DEVTYPE %q", devtype)
	}

	if realDiskProps["DEVNAME"] == "" {
		return nil, fmt.Errorf("incomplete udev output missing required property \"DEVNAME\"")
	}

	d.devname = realDiskProps["DEVNAME"]

	if realDiskProps["DEVPATH"] == "" {
		return nil, fmt.Errorf("incomplete udev output missing required property \"DEVPATH\"")
	}

	// the DEVPATH is given as relative to /sys, so for simplicity's sake
	// add /sys to the path we save as we return it later
	d.devpath = filepath.Join(dirs.SysfsDir, realDiskProps["DEVPATH"])

	partTableID := realDiskProps["ID_PART_TABLE_UUID"]
	if partTableID == "" {
		return nil, fmt.Errorf("incomplete udev output missing required property \"ID_PART_TABLE_UUID\"")
	}

	schema := strings.ToLower(realDiskProps["ID_PART_TABLE_TYPE"])
	if schema == "" {
		return nil, fmt.Errorf("incomplete udev output missing required property \"ID_PART_TABLE_TYPE\"")
	}

	if schema != "gpt" && schema != "dos" {
		return nil, fmt.Errorf("unsupported disk schema %q", realDiskProps["ID_PART_TABLE_TYPE"])
	}

	d.schema = schema
	d.diskID = partTableID

	// since the mountpoint device has a disk, the mountpoint source itself
	// must be a partition from a disk, thus the disk has partitions
	d.hasPartitions = true
	return d, nil
}

func partitionPropsFromMountPoint(mountpoint string) (source string, props map[string]string, err error) {
	// first get the mount entry for the mountpoint
	mounts, err := osutil.LoadMountInfo()
	if err != nil {
		return "", nil, err
	}
	// loop over the mount entries in reverse order to prevent shadowing of a
	// particular mount on top of another one
	for i := len(mounts) - 1; i >= 0; i-- {
		if mounts[i].MountDir == mountpoint {
			source = mounts[i].MountSource
			break
		}
	}
	if source == "" {
		return "", nil, fmt.Errorf("cannot find mountpoint %q", mountpoint)
	}

	// now we have the partition for this mountpoint, we need to tie that back
	// to a disk with a major minor, so query udev with the mount source path
	// of the mountpoint for properties
	props, err = udevPropertiesForName(source)
	if err != nil && props == nil {
		// only fail here if props is nil, if it's available we validate it
		// below
		return "", nil, fmt.Errorf("cannot process udev properties of %s: %v", source, err)
	}
	return source, props, nil
}

// diskFromMountPointImpl returns a Disk for the underlying mount source of the
// specified mount point. For mount points which have sources that are not
// partitions, and thus are a part of a disk, the returned Disk refers to the
// volume/disk of the mount point itself.
func diskFromMountPointImpl(mountpoint string, opts *Options) (*disk, error) {
	source, props, err := partitionPropsFromMountPoint(mountpoint)
	if err != nil {
		return nil, err
	}

	if opts != nil && opts.IsDecryptedDevice {
		props, err = parentPartitionPropsForOptions(props)
		if err != nil {
			return nil, fmt.Errorf("cannot process properties of %v parent device: %v", source, err)
		}
	}

	disk, err := diskFromPartUDevProps(props)
	if err != nil {
		// TODO: leave the inclusion of mpointpoint source in the error
		// to the caller
		return nil, fmt.Errorf("cannot find disk from mountpoint source %s of %s: %v", source, mountpoint, err)
	}
	return disk, nil
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
			_, err := os.ReadFile(filepath.Join(path, "partition"))
			if err != nil {
				continue
			}

			partDev := filepath.Base(path)

			// then the device is a partition, trigger any udev events for it
			// that might be sitting around and then get the udev props for it

			// trigger
			out, err := exec.Command("udevadm", "trigger", "--name-match="+partDev).CombinedOutput()
			if err != nil {
				return osutil.OutputErr(out, err)
			}

			// then settle
			out, err = exec.Command("udevadm", "settle", "--timeout=180").CombinedOutput()
			if err != nil {
				return osutil.OutputErr(out, err)
			}

			udevProps, err := udevPropertiesForName(partDev)
			if err != nil {
				continue
			}

			emitUDevPropErr := func(e error) error {
				return fmt.Errorf("cannot get required udev property for device %s (a partition of %s): %v", partDev, d.Dev(), e)
			}

			// the devpath and devname should always be available
			devpath, ok := udevProps["DEVPATH"]
			if !ok {
				return emitUDevPropErr(fmt.Errorf(propNotFoundErrFmt, "DEVPATH"))
			}
			part.KernelDevicePath = filepath.Join(dirs.SysfsDir, devpath)

			devname, ok := udevProps["DEVNAME"]
			if !ok {
				return emitUDevPropErr(fmt.Errorf(propNotFoundErrFmt, "DEVNAME"))
			}
			part.KernelDeviceNode = devname

			// we should always have the partition type
			partType, ok := udevProps["ID_PART_ENTRY_TYPE"]
			if !ok {
				return emitUDevPropErr(fmt.Errorf(propNotFoundErrFmt, "ID_PART_ENTRY_TYPE"))
			}

			// on dos disks, the type is formatted like "0xc", when we prefer to
			// always use "0C" so fix the formatting
			if d.schema == "dos" {
				partType = strings.TrimPrefix(strings.ToLower(partType), "0x")
				v, err := strconv.ParseUint(partType, 16, 8)
				if err != nil {
					return emitUDevPropErr(fmt.Errorf("cannot convert MBR partition type %q: %v", partType, err))
				}
				partType = fmt.Sprintf("%02X", v)
			} else {
				// on GPT disks, just capitalize the partition type since it's a
				// UUID
				partType = strings.ToUpper(partType)
			}

			part.PartitionType = partType

			// the partition may not have a filesystem, in which case this might
			// be the empty string
			part.FilesystemType = udevProps["ID_FS_TYPE"]

			part.SizeInBytes, err = requiredUDevPropUint(udevProps, "ID_PART_ENTRY_SIZE")
			if err != nil {
				return emitUDevPropErr(err)
			}

			part.DiskIndex, err = requiredUDevPropUint(udevProps, "ID_PART_ENTRY_NUMBER")
			if err != nil {
				return emitUDevPropErr(err)
			}

			part.StartInBytes, err = requiredUDevPropUint(udevProps, "ID_PART_ENTRY_OFFSET")
			if err != nil {
				return emitUDevPropErr(err)
			}

			// udev always reports the size and offset in 512 byte blocks,
			// regardless of sector size, so multiply the size in 512 byte
			// blocks by 512 to get the size/offset in bytes
			part.StartInBytes *= 512
			part.SizeInBytes *= 512

			// we should always have the partition uuid, and we may not have
			// either the partition label or the filesystem label, on GPT disks
			// the partition label is optional, and may or may not have a
			// filesystem on the partition, on MBR we will never have a
			// partition label, and we also may or may not have a filesystem on
			// the partition
			part.PartitionUUID = udevProps["ID_PART_ENTRY_UUID"]
			if part.PartitionUUID == "" {
				return emitUDevPropErr(fmt.Errorf(propNotFoundErrFmt, "ID_PART_ENTRY_UUID"))
			}

			// we should also always have the device major/minor for this device
			maj, err := requiredUDevPropUint(udevProps, "MAJOR")
			if err != nil {
				return emitUDevPropErr(err)
			}
			part.Major = int(maj)

			min, err := requiredUDevPropUint(udevProps, "MINOR")
			if err != nil {
				return emitUDevPropErr(err)
			}
			part.Minor = int(min)

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
		if p.hasFilesystemLabel(encodedLabel) {
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

func AllPhysicalDisks() ([]Disk, error) {
	// get disks for every block device in /sys/block/
	blockDir := filepath.Join(dirs.SysfsDir, "block")

	files, err := os.ReadDir(blockDir)
	if err != nil {
		return nil, err
	}

	disks := make([]Disk, 0, len(files))

	for _, f := range files {
		if f.IsDir() {
			// unexpected to have a directory here and not a symlink, but for
			// now just silently ignore it
			continue
		}

		// get a disk by path with the name of the file and /block/
		fullpath := filepath.Join(blockDir, f.Name())

		disk, err := DiskFromDevicePath(fullpath)
		if err != nil {
			if errors.As(err, &errNonPhysicalDisk{}) {
				continue
			}
			return nil, err
		}
		disks = append(disks, disk)
	}
	return disks, nil
}

// PartitionUUIDFromMountPoint returns the UUID of the partition which is a
// source of a given mount point.
func PartitionUUIDFromMountPoint(mountpoint string, opts *Options) (string, error) {
	_, props, err := partitionPropsFromMountPoint(mountpoint)
	if err != nil {
		return "", err
	}

	if opts != nil && opts.IsDecryptedDevice {
		props, err = parentPartitionPropsForOptions(props)
		if err != nil {
			return "", err
		}
	}
	partUUID := props["ID_PART_ENTRY_UUID"]
	if partUUID == "" {
		partDev := filepath.Join("/dev", props["DEVNAME"])
		return "", fmt.Errorf("cannot get required partition UUID udev property for device %s", partDev)
	}
	return partUUID, nil
}

// PartitionUUID returns the UUID of a given partition
func PartitionUUID(node string) (string, error) {
	props, err := udevPropertiesForName(node)
	if err != nil && props == nil {
		// only fail here if props is nil, if it's available we validate it
		// below
		return "", fmt.Errorf("cannot process udev properties: %v", err)
	}
	partUUID := props["ID_PART_ENTRY_UUID"]
	if partUUID == "" {
		return "", fmt.Errorf("cannot get required udev partition UUID property")
	}
	return partUUID, nil
}

func SectorSize(devname string) (uint64, error) {
	return blockDeviceSectorSize(devname)
}

// filesystemTypeForPartition returns the filesystem type for a
// partition passed by device name. The type might be an empty string
// if no filesystem has been detected.
func filesystemTypeForPartition(devname string) (string, error) {
	props, err := udevPropertiesForName(devname)
	if err != nil {
		return "", err
	}

	return props["ID_FS_TYPE"], nil
}

func MockDmIoctlTableStatus(f func(major uint32, minor uint32) ([]osutil.TargetInfo, error)) func() {
	osutil.MustBeTestBinary("cannot mock dm-ioctl of tests")
	old := osutilDmIoctlTableStatus
	osutilDmIoctlTableStatus = f
	return func() {
		osutilDmIoctlTableStatus = old
	}
}
