// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package gadget

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/strutil"
)

var ErrNoUpdate = errors.New("nothing to update")

// GadgetData holds references to a gadget revision metadata and its data directory.
type GadgetData struct {
	// Info is the gadget metadata
	Info *Info
	// XXX: should be GadgetRootDir
	// RootDir is the root directory of gadget snap data
	RootDir string

	// KernelRootDir is the root directory of kernel snap data
	KernelRootDir string
}

// UpdatePolicyFunc is a callback that evaluates the provided pair of
// (potentially not yet resolved) structures and returns true when the
// pair should be part of an update. It may also return a filter
// function for the resolved content when not all of the content
// should be applied as part of the update (e.g. when updating assets
// from the kernel snap).
type UpdatePolicyFunc func(from, to *LaidOutStructure) (bool, ResolvedContentFilterFunc)

// ResolvedContentFilterFunc is a callback that evaluates the given
// ResolvedContent and returns true if it should be applied as part of
// an update. This is relevant for e.g. asset updates that come from
// the kernel snap.
type ResolvedContentFilterFunc func(*ResolvedContent) bool

// ContentChange carries paths to files containing the content data being
// modified by the operation.
type ContentChange struct {
	// Before is a path to a file containing the original data before the
	// operation takes place (or took place in case of ContentRollback).
	Before string
	// After is a path to a file location of the data applied by the operation.
	After string
}

type (
	ContentOperation    int
	ContentChangeAction int
)

const (
	ContentWrite ContentOperation = iota
	ContentUpdate
	ContentRollback

	ChangeAbort ContentChangeAction = iota
	ChangeApply
	ChangeIgnore
)

// ContentObserver allows for observing operations on the content of the gadget
// structures.
type ContentObserver interface {
	// Observe is called to observe an pending or completed action, related
	// to content being written, updated or being rolled back. In each of
	// the scenarios, the target path is relative under the root. The role
	// of the affected partition is needed as different assets are tracked
	// depending on whether this is a boot or a seed partition.
	//
	// For a file write or update, the source path points to the content
	// that will be written. When called during rollback, observe call
	// happens after the original file has been restored (or removed if the
	// file was added during the update), the source path is empty.
	//
	// Returning ChangeApply indicates that the observer agrees for a given
	// change to be applied. When called with a ContentUpdate or
	// ContentWrite operation, returning ChangeIgnore indicates that the
	// change shall be ignored. ChangeAbort is expected to be returned along
	// with a non-nil error.
	Observe(op ContentOperation, partRole, targetRootDir, relativeTargetPath string, dataChange *ContentChange) (ContentChangeAction, error)
}

// ContentUpdateObserver allows for observing update (and potentially a
// rollback) of the gadget structure content.
type ContentUpdateObserver interface {
	ContentObserver
	// BeforeWrite is called when the backups of content that will get
	// modified during the update are complete and update is ready to be
	// applied.
	BeforeWrite() error
	// Canceled is called when the update has been canceled, or if changes
	// were written and the update has been reverted.
	Canceled() error
}

// searchVolumeWithTraitsAndMatchParts searches for a disk matching the given
// traits and returns the matched partitions.
func searchVolumeWithTraitsAndMatchParts(vol *Volume, traits DiskVolumeDeviceTraits, validateOpts *DiskVolumeValidationOptions) (disks.Disk, map[int]*OnDiskStructure, error) {
	if validateOpts == nil {
		validateOpts = &DiskVolumeValidationOptions{}
	}

	// iterate over the different traits, validating whether the resulting disk
	// actually exists and matches the volume we have in the gadget.yaml

	compatibleCandidate := func(candidate disks.Disk, method string, providedErr error) map[int]*OnDiskStructure {
		if providedErr != nil {
			if candidate != nil {
				logger.Debugf("candidate disk %s not appropriate for volume %s because err: %v", candidate.KernelDeviceNode(), vol.Name, providedErr)
				return nil
			}
			logger.Debugf("cannot locate disk for volume %s with method %s because err: %v", vol.Name, method, providedErr)

			return nil
		}
		diskLayout, onDiskErr := OnDiskVolumeFromDevice(candidate.KernelDeviceNode())
		if onDiskErr != nil {
			// unexpected in reality, we already called one of
			// DiskFromDeviceName or DiskFromDevicePath to get this reference,
			// so it's unclear how those methods could return a disk that
			// OnDiskVolumeFromDevice is unhappy about
			logger.Debugf("cannot find on disk volume from candidate disk %s: %v", candidate.KernelDeviceNode(), onDiskErr)
			return nil
		}
		// then try to validate it by laying out the volume
		opts := &VolumeCompatibilityOptions{
			AssumeCreatablePartitionsCreated: true,
			AllowImplicitSystemData:          validateOpts.AllowImplicitSystemData,
			ExpectedStructureEncryption:      validateOpts.ExpectedStructureEncryption,
		}
		gadgetStructToDiskStruct, ensureErr := EnsureVolumeCompatibility(vol, diskLayout, opts)
		if ensureErr != nil {
			logger.Debugf("candidate disk %s not appropriate for volume %s due to incompatibility: %v", candidate.KernelDeviceNode(), vol.Name, ensureErr)
			return nil
		}

		// success, we found it
		return gadgetStructToDiskStruct
	}

	// first try the kernel device path if it is set
	if traits.OriginalDevicePath != "" {
		disk := mylog.Check2(disks.DiskFromDevicePath(traits.OriginalDevicePath))
		gadgetStructToDiskStruct := compatibleCandidate(disk, "device path", err)
		if gadgetStructToDiskStruct != nil {
			return disk, gadgetStructToDiskStruct, nil
		}
	}

	// next try the kernel device node name
	if traits.OriginalKernelPath != "" {
		disk := mylog.Check2(disks.DiskFromDeviceName(traits.OriginalKernelPath))
		gadgetStructToDiskStruct := compatibleCandidate(disk, "device name", err)
		if gadgetStructToDiskStruct != nil {
			return disk, gadgetStructToDiskStruct, nil
		}
	}

	// next try the disk ID from the partition table
	if traits.DiskID != "" {
		// there isn't a way to find a disk using the disk ID directly, so we
		// instead have to get all the disks and then check them all to see if
		// the disk ID's match
		blockdevDisks := mylog.Check2(disks.AllPhysicalDisks())
		if err == nil {
			for _, blockDevDisk := range blockdevDisks {
				if blockDevDisk.DiskID() == traits.DiskID {
					// found the block device for this Disk ID, get the
					// disks.Disk for it
					gadgetStructToDiskStruct := compatibleCandidate(blockDevDisk, "disk ID", err)
					if gadgetStructToDiskStruct != nil {
						return blockDevDisk, gadgetStructToDiskStruct, nil
					}

					// otherwise if it didn't match we keep iterating over
					// the block devices, since we could have a situation
					// where an attacker has cloned the disk and put their own
					// content on it to attack the device and so there are two
					// block devices with the same ID but non-matching
					// structures
				}
			}
		} else {
			logger.Noticef("error getting all physical disks: %v", err)
		}
	}

	// TODO: implement this final last ditch effort
	// finally, try doing an inverse search using the individual
	// structures to match a structure we measured previously to find a on disk
	// device and then find a disk from that device and see if it matches

	return nil, nil, fmt.Errorf("cannot find physical disk laid out to map with volume %s", vol.Name)
}

// IsCreatableAtInstall returns whether the gadget structure would be created at
// install - currently that is only ubuntu-save, ubuntu-data, and ubuntu-boot
func IsCreatableAtInstall(gv *VolumeStructure) bool {
	// a structure is creatable at install if it is one of the roles for
	// system-save, system-data, or system-boot
	switch gv.Role {
	case SystemSave, SystemData, SystemBoot:
		return true
	default:
		return false
	}
}

func isCompatibleSchema(gadgetSchema, diskSchema string) bool {
	switch gadgetSchema {
	// XXX: "mbr,gpt" is currently unsupported
	case "", "gpt":
		return diskSchema == "gpt"
	case "mbr":
		return diskSchema == "dos"
	default:
		return false
	}
}

func onDiskStructureIsLikelyImplicitSystemDataRole(gadgetVolume *Volume, diskLayout *OnDiskVolume, s OnDiskStructure) bool {
	// in uc16/uc18 we used to allow system-data to be implicit / missing from
	// the gadget.yaml in which case we won't have system-data in the laidOutVol
	// but it will be in diskLayout, so we sometimes need to check if a given on
	// disk partition looks like it was created implicitly by ubuntu-image as
	// specified via the defaults in
	// https://github.com/canonical/ubuntu-image-legacy/blob/master/ubuntu_image/parser.py#L568-L589

	// namely it must meet the following conditions:
	// * fs is ext4
	// * partition type is "Linux filesystem data"
	// * fs label is "writable"
	// * this on disk structure is last on the disk
	// * there is exactly one more structure on disk than partitions in the
	//   gadget
	// * there is no system-data role in the gadget.yaml

	// note: we specifically do not check the size of the structure because it
	// likely was resized, but it also could have not been resized if there
	// ended up being less than 10% free space as per the resize script in the
	// initramfs:
	// https://github.com/snapcore/core-build/blob/master/initramfs/scripts/local-premount/resize

	// bare structures don't show up on disk, so we can't include them
	// when calculating how many "structures" are in gadgetVolume to
	// ensure that there is only one extra OnDiskStructure at the end
	numPartsInGadget := 0
	for _, s := range gadgetVolume.Structure {
		if s.IsPartition() {
			numPartsInGadget++
		}

		// also check for explicit system-data role
		if s.Role == SystemData {
			// s can't be implicit system-data since there is an explicit
			// system-data
			return false
		}
	}

	numPartsOnDisk := len(diskLayout.Structure)

	return s.PartitionFSType == "ext4" &&
		(s.Type == "0FC63DAF-8483-4772-8E79-3D69D8477DE4" || s.Type == "83") &&
		s.PartitionFSLabel == "writable" &&
		// DiskIndex is 1-based
		s.DiskIndex == numPartsOnDisk &&
		numPartsInGadget+1 == numPartsOnDisk
}

// VolumeCompatibilityOptions is a set of options for determining how
// strict to be when evaluating whether an on-disk structure matches a laid out
// structure.
type VolumeCompatibilityOptions struct {
	// AssumeCreatablePartitionsCreated will assume that all partitions such as
	// ubuntu-data, ubuntu-save, etc. that are creatable in install mode have
	// already been created and thus must be already exactly matching that which
	// is in the gadget.yaml.
	AssumeCreatablePartitionsCreated bool

	// AllowImplicitSystemData allows the system-data role to be missing from
	// the gadget volume as was allowed in UC18 and UC16 where the system-data
	// partition would be dynamically inserted into the image at image build
	// time by ubuntu-image without being mentioned in the gadget.yaml.
	AllowImplicitSystemData bool

	// ExpectedStructureEncryption is a map of the structure name to information
	// about the encrypted partitions that can be used to validate whether a
	// given structure should be accepted as an encrypted partition.
	ExpectedStructureEncryption map[string]StructureEncryptionParameters
}

// EnsureVolumeCompatibility checks compatibility between a gadget volume and a
// real disk. It returns a map of the gadget structures yaml indexes to disk
// structures that was possible to match.
// TODO change to returning OnDiskAndGadgetStructurePair
func EnsureVolumeCompatibility(gadgetVolume *Volume, diskVolume *OnDiskVolume, opts *VolumeCompatibilityOptions) (map[int]*OnDiskStructure, error) {
	gadgetStructIdxToOnDiskStruct := map[int]*OnDiskStructure{}
	if opts == nil {
		opts = &VolumeCompatibilityOptions{}
	}
	logger.Debugf("checking volume compatibility between gadget volume %s (partial: %v) and disk %s",
		gadgetVolume.Name, gadgetVolume.Partial, diskVolume.Device)

	eq := func(ds *OnDiskStructure, vss []VolumeStructure, vssIdx int) (bool, string) {
		gs := &vss[vssIdx]
		// name mismatch
		if gs.Name != ds.Name {
			// partitions have no names in MBR so bypass the name check
			if gadgetVolume.Schema != "mbr" {
				// don't return a reason if the names don't match
				return false, ""
			}
		}
		mylog.Check(

			// start offset mismatch
			CheckValidStartOffset(ds.StartOffset, vss, vssIdx))

		maxSz := effectivePartSize(gs)
		switch {
		// on disk size too small
		case ds.Size < gs.MinSize:
			return false, fmt.Sprintf("on disk size %d (%s) is smaller than gadget min size %d (%s)",
				ds.Size, ds.Size.IECString(), gs.MinSize, gs.MinSize.IECString())

		// on disk size too large
		case ds.Size > maxSz:
			// larger on disk size is allowed specifically only for system-data
			if gs.Role != SystemData {
				return false, fmt.Sprintf("on disk size %d (%s) is larger than gadget size %d (%s) (and the role should not be expanded)",
					ds.Size, ds.Size.IECString(), maxSz, maxSz.IECString())
			}
		}

		// If we got to this point, the structure on disk has the same
		// name, and compatible size and offset, so the last thing to
		// check is that the filesystem matches (or that we don't care
		// about the filesystem).

		// first handle the strict case where this partition was created at
		// install in case it is an encrypted one
		if opts.AssumeCreatablePartitionsCreated && IsCreatableAtInstall(gs) {
			// only partitions that are creatable at install can be encrypted,
			// check if this partition was encrypted
			if encTypeParams, ok := opts.ExpectedStructureEncryption[gs.Name]; ok {
				if encTypeParams.Method == "" {
					return false, "encrypted structure parameter missing required parameter \"method\""
				}
				// for now we don't handle any other keys, but in case they show
				// up in the wild for debugging purposes log off the key name
				for k := range encTypeParams.unknownKeys {
					if k != "method" {
						logger.Noticef("ignoring unknown expected encryption structure parameter %q", k)
					}
				}

				switch encTypeParams.Method {
				case EncryptionLUKS:
					// then this partition is expected to have been encrypted, the
					// filesystem label on disk will need "-enc" appended
					if ds.PartitionFSLabel != gs.Name+"-enc" {
						return false, fmt.Sprintf("partition %[1]s is expected to be encrypted but is not named %[1]s-enc", gs.Name)
					}

					// the filesystem should also be "crypto_LUKS"
					if ds.PartitionFSType != "crypto_LUKS" {
						return false, fmt.Sprintf("partition %[1]s is expected to be encrypted but does not have an encrypted filesystem", gs.Name)
					}

					// at this point the partition matches
					return true, ""
				default:
					return false, fmt.Sprintf("unsupported encrypted partition type %q", encTypeParams.Method)
				}
			}

			// for non-encrypted partitions that were created at install, the
			// below logic still applies
		}

		if opts.AssumeCreatablePartitionsCreated || !IsCreatableAtInstall(gs) {
			// we assume that this partition has already been created
			// successfully - either because this function was forced to(as is
			// the case when doing gadget asset updates), or because this
			// structure is not created during install

			// note that we only check the filesystem if the gadget specified a
			// filesystem, this is to allow cases where a structure in the
			// gadget has a image, but does not specify the filesystem because
			// it is some binary blob from a hardware vendor for non-Linux
			// components on the device that _just so happen_ to also have a
			// filesystem when the image is deployed to a partition. In this
			// case we don't care about the filesystem at all because snapd does
			// not touch it, unless a gadget asset update says to update that
			// image file with a new binary image file. This also covers the
			// partial filesystem case.
			if gs.Filesystem != "" && gs.LinuxFilesystem() != ds.PartitionFSType {
				// use more specific error message for structures that are
				// not creatable at install when we are not being strict
				if !IsCreatableAtInstall(gs) && !opts.AssumeCreatablePartitionsCreated {
					return false, fmt.Sprintf("filesystems do not match (and the partition is not creatable at install): declared as %s, got %s", gs.Filesystem, ds.PartitionFSType)
				}
				// otherwise generic
				return false, fmt.Sprintf("filesystems do not match: declared as %s, got %s", gs.Filesystem, ds.PartitionFSType)
			}
		}

		// otherwise if we got here things are matching
		return true, ""
	}

	gadgetContains := func(vss []VolumeStructure, ds *OnDiskStructure) (bool, string) {
		reasonAbsent := ""
		for vssIdx := range vss {
			matches, reasonNotMatches := eq(ds, vss, vssIdx)
			if matches {
				return true, ""
			}
			// TODO: handle multiple error cases for DOS disks and fail early
			// for GPT disks since we should not have multiple non-empty reasons
			// for not matching for GPT disks, as that would require two YAML
			// structures with the same name to be considered as candidates for
			// a given on disk structure, and we do not allow duplicated
			// structure names in the YAML at all via ValidateVolume.
			//
			// For DOS, since we cannot check the partition names, there will
			// always be a reason if there was not a match, in which case we
			// only want to return an error after we have finished searching the
			// full haystack and didn't find any matches whatsoever. Note that
			// the YAML structure that "should" have matched the on disk one we
			// are looking for but doesn't because of some problem like wrong
			// size or wrong filesystem may not be the last one, so returning
			// only the last error like we do here is wrong. We should include
			// all reasons why so the user can see which structure was the
			// "closest" to what we were searching for so they can fix their
			// gadget.yaml or on disk partitions so that it matches.
			if reasonNotMatches != "" {
				reasonAbsent = reasonNotMatches
			}
		}

		if opts.AllowImplicitSystemData {
			// Handle the case of an implicit system-data role before giving up;
			// we used to allow system-data to be implicit from the gadget.yaml.
			// In that case we won't have system-data in the gadget volume but it
			// could be on the disk, so if after searching all the gadget
			// structures we don't find the disk structure, check if we might
			// be dealing with a structure that looks like the implicit
			// system-data that ubuntu-image would have created.
			if onDiskStructureIsLikelyImplicitSystemDataRole(gadgetVolume, diskVolume, *ds) {
				return true, ""
			}
		}

		return false, reasonAbsent
	}

	onDiskContains := func(dss []OnDiskStructure, vss []VolumeStructure, vssIdx int) (*OnDiskStructure, string) {
		reasonAbsent := ""
		for _, ds := range dss {
			matches, reasonNotMatches := eq(&ds, vss, vssIdx)
			if matches {
				return &ds, ""
			}
			// this has the effect of only returning the last non-empty reason
			// string
			if reasonNotMatches != "" {
				reasonAbsent = reasonNotMatches
			}
		}
		return nil, reasonAbsent
	}

	// check size of volumes
	lastUsableByte := quantity.Size(diskVolume.UsableSectorsEnd) * diskVolume.SectorSize
	if gadgetVolume.MinSize() > lastUsableByte {
		return nil, fmt.Errorf("device %v (last usable byte at %s) is too small to fit the requested minimal size (%s)", diskVolume.Device,
			lastUsableByte.IECString(), gadgetVolume.MinSize().IECString())
	}

	// check that the sizes of all structures in the gadget are multiples of
	// the disk sector size (unless the structure is the MBR)
	for _, vs := range gadgetVolume.Structure {
		if !vs.IsRoleMBR() {
			for _, sz := range []quantity.Size{vs.MinSize, vs.Size} {
				if sz%diskVolume.SectorSize != 0 {
					return nil, fmt.Errorf("gadget volume structure %q size is not a multiple of disk sector size %v",
						vs.Name, diskVolume.SectorSize)
				}
			}
		}
	}

	// Check if gadget schema is compatible with the disk, when defined
	if (!gadgetVolume.HasPartial(PartialSchema) || gadgetVolume.Schema != "") &&
		!isCompatibleSchema(gadgetVolume.Schema, diskVolume.Schema) {
		return nil, fmt.Errorf("disk partitioning schema %q doesn't match gadget schema %q", diskVolume.Schema, gadgetVolume.Schema)
	}

	// Check disk ID if defined in gadget
	if gadgetVolume.ID != "" && gadgetVolume.ID != diskVolume.ID {
		return nil, fmt.Errorf("disk ID %q doesn't match gadget volume ID %q", diskVolume.ID, gadgetVolume.ID)
	}

	// Check if all existing device partitions are also in gadget
	// (unless partial strucuture).
	if !gadgetVolume.HasPartial(PartialStructure) {
		for _, ds := range diskVolume.Structure {
			present, reasonAbsent := gadgetContains(gadgetVolume.Structure, &ds)
			if !present {
				if reasonAbsent != "" {
					// use the right format so that it can be
					// appended to the error message
					reasonAbsent = fmt.Sprintf(": %s", reasonAbsent)
				}
				return nil, fmt.Errorf("cannot find disk partition %s (starting at %d) in gadget%s", ds.Node, ds.StartOffset, reasonAbsent)
			}
		}
	}

	// check all structures in the gadget are present on the disk, or have a
	// valid excuse for absence (i.e. mbr or creatable structures at install)
	var prevDs *OnDiskStructure
	for vssIdx, gs := range gadgetVolume.Structure {
		// we ignore reasonAbsent here since if there was an extra on disk
		// structure that didn't match something in the YAML, we would have
		// caught it above, this loop can only ever identify structures in the
		// YAML that are not on disk at all
		if ds, _ := onDiskContains(diskVolume.Structure, gadgetVolume.Structure, vssIdx); ds != nil {
			gadgetStructIdxToOnDiskStruct[gs.YamlIndex] = ds
			prevDs = ds
			continue
		}

		// otherwise not present, figure out if it has a valid excuse

		if !gs.IsPartition() {
			// Raw structures like mbr or other "bare" type will not be
			// identified by linux and thus should be skipped as they will not
			// show up on the disk. However, we insert a value in the map,
			// assuming they are where expected.
			offset := gs.Offset
			if offset == nil {
				// Case only possible if min-size is being used so we are in
				// an update. We will always have prevDs set because as a
				// minimum the first partition will have offset defined.
				// In any case, if using bare partitions, it is not a great
				// idea to have some previous partition with a valid range
				// of sizes.
				offsetV := prevDs.StartOffset + quantity.Offset(prevDs.Size)
				offset = &offsetV
			}
			ds := &OnDiskStructure{
				Name:        gs.Name,
				Type:        gs.Type,
				StartOffset: *offset,
				Size:        gs.Size,
			}
			gadgetStructIdxToOnDiskStruct[gs.YamlIndex] = ds
			prevDs = ds
			continue
		}

		// allow structures that are creatable during install if we don't assume
		// created partitions to already exist
		if IsCreatableAtInstall(&gs) && !opts.AssumeCreatablePartitionsCreated {
			continue
		}

		return nil, fmt.Errorf("cannot find gadget structure %q on disk", gs.Name)
	}

	// finally ensure that all encrypted partitions mentioned in the options are
	// present in the gadget.yaml (and thus will also need to have been present
	// on the disk)
	for gadgetLabel := range opts.ExpectedStructureEncryption {
		found := false
		for _, gs := range gadgetVolume.Structure {
			if gs.Name == gadgetLabel {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("expected encrypted structure %s not present in gadget", gadgetLabel)
		}
	}

	return gadgetStructIdxToOnDiskStruct, nil
}

// TODO:ICE: remove this as we only support LUKS (and ICE is a variant of LUKS now)
type DiskEncryptionMethod string

const (
	// values for the "method" key of encrypted structure information

	// standard LUKS as it is used for automatic FDE using SecureBoot and TPM
	// 2.0 in UC20+
	EncryptionLUKS DiskEncryptionMethod = "LUKS"
)

// DiskVolumeValidationOptions is a set of options on how to validate a disk to
// volume mapping for a specific disk/volume pair. It is closely related to the
// options provided to EnsureVolumeCompatibility via
// EnsureVolumeCompatibilityOptions.
type DiskVolumeValidationOptions struct {
	// AllowImplicitSystemData has the same meaning as the eponymously named
	// field in VolumeCompatibilityOptions.
	AllowImplicitSystemData bool
	// ExpectedEncryptedPartitions is a map of the names (gadget structure
	// names) of partitions that are encrypted on the volume and information
	// about that encryption.
	ExpectedStructureEncryption map[string]StructureEncryptionParameters
}

// DiskTraitsFromDeviceAndValidate takes a gadget volume and an
// expected disk device path and confirms that they are compatible,
// and then builds up the disk volume traits for that device. If the
// laid out volume is not compatible with the disk structure for the
// specified device an error is returned.
func DiskTraitsFromDeviceAndValidate(vol *Volume, dev string, opts *DiskVolumeValidationOptions) (res DiskVolumeDeviceTraits, err error) {
	if opts == nil {
		opts = &DiskVolumeValidationOptions{}
	}

	// get the disk layout for this device
	diskLayout := mylog.Check2(OnDiskVolumeFromDevice(dev))

	// ensure that the on disk volume and the gadget volume are actually
	// compatible
	volCompatOpts := &VolumeCompatibilityOptions{
		// at this point all partitions should be created
		AssumeCreatablePartitionsCreated: true,

		// provide the other opts as we were provided
		AllowImplicitSystemData:     opts.AllowImplicitSystemData,
		ExpectedStructureEncryption: opts.ExpectedStructureEncryption,
	}
	gadgetToDiskStruct := mylog.Check2(EnsureVolumeCompatibility(vol, diskLayout, volCompatOpts))

	// also get a Disk{} interface for this device
	disk := mylog.Check2(disks.DiskFromDeviceName(dev))

	diskPartitions := mylog.Check2(disk.Partitions())

	// make a map of start offsets to partitions for lookup
	diskPartitionsByOffset := make(map[uint64]disks.Partition, len(diskPartitions))
	for _, p := range diskPartitions {
		diskPartitionsByOffset[p.StartInBytes] = p
	}

	mappedStructures := make([]DiskStructureDeviceTraits, 0, len(diskLayout.Structure))

	// create the traits for each structure looping over the gadget structures
	// to ensure that extra partitions don't sneak in - we double check things
	// again below this loop
	for _, structure := range vol.Structure {
		// don't create traits for non-partitions, there is nothing we can
		// measure on the disk about bare structures other than perhaps reading
		// their content - the fact that bare structures do not overlap with
		// real partitions will have been validated when the YAML was validated
		// previously
		if !structure.IsPartition() {
			continue
		}

		ds, ok := gadgetToDiskStruct[structure.YamlIndex]
		if !ok {
			return res, fmt.Errorf("internal error: all disk structures should have been matched")
		}

		part, ok := diskPartitionsByOffset[uint64(ds.StartOffset)]
		if !ok {
			// unexpected error - somehow this structure's start offset is not
			// present in the OnDiskVolume, which is unexpected because we
			// validated that the gadget volume structure matches the on disk
			// volume
			return res, fmt.Errorf("internal error: inconsistent disk structures from gadget and disks.Disk: structure starting at %d missing on disk", ds.StartOffset)
		}
		ms := DiskStructureDeviceTraits{
			Size:               quantity.Size(part.SizeInBytes),
			Offset:             quantity.Offset(part.StartInBytes),
			PartitionUUID:      part.PartitionUUID,
			OriginalKernelPath: part.KernelDeviceNode,
			OriginalDevicePath: part.KernelDevicePath,
			PartitionType:      part.PartitionType,
			PartitionLabel:     part.PartitionLabel,  // this will be empty on dos disks
			FilesystemLabel:    part.FilesystemLabel, // blkid encoded
			FilesystemUUID:     part.FilesystemUUID,  // blkid encoded
			FilesystemType:     part.FilesystemType,
		}

		mappedStructures = append(mappedStructures, ms)

		// delete this partition from the map
		delete(diskPartitionsByOffset, uint64(ds.StartOffset))
	}

	// We should have deleted all structures from diskPartitionsByOffset that
	// are in the gadget.yaml volume, however there is a small
	// possibility (mainly due to bugs) where we could still have partitions in
	// diskPartitionsByOffset. So we check to make sure there are no partitions
	// left over.
	// However, the one notable exception to this is in the case of legacy UC16
	// or UC18 gadgets where the system-data role could have been left out and
	// ubuntu-image would dynamically create the partition. In this case, we
	// ought to just ignore this on-disk structure since it is not in the
	// gadget.yaml, and the primary use case of tracking disks and structures is
	// for gadget asset update, but by definition something which is not in the
	// gadget.yaml cannot be updated via gadget asset updates.
	switch len(diskPartitionsByOffset) {
	case 0:
		// expected, no implicit system-data
		break
	case 1:
		// could be implicit system-data
		if opts.AllowImplicitSystemData {
			var part disks.Partition
			for _, part = range diskPartitionsByOffset {
				break
			}

			s := mylog.Check2(OnDiskStructureFromPartition(part))

			if onDiskStructureIsLikelyImplicitSystemDataRole(vol, diskLayout, s) {
				// it is likely the implicit system-data
				logger.Debugf("Identified implicit system-data role on system as %s", s.Node)
				break
			}
		}
		fallthrough
	default:
		// we for sure have left over partitions that should have been in the
		// gadget.yaml - make a nice string with what partitions are leftover
		leftovers := []string{}
		for _, part := range diskPartitionsByOffset {
			leftovers = append(leftovers, part.KernelDeviceNode)
		}
		if vol.HasPartial(PartialStructure) {
			logger.Debugf("additional partitions on disk %s ignored as the gadget has partial structures: %v", disk.KernelDeviceNode(), leftovers)
		} else {
			// this is an internal error because to get here we would have had to
			// pass validation in EnsureVolumeCompatibility but then still have
			// extra partitions - the only non-buggy situation where that function
			// passes validation but leaves partitions on disk not in the YAML is
			// the implicit system-data role handled above
			return res, fmt.Errorf("internal error: unexpected additional partitions on disk %s not present in the gadget layout: %v", disk.KernelDeviceNode(), leftovers)
		}
	}

	return DiskVolumeDeviceTraits{
		OriginalDevicePath:  disk.KernelDevicePath(),
		OriginalKernelPath:  dev,
		DiskID:              diskLayout.ID,
		Structure:           mappedStructures,
		Size:                diskLayout.Size,
		SectorSize:          diskLayout.SectorSize,
		Schema:              disk.Schema(),
		StructureEncryption: opts.ExpectedStructureEncryption,
	}, nil
}

// unable to proceed with the gadget asset update, but not fatal to the refresh
// operation itself
var errSkipUpdateProceedRefresh = errors.New("cannot identify disk for gadget asset update")

// buildNewVolumeToDeviceMapping builds a DiskVolumeDeviceTraits for only the
// volume containing the system-boot role, when we cannot load an existing
// traits object from disk-mapping.json. It is meant to be used only with all
// UC16/UC18 installs as well as UC20 installs from before we started writing
// disk-mapping.json during install mode.
func buildNewVolumeToDeviceMapping(mod Model, old GadgetData, vols map[string]*Volume) (map[string]DiskVolumeDeviceTraits, error) {
	var likelySystemBootVolume string

	isPreUC20 := (mod.Grade() == asserts.ModelGradeUnset)

	if len(old.Info.Volumes) == 1 {
		// If we only have one volume, then that is the volume we are concerned
		// with, we do not validate that it has a system-boot role on it like
		// we do in the multi-volume case below, this is because we used to
		// allow installation of gadgets that have no system-boot role on them
		// at all

		// then we only have one volume to be concerned with
		for volName := range old.Info.Volumes {
			likelySystemBootVolume = volName
		}
	} else {
		// we need to pick the volume, since updates for this setup are best
		// effort and mainly focused on the main volume with system-* roles
		// on it, we need to pick the volume with that role
	volumeLoop:
		for volName, vol := range old.Info.Volumes {
			for _, structure := range vol.Structure {
				if structure.Role == SystemBoot {
					// this is the volume
					likelySystemBootVolume = volName
					break volumeLoop
				}
			}
		}
	}

	if likelySystemBootVolume == "" {
		// this is only possible in the case where there is more than one volume
		// and we didn't find system-boot anywhere, in this case for pre-UC20
		// we use a non-fatal error and just don't perform any update - this was
		// always the old behavior so we are not regressing here
		if isPreUC20 {
			logger.Noticef("WARNING: cannot identify disk for gadget asset update of volume %s: unable to find any volume with system-boot role on it", likelySystemBootVolume)
			return nil, errSkipUpdateProceedRefresh
		}

		// on UC20 and later however this is a fatal error, we should never have
		// allowed installation of a gadget which does not have the system-boot
		// role on it
		return nil, fmt.Errorf("cannot find any volume with system-boot, gadget is broken")
	}

	vol := vols[likelySystemBootVolume]

	// search for matching devices that correspond to the gadget volume
	dev := ""
	for i := range vol.Structure {
		// here it is okay that we require there to be either a partition label
		// or a filesystem label since we require there to be a system-boot role
		// on this volume which by definition must have a filesystem
		structureDevice := mylog.Check2(FindDeviceForStructure(&vol.Structure[i]))
		if err == ErrDeviceNotFound {
			continue
		}

		// TODO: should this be a fatal error?

		// we found a device for this structure, get the parent disk
		// and save that as the device for this volume
		disk := mylog.Check2(disks.DiskFromPartitionDeviceNode(structureDevice))

		// TODO: should we keep looping instead and try again with
		// another structure? it probably wouldn't work because we found
		// something on disk with the same name as something from the
		// gadget.yaml, but then we failed to make a disk from that
		// partition which suggests something is inconsistent with the
		// state of the disk/udev database

		dev = disk.KernelDeviceNode()
		break
	}

	if dev == "" {
		// couldn't find a disk at all, pre-UC20 we just warn about this
		// but let the update continue
		if isPreUC20 {
			logger.Noticef("WARNING: cannot identify disk for gadget asset update of volume %s", likelySystemBootVolume)
			return nil, errSkipUpdateProceedRefresh
		}
		// fatal error on UC20+
		return nil, fmt.Errorf("cannot identify disk for gadget asset update of volume %s", likelySystemBootVolume)
	}

	// we found the device, construct the traits with validation options
	validateOpts := &DiskVolumeValidationOptions{
		// allow implicit system-data on pre-uc20 only
		AllowImplicitSystemData: isPreUC20,
	}

	// setup encrypted structure information to perform validation if this
	// device used encryption
	if !isPreUC20 {
		// TODO: this needs to check if the specified partitions are ICE when
		// we support ICE too

		// check if there is a marker file written, that will indicate if
		// encryption was turned on
		if device.HasEncryptedMarkerUnder(dirs.SnapFDEDir) {
			// then we have the crypto marker file for encryption
			// cross-validation between ubuntu-data and ubuntu-save stored from
			// install mode, so mark ubuntu-save and data as expected to be
			// encrypted
			validateOpts.ExpectedStructureEncryption = map[string]StructureEncryptionParameters{
				"ubuntu-data": {Method: EncryptionLUKS},
				"ubuntu-save": {Method: EncryptionLUKS},
			}
		}
	}

	traits := mylog.Check2(DiskTraitsFromDeviceAndValidate(vol, dev, validateOpts))

	// TODO: should we save the traits here so they can be re-used in another
	// future update routine?

	return map[string]DiskVolumeDeviceTraits{
		likelySystemBootVolume: traits,
	}, nil
}

// StructureLocation represents the location of a structure for updating
// purposes. Either Device + Offset must be set for a raw structure without a
// filesystem, or RootMountPoint must be set for structures with a
// filesystem.
type StructureLocation struct {
	// Device is the kernel device node path such as /dev/vda1 for the
	// structure's backing physical disk.
	Device string
	// Offset is the offset from 0 for the physical disk that this structure
	// starts at.
	Offset quantity.Offset

	// RootMountPoint is the directory where the root directory of the structure
	// is mounted read/write. There may be other mount points for this structure
	// on the system, but this one is guaranteed to be writable and thus
	// suitable for gadget asset updates.
	RootMountPoint string
}

// buildVolumeStructureToLocation builds a map of gadget volumes to
// locations and to matched disk structures.
func buildVolumeStructureToLocation(mod Model,
	old GadgetData,
	vols map[string]*Volume,
	volToDeviceMapping map[string]DiskVolumeDeviceTraits,
	missingInitialMapping bool,
) (map[string]map[int]StructureLocation, map[string]map[int]*OnDiskStructure, error) {
	isPreUC20 := (mod.Grade() == asserts.ModelGradeUnset)

	// helper function for handling non-fatal errors on pre-UC20
	maybeFatalError := func(err error) error {
		if missingInitialMapping && isPreUC20 {
			// this is not a fatal error on pre-UC20
			logger.Noticef("WARNING: not applying gadget asset updates on main system-boot volume due to error mapping volume to physical disk: %v", err)
			return errSkipUpdateProceedRefresh
		}
		return err
	}

	volumeStructureToLocation := make(map[string]map[int]StructureLocation, len(old.Info.Volumes))
	gadgetVolToPartMap := make(map[string]map[int]*OnDiskStructure, len(old.Info.Volumes))

	// now for each volume, iterate over the structures, putting the
	// necessary info into the map for that volume as we iterate

	// this loop assumes that none of those things are different between the
	// new and old volume, which may not be true in the case where an
	// unsupported structure change is present in the new one, but we check that
	// situation after we have built the mapping
	for volName, diskDeviceTraits := range volToDeviceMapping {
		volumeStructureToLocation[volName] = make(map[int]StructureLocation)
		gadgetVolToPartMap[volName] = make(map[int]*OnDiskStructure)
		oldVol, ok := old.Info.Volumes[volName]
		if !ok {
			return nil, nil, fmt.Errorf("internal error: volume %s not present in gadget.yaml but present in traits mapping", volName)
		}

		newVol := vols[volName]
		if newVol == nil {
			return nil, nil, fmt.Errorf("internal error: missing volume %s", volName)
		}

		// find the disk associated with this volume using the traits we
		// measured for this volume
		validateOpts := &DiskVolumeValidationOptions{
			// implicit system-data role only allowed on pre UC20 systems
			AllowImplicitSystemData:     isPreUC20,
			ExpectedStructureEncryption: diskDeviceTraits.StructureEncryption,
		}

		disk, gadgetToDiskStruct := mylog.Check3(searchVolumeWithTraitsAndMatchParts(newVol, diskDeviceTraits, validateOpts))

		gadgetVolToPartMap[volName] = gadgetToDiskStruct

		// the index here is 0-based and is equal to VolumeStructure.YamlIndex
		for volYamlIndex, volStruct := range oldVol.Structure {
			structStartOffset := gadgetToDiskStruct[volYamlIndex].StartOffset

			loc := StructureLocation{}

			if volStruct.HasFilesystem() {
				// Here we know what disk is associated with this volume, so we
				// just need to find what partition is associated with this
				// structure to find it's root mount points. On GPT since
				// partition labels/names are unique in the partition table, we
				// could do a lookup by matching partition label, but this won't
				// work on MBR which doesn't have such a concept, so instead we
				// use the start offset to locate which disk partition this
				// structure is equal to.

				partitions := mylog.Check2(disk.Partitions())

				var foundP disks.Partition
				found := false
				for _, p := range partitions {
					if p.StartInBytes == uint64(structStartOffset) {
						foundP = p
						found = true
						break
					}
				}
				if !found {
					dieErr := fmt.Errorf("cannot locate structure %d on volume %s: no matching start offset", volYamlIndex, volName)
					return nil, nil, maybeFatalError(dieErr)
				}

				// if this structure is an encrypted one, then we can't just
				// get the root mount points for the device node, we would need
				// to find the decrypted mapper device for the encrypted device
				// node and then find the root mount point of the mapper device
				if _, ok := diskDeviceTraits.StructureEncryption[volStruct.Name]; ok {
					logger.Noticef("gadget asset update for assets on encrypted partition %s unsupported", volStruct.Name)

					// leaving this structure as an empty location will
					// mean when an update to this structure is actually
					// performed it will fail, but we won't fail updates to
					// other structures - it is treated like an unmounted
					// partition
					volumeStructureToLocation[volName][volYamlIndex] = loc
					continue
				}

				// otherwise normal unencrypted filesystem, find the rw mount
				// points
				mountpts := mylog.Check2(disks.MountPointsForPartitionRoot(foundP, map[string]string{"rw": ""}))

				var mountpt string
				if len(mountpts) == 0 {
					// this filesystem is not already mounted, we probably
					// should mount it in order to proceed with the update?

					// TODO: do something better here?
					logger.Noticef("structure %d on volume %s (%s) is not mounted read/write anywhere to be able to update it", volYamlIndex, volName, foundP.KernelDeviceNode)
				} else {
					// use the first one, it doesn't really matter to us
					// which one is used to update the contents
					mountpt = mountpts[0]
				}
				loc.RootMountPoint = mountpt
			} else {
				// no filesystem, the device for this one is just the device
				// for the disk itself
				loc.Device = disk.KernelDeviceNode()
				loc.Offset = structStartOffset
			}

			volumeStructureToLocation[volName][volYamlIndex] = loc
		}
	}

	return volumeStructureToLocation, gadgetVolToPartMap, nil
}

func MockVolumeStructureToLocationMap(f func(_ GadgetData, _ Model, _ map[string]*Volume) (
	map[string]map[int]StructureLocation, map[string]map[int]*OnDiskStructure, error),
) (restore func()) {
	old := volumeStructureToLocationMap
	volumeStructureToLocationMap = f
	return func() {
		volumeStructureToLocationMap = old
	}
}

// use indirection to allow mocking
var volumeStructureToLocationMap = volumeStructureToLocationMapImpl

// volumeStructureToLocationMapImpl builds a map of gadget structures
// to locations and to matched disk structures. For the locations, the
// first key is the volume name, and the second key is the structure's
// index in the list of structures on that volume. The value is the
// StructureLocation that can actually be used to perform the
// lookup/update in applyUpdates. For the matched disk, the first key
// is the volume name and the second key is the yaml index of the
// structure in the gadget definition. The value is the disk structure
// that matches the gadget description.
func volumeStructureToLocationMapImpl(old GadgetData, mod Model, vols map[string]*Volume) (
	map[string]map[int]StructureLocation, map[string]map[int]*OnDiskStructure, error,
) {
	// first try to load the disk-mapping.json volume trait info
	volToDeviceMapping := mylog.Check2(LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDir))

	missingInitialMapping := false

	// check if we had no mapping, if so then we try our best to build a mapping
	// for the system-boot volume only to perform gadget asset updates there
	// but if we fail to build a mapping, then on UC18 we non-fatally return
	// without doing any updates, but on UC20 we fail the refresh because we
	// expect UC20's gadget.yaml validation to be robust
	if len(volToDeviceMapping) == 0 {
		// then there was no mapping provided, this is a system which never
		// performed the initial saving of disk/volume mapping info during
		// install, so we build up a mapping specifically just for the
		// volume with the system-boot role on it

		// TODO: after we calculate this the first time should we save a new
		// disk-mapping.json with this information and some marker that this
		// was not calculated at first boot but a later date?

		// TODO: the rest of this function in this case is technically not as
		// efficient as we could be, since we build up these heuristics here and
		// then immediately below treat them as if they were from the initial
		// install boot and thus could have changed, but there is no way for
		// this mapping to have changed between when this code runs here and the
		// code below, but in the interest of sharing the same codepath for all
		// cases below, we treat this heuristic mapping data the same
		missingInitialMapping = true

		volToDeviceMapping = mylog.Check2(buildNewVolumeToDeviceMapping(mod, old, vols))

		// volToDeviceMapping should always be of length one
		var volName string
		for volName = range volToDeviceMapping {
			break
		}

		// if there are multiple volumes leave a message that we are only
		// performing updates for the volume with the system-boot role
		if len(old.Info.Volumes) != 1 {
			logger.Noticef("WARNING: gadget has multiple volumes but updates are only being performed for volume %s", volName)
		}
	}

	// now that we have some traits about the volume -> disk mapping, either
	// because we just constructed it or that we were provided it the .json file
	// we have to build up a map for the updaters to use to find the structure
	// location to update given the VolumeStructure
	return buildVolumeStructureToLocation(
		mod,
		old,
		vols,
		volToDeviceMapping,
		missingInitialMapping,
	)
}

// Update applies the gadget update given the gadget information and data from
// old and new revisions. It errors out when the update is not possible or
// illegal, or a failure occurs at any of the steps. When there is no update, a
// special error ErrNoUpdate is returned.
//
// Only structures selected by the update policy are part of the update. When
// the policy is nil, a default one is used. The default policy selects
// structures in an opt-in manner, only tructures with a higher value of Edition
// field in the new gadget definition are part of the update.
//
// Data that would be modified during the update is first backed up inside the
// rollback directory. Should the apply step fail, the modified data is
// recovered.
//
// The rules for gadget/kernel updates with "$kernel:refs":
//
//  1. When installing a kernel with assets that have "update: true"
//     there *must* be a matching entry in gadget.yaml. If not we risk
//     bricking the system because the kernel tells us that it *needs*
//     this file to boot but without gadget.yaml we would not put it
//     anywhere.
//  2. When installing a gadget with "$kernel:ref" content it is okay
//     if this content cannot get resolved as long as there is no
//     "edition" jump. This means adding new "$kernel:ref" without
//     "edition" updates is always possible.
//
// To add a new "$kernel:ref" to gadget/kernel:
// a. Update gadget and gadget.yaml and add "$kernel:ref" but do not update
// edition (if edition update is needed, use epoch)
// b. Update kernel and kernel.yaml with new assets.
// c. snapd will refresh gadget (see rule 2) but refuse to take the	new
// kernel (rule 1)
// d. After step (c) is completed the kernel refresh will now also work (no more
// violation of rule 1)
func Update(model Model, old, new GadgetData, rollbackDirPath string, updatePolicy UpdatePolicyFunc, observer ContentUpdateObserver) error {
	// if the volumes from the old and the new gadgets do not match, then fail -
	// we don't support adding or removing volumes from the gadget.yaml
	newVolumes := make([]string, 0, len(new.Info.Volumes))
	oldVolumes := make([]string, 0, len(old.Info.Volumes))
	for newVol := range new.Info.Volumes {
		newVolumes = append(newVolumes, newVol)
	}
	for oldVol := range old.Info.Volumes {
		oldVolumes = append(oldVolumes, oldVol)
	}
	common := strutil.Intersection(newVolumes, oldVolumes)
	// check dissimilar cases between common, new and old
	switch {
	case len(common) != len(newVolumes) && len(common) != len(oldVolumes):
		// there are both volumes removed from old and volumes added to new
		return fmt.Errorf("cannot update gadget assets: volumes were both added and removed")
	case len(common) != len(newVolumes):
		// then there are volumes in old that are not in new, i.e. a volume
		// was removed
		return fmt.Errorf("cannot update gadget assets: volumes were removed")
	case len(common) != len(oldVolumes):
		// then there are volumes in new that are not in old, i.e. a volume
		// was added
		return fmt.Errorf("cannot update gadget assets: volumes were added")
	}

	if updatePolicy == nil {
		updatePolicy = defaultPolicy
	}

	// collect the updates and validate that they are doable from an abstract
	// sense first

	// note that this code is written such that before we perform any update, we
	// validate that all updates are valid and that all volumes are compatible
	// between the old and the new state, this is to prevent applying valid
	// updates on one volume when another volume is invalid, if that's the case
	// we treat the whole gadget as invalid and return an error blocking the
	// refresh

	// TODO: should we handle the updates on multiple volumes in a
	// deterministic order? iterating over maps is not deterministic, but we
	// perform all updates at the end together in one call

	// ensure all required kernel assets are found in the gadget
	kernelInfo := mylog.Check2(kernel.ReadInfo(new.KernelRootDir))

	allKernelAssets := []string{}
	for assetName, asset := range kernelInfo.Assets {
		if !asset.Update {
			continue
		}
		allKernelAssets = append(allKernelAssets, assetName)
	}

	atLeastOneKernelAssetConsumed := false

	// build the map of volume structures to locations and of disk strucutures
	structureLocations, volToPartsMap := mylog.Check3(volumeStructureToLocationMap(old, model, new.Info.Volumes))

	// we couldn't successfully build a map for the structure locations,
	// but for various reasons this isn't considered a fatal error for
	// the gadget refresh, so just return nil instead, a message should
	// already have been logged

	// Layout new volume, delay resolving of filesystem content
	opts := &LayoutOptions{
		SkipResolveContent: true,
		GadgetRootDir:      new.RootDir,
		KernelRootDir:      new.KernelRootDir,
	}

	allUpdates := []updatePair{}
	laidOutVols := map[string]*LaidOutVolume{}
	for volName, oldVol := range old.Info.Volumes {
		newVol := new.Info.Volumes[volName]

		// layout old partially, without going deep into the layout of structure
		// content
		pOld := mylog.Check2(layoutVolumePartially(oldVol, volToPartsMap[volName]))

		pNew := mylog.Check2(LayoutVolume(newVol, volToPartsMap[volName], opts))

		laidOutVols[volName] = pNew
		mylog.Check(canUpdateVolume(pOld, pNew))

		// if we haven't consumed any kernel assets yet check if this volume
		// consumes at least one - we require at least one asset to be consumed
		// by some volume in the gadget
		if !atLeastOneKernelAssetConsumed {
			consumed := mylog.Check2(gadgetVolumeKernelUpdateAssetsConsumed(pNew.Volume, kernelInfo))

			atLeastOneKernelAssetConsumed = consumed
		}

		// now we know which structure is which, find which ones need an update
		updates := mylog.Check2(resolveUpdate(pOld, pNew, updatePolicy, new.RootDir, new.KernelRootDir, kernelInfo))

		// can update old layout to new layout
		for _, update := range updates {
			fromIdx := mylog.Check2(oldVol.yamlIdxToStructureIdx(update.from.VolumeStructure.YamlIndex))

			toIdx := mylog.Check2(oldVol.yamlIdxToStructureIdx(update.from.VolumeStructure.YamlIndex))
			mylog.Check(canUpdateStructure(oldVol, fromIdx, newVol, toIdx))

		}

		// collect updates per volume into a single set of updates to perform
		// at once
		allUpdates = append(allUpdates, updates...)
	}

	// check if there were kernel assets that at least one was consumed across
	// any of the volumes
	if len(allKernelAssets) != 0 && !atLeastOneKernelAssetConsumed {
		sort.Strings(allKernelAssets)
		return fmt.Errorf("gadget does not consume any of the kernel assets needing synced update %s", strutil.Quoted(allKernelAssets))
	}

	if len(allUpdates) == 0 {
		// nothing to update
		return ErrNoUpdate
	}

	if len(new.Info.Volumes) != 1 {
		logger.Debugf("gadget asset update routine for multiple volumes")

		// check if the structure location map has only one volume in it - this
		// is the case in legacy update operations where we only support updates
		// to the system-boot / main volume
		if len(structureLocations) == 1 {
			// log a message and drop all updates to structures not in the
			// volume we have
			supportedVolume := ""
			for volName := range structureLocations {
				supportedVolume = volName
			}
			keepUpdates := make([]updatePair, 0, len(allUpdates))
			for _, update := range allUpdates {
				if update.volume.Name != supportedVolume {
					// TODO: or should we error here instead?
					logger.Noticef("skipping update on non-supported volume %s to structure %s", update.volume.Name, update.to.Name())
				} else {
					keepUpdates = append(keepUpdates, update)
				}
			}
			allUpdates = keepUpdates
		}
	}
	mylog.Check(

		// apply all updates at once
		applyUpdates(structureLocations, new, allUpdates, rollbackDirPath, observer))

	return nil
}

func resolveVolume(old *Info, new *Info) (oldVol, newVol *Volume, err error) {
	// support only one volume
	if len(new.Volumes) != 1 || len(old.Volumes) != 1 {
		return nil, nil, errors.New("cannot update with more than one volume")
	}

	var name string
	for n := range old.Volumes {
		name = n
		break
	}
	oldV := old.Volumes[name]

	newV, ok := new.Volumes[name]
	if !ok {
		return nil, nil, fmt.Errorf("cannot find entry for volume %q in updated gadget info", name)
	}

	return oldV, newV, nil
}

func isLegacyMBRTransition(from *VolumeStructure, to *VolumeStructure) bool {
	// legacy MBR could have been specified by setting type: mbr, with no
	// role
	return from.Type == schemaMBR && to.Role == schemaMBR
}

func effectivePartSize(part *VolumeStructure) quantity.Size {
	// Partitions with partial size are set as unbounded (their Size field is 0)
	if part.hasPartialSize() {
		return UnboundedStructureSize
	}
	return part.Size
}

func arePossibleSizesCompatible(from *VolumeStructure, to *VolumeStructure) bool {
	// Check if [from.MinSize,from.Size], the interval of sizes allowed in
	// "from", intersects with [to.MinSize,to.Size] (the interval of sizes
	// allowed in "to"). When both checks are true we know some overlap
	// between the segments is happening (that this is right can be
	// visualized by sliding a segment over the abscissa while the other is
	// fixed, for a moving segment either smaller or bigger than the fixed
	// one).
	return effectivePartSize(from) >= to.MinSize && from.MinSize <= effectivePartSize(to)
}

func arePossibleOffsetsCompatible(vss1 []VolumeStructure, idx1 int, vss2 []VolumeStructure, idx2 int) bool {
	// See comment in arePossibleSizesCompatible, this is the same check but
	// for offsets instead of sizes.
	return maxStructureOffset(vss1, idx1) >= minStructureOffset(vss2, idx2) &&
		minStructureOffset(vss1, idx1) <= maxStructureOffset(vss2, idx2)
}

func arePartitionTypesCompatible(from, to *VolumeStructure) bool {
	// As far as there is an intersection on the possible types we are fine
	fromTs := strings.Split(from.Type, ",")
	toTs := strings.Split(to.Type, ",")
	for _, tp := range fromTs {
		if strutil.ListContains(toTs, tp) {
			return true
		}
	}

	if isLegacyMBRTransition(from, to) {
		return true
	}

	return false
}

// canUpdateStructure checks gadget compatibility on updates, looking only at
// features that are not reflected on the installed disk (for this we check
// elsewhere the new gadget against the actual disk content).
//
// Partial properties are not checked as they will be checked against the real
// disk later, in EnsureVolumeCompatibility. TODO Some checks should maybe
// happen only there even for non-partial gadgets.
func canUpdateStructure(fromV *Volume, fromIdx int, toV *Volume, toIdx int) error {
	from := &fromV.Structure[fromIdx]
	to := &toV.Structure[toIdx]
	if !toV.HasPartial(PartialSchema) && toV.Schema == schemaGPT && from.Name != to.Name {
		// partition names are only effective when GPT is used
		return fmt.Errorf("cannot change structure name from %q to %q",
			from.Name, to.Name)
	}
	if !arePossibleSizesCompatible(from, to) {
		return fmt.Errorf("new valid structure size range [%v, %v] is not compatible with current ([%v, %v])",
			to.MinSize, effectivePartSize(to), from.MinSize, effectivePartSize(from))
	}
	if !arePossibleOffsetsCompatible(fromV.Structure, fromIdx, toV.Structure, toIdx) {
		return fmt.Errorf("new valid structure offset range [%v, %v] is not compatible with current ([%v, %v])",
			minStructureOffset(toV.Structure, toIdx), maxStructureOffset(toV.Structure, toIdx), minStructureOffset(fromV.Structure, fromIdx), maxStructureOffset(fromV.Structure, fromIdx))
	}
	if from.Role != to.Role {
		return fmt.Errorf("cannot change structure role from %q to %q",
			from.Role, to.Role)
	}
	if !arePartitionTypesCompatible(from, to) {
		return fmt.Errorf("cannot change structure type from %q to %q",
			from.Type, to.Type)
	}
	if from.ID != to.ID {
		return fmt.Errorf("cannot change structure ID from %q to %q", from.ID, to.ID)
	}
	if to.HasFilesystem() {
		if !from.HasFilesystem() {
			return fmt.Errorf("cannot change a bare structure to filesystem one")
		}
		// If partial filesystem we have an empty string. Here we allow
		// moving from undefined filesystem to defined one, but not from
		// defined to undefined, or changing defined filesystem.
		if from.Filesystem != "" && from.Filesystem != to.Filesystem {
			return fmt.Errorf("cannot change filesystem from %q to %q",
				from.Filesystem, to.Filesystem)
		}
		if from.Label != to.Label {
			return fmt.Errorf("cannot change filesystem label from %q to %q",
				from.Label, to.Label)
		}
	} else {
		if from.HasFilesystem() {
			return fmt.Errorf("cannot change a filesystem structure to a bare one")
		}
	}

	return nil
}

func canUpdateVolume(from *PartiallyLaidOutVolume, to *LaidOutVolume) error {
	if from.ID != to.ID {
		return fmt.Errorf("cannot change volume ID from %q to %q", from.ID, to.ID)
	}
	mylog.Check(checkCompatibleSchema(from.Volume, to.Volume))

	if len(from.LaidOutStructure) != len(to.LaidOutStructure) {
		return fmt.Errorf("cannot change the number of structures within volume from %v to %v", len(from.LaidOutStructure), len(to.LaidOutStructure))
	}
	return nil
}

type updatePair struct {
	from   *LaidOutStructure
	to     *LaidOutStructure
	volume *Volume
}

func defaultPolicy(from, to *LaidOutStructure) (bool, ResolvedContentFilterFunc) {
	return to.VolumeStructure.Update.Edition > from.VolumeStructure.Update.Edition, nil
}

// RemodelUpdatePolicy implements the update policy of a remodel scenario. The
// policy selects all non-MBR structures for the update.
func RemodelUpdatePolicy(from, to *LaidOutStructure) (bool, ResolvedContentFilterFunc) {
	if from.Role() == schemaMBR {
		return false, nil
	}
	return true, nil
}

// KernelUpdatePolicy implements the update policy for kernel asset updates.
//
// This is called when there is a kernel->kernel refresh for kernels that
// contain bootloader assets. In this case all bootloader assets that are
// marked as "update: true" in the kernel.yaml need updating.
//
// But any non-kernel assets need to be ignored, they will be handled by
// the regular gadget->gadget update mechanism and policy.
func KernelUpdatePolicy(from, to *LaidOutStructure) (bool, ResolvedContentFilterFunc) {
	// The policy function has to work on unresolved content, the
	// returned filter will make sure that after resolving only the
	// relevant $kernel:refs are updated.
	for _, ct := range to.VolumeStructure.Content {
		if strings.HasPrefix(ct.UnresolvedSource, "$kernel:") {
			return true, func(rn *ResolvedContent) bool {
				return rn.KernelUpdate
			}
		}
	}

	return false, nil
}

func resolveUpdate(oldVol *PartiallyLaidOutVolume, newVol *LaidOutVolume, policy UpdatePolicyFunc, newGadgetRootDir, newKernelRootDir string, kernelInfo *kernel.Info) (updates []updatePair, err error) {
	if len(oldVol.LaidOutStructure) != len(newVol.LaidOutStructure) {
		return nil, errors.New("internal error: the number of structures in new and old volume definitions is different")
	}
	// We must order updates from the latest binary in the boot
	// chain to the newest. So any seed partitions should come
	// after boot partitions.
	var seedUpdates []updatePair
	var bootUpdates []updatePair
	for j, oldStruct := range oldVol.LaidOutStructure {
		newStruct := newVol.LaidOutStructure[j]
		updatesTarget := &updates
		if strings.HasPrefix(newStruct.Role(), "system-seed") {
			updatesTarget = &seedUpdates
		} else if strings.HasPrefix(newStruct.Role(), "system-boot") {
			updatesTarget = &bootUpdates
		}
		// update only when the policy says so; boot assets
		// are assumed to be backwards compatible, once
		// deployed they are not rolled back or replaced unless
		// told by the new policy
		if update, filter := policy(&oldStruct, &newStruct); update {
			// Ensure content is resolved and filtered. Filtering
			// is required for e.g. KernelUpdatePolicy, see above.
			resolvedContent := mylog.Check2(resolveVolumeContent(newGadgetRootDir, newKernelRootDir, kernelInfo, newStruct.VolumeStructure, filter))

			// No resolved or raw content that would need updating
			if len(resolvedContent) == 0 && len(newStruct.LaidOutContent) == 0 {
				continue
			}
			newVol.LaidOutStructure[j].ResolvedContent = resolvedContent

			// and add to updates
			*updatesTarget = append(*updatesTarget, updatePair{
				from:   &oldVol.LaidOutStructure[j],
				to:     &newVol.LaidOutStructure[j],
				volume: newVol.Volume,
			})
		}
	}
	updates = append(updates, bootUpdates...)
	updates = append(updates, seedUpdates...)
	return updates, nil
}

type Updater interface {
	// Update applies the update or errors out on failures. When no actual
	// update was applied because the new content is identical a special
	// ErrNoUpdate is returned.
	Update() error
	// Backup prepares a backup copy of data that will be modified by
	// Update()
	Backup() error
	// Rollback restores data modified by update
	Rollback() error
}

func updateLocationForStructure(structureLocations map[string]map[int]StructureLocation, ps *LaidOutStructure) (loc StructureLocation, err error) {
	loc, ok := structureLocations[ps.VolumeStructure.VolumeName][ps.VolumeStructure.YamlIndex]
	if !ok {
		return loc, fmt.Errorf("structure with index %d on volume %s not found", ps.VolumeStructure.YamlIndex, ps.VolumeStructure.VolumeName)
	}
	if !ps.HasFilesystem() {
		if loc.Device == "" {
			return loc, fmt.Errorf("internal error: structure %d on volume %s should have had a device set but did not have one in an internal mapping", ps.VolumeStructure.YamlIndex, ps.VolumeStructure.VolumeName)
		}
		return loc, nil
	} else {
		if loc.RootMountPoint == "" {
			// then we can't update this structure because it has a filesystem
			// specified in the gadget.yaml, but that partition is not mounted
			// anywhere writable for us to update the filesystem content
			// there is a TODO in buildVolumeStructureToLocation above about
			// possibly mounting it, we could also mount it here instead and
			// then proceed with the update, but we should also have a way to
			// unmount it when we are done updating it
			return loc, fmt.Errorf("structure %d on volume %s does not have a writable mountpoint in order to update the filesystem content", ps.VolumeStructure.YamlIndex, ps.VolumeStructure.VolumeName)
		}
		return loc, nil
	}
}

func applyUpdates(structureLocations map[string]map[int]StructureLocation, new GadgetData, updates []updatePair, rollbackDir string, observer ContentUpdateObserver) error {
	updaters := make([]Updater, len(updates))

	for i, one := range updates {
		loc := mylog.Check2(updateLocationForStructure(structureLocations, one.to))

		up := mylog.Check2(updaterForStructure(loc, one.from, one.to, new.RootDir, rollbackDir, observer))

		updaters[i] = up
	}

	var backupErr error
	for i, one := range updaters {
		mylog.Check(one.Backup())
	}
	if backupErr != nil {
		if observer != nil {
			mylog.Check(observer.Canceled())
		}
		return backupErr
	}
	if observer != nil {
		mylog.Check(observer.BeforeWrite())
	}

	var updateErr error
	var updateLastAttempted int
	var skipped int
	for i, one := range updaters {
		updateLastAttempted = i
		mylog.Check(one.Update())

	}
	if skipped == len(updaters) {
		// all updates were a noop
		return ErrNoUpdate
	}

	if updateErr == nil {
		// all good, updates applied successfully
		return nil
	}

	logger.Noticef("cannot update gadget: %v", updateErr)
	// not so good, rollback ones that got applied
	for i := 0; i <= updateLastAttempted; i++ {
		one := updaters[i]
		mylog.Check(one.Rollback())
		// TODO: log errors to oplog

	}

	if observer != nil {
		mylog.Check(observer.Canceled())
	}

	return updateErr
}

var updaterForStructure = updaterForStructureImpl

func updaterForStructureImpl(loc StructureLocation, fromPs *LaidOutStructure, ps *LaidOutStructure, newRootDir, rollbackDir string, observer ContentUpdateObserver) (Updater, error) {
	// TODO: this is sort of clunky, we already did the lookup, but doing the
	// lookup out of band from this function makes for easier mocking
	if !ps.HasFilesystem() {
		lookup := func(ps *LaidOutStructure) (device string, offs quantity.Offset, err error) {
			return loc.Device, loc.Offset, nil
		}
		return newRawStructureUpdater(newRootDir, ps, rollbackDir, lookup)
	} else {
		lookup := func(ps *LaidOutStructure) (string, error) {
			return loc.RootMountPoint, nil
		}
		return newMountedFilesystemUpdater(fromPs, ps, rollbackDir, lookup, observer)
	}
}

// MockUpdaterForStructure replace internal call with a mocked one, for use in tests only
func MockUpdaterForStructure(mock func(loc StructureLocation, fromPs, ps *LaidOutStructure, rootDir, rollbackDir string, observer ContentUpdateObserver) (Updater, error)) (restore func()) {
	old := updaterForStructure
	updaterForStructure = mock
	return func() {
		updaterForStructure = old
	}
}
