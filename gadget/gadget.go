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
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/gadget/edition"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/metautil"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/strutil"
)

const (
	// schemaMBR identifies a Master Boot Record partitioning schema, or an
	// MBR like role
	schemaMBR = "mbr"
	// schemaGPT identifies a GUID Partition Table partitioning schema
	schemaGPT = "gpt"

	SystemBoot     = "system-boot"
	SystemData     = "system-data"
	SystemSeed     = "system-seed"
	SystemSeedNull = "system-seed-null"
	SystemSave     = "system-save"

	// extracted kernels for all uc systems
	bootImage = "system-boot-image"

	// extracted kernels for recovery kernels for uc20 specifically
	seedBootImage = "system-seed-image"

	// bootloader specific partition which stores bootloader environment vars
	// for purposes of booting normal run mode on uc20 and all modes on
	// uc16 and uc18
	bootSelect = "system-boot-select"

	// bootloader specific partition which stores bootloader environment vars
	// for purposes of booting recovery systems on uc20, i.e. recover or install
	seedBootSelect = "system-seed-select"

	// implicitSystemDataLabel is the implicit filesystem label of structure
	// of system-data role
	implicitSystemDataLabel = "writable"

	// UC20 filesystem labels for roles
	ubuntuBootLabel = "ubuntu-boot"
	ubuntuSeedLabel = "ubuntu-seed"
	ubuntuDataLabel = "ubuntu-data"
	ubuntuSaveLabel = "ubuntu-save"

	// only supported for legacy reasons
	legacyBootImage  = "bootimg"
	legacyBootSelect = "bootselect"

	// UnboundedStructureOffset is the maximum effective partition offset
	// that we can handle.
	UnboundedStructureOffset = quantity.Offset(math.MaxUint64)

	// UnboundedStructureSize is the maximum effective partition size
	// that we can handle.
	UnboundedStructureSize = quantity.Size(math.MaxUint64)
)

var (
	validVolumeName = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9-]+$")
	validTypeID     = regexp.MustCompile("^[0-9A-F]{2}$")
	validGUUID      = regexp.MustCompile("^(?i)[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}$")
)

type KernelCmdline struct {
	// Allow is the list of allowed parameters for the system.kernel.cmdline-append
	// system option
	Allow []kcmdline.ArgumentPattern `yaml:"allow"`
	// Append are kernel parameters added by the gadget
	Append []kcmdline.Argument `yaml:"append"`
	// Remove are patterns to be removed from default command line
	Remove []kcmdline.ArgumentPattern `yaml:"remove"`
}

type Info struct {
	Volumes map[string]*Volume `yaml:"volumes,omitempty"`

	// Default configuration for snaps (snap-id => key => value).
	Defaults map[string]map[string]interface{} `yaml:"defaults,omitempty"`

	Connections []Connection `yaml:"connections"`

	KernelCmdline KernelCmdline `yaml:"kernel-cmdline"`
}

// HasRole returns true if any of the volume structures in this Info has the
// given role.
func (i *Info) HasRole(role string) bool {
	for _, v := range i.Volumes {
		for _, s := range v.Structure {
			if s.Role == role {
				return true
			}
		}
	}
	return false
}

// PartialProperty is a gadget property that can be partially defined.
type PartialProperty string

// These are the different properties of the gadget that can be partially
// defined.
// TODO What is the exact meaning of having a partial "structure" is not yet
// fully defined, so enforcing it has not been implemented yet.
const (
	PartialSize       PartialProperty = "size"
	PartialFilesystem PartialProperty = "filesystem"
	PartialSchema     PartialProperty = "schema"
	PartialStructure  PartialProperty = "structure"
)

var validPartialProperties = [...]PartialProperty{PartialSize, PartialFilesystem, PartialSchema, PartialStructure}

// Volume defines the structure and content for the image to be written into a
// block device.
type Volume struct {
	// Partial is a list of properties that are only only partially
	// described in the gadget and that need to be filled by an
	// installer.
	Partial []PartialProperty `yaml:"partial,omitempty" json:"partial,omitempty"`
	// Schema for the volume can be either gpt or mbr.
	Schema string `yaml:"schema" json:"schema"`
	// Bootloader names the bootloader used by the volume
	Bootloader string `yaml:"bootloader" json:"bootloader"`
	//  ID is a 2-hex digit disk ID or GPT GUID
	ID string `yaml:"id" json:"id"`
	// Structure describes the structures that are part of the volume
	Structure []VolumeStructure `yaml:"structure" json:"structure"`
	// Name is the name of the volume from the gadget.yaml
	Name string `json:"-"`
}

// HasPartial checks if the volume has a partially defined part.
func (v *Volume) HasPartial(pp PartialProperty) bool {
	for _, vp := range v.Partial {
		if vp == pp {
			return true
		}
	}
	return false
}

// MinSize returns the minimum size required by a volume, as implicitly
// defined by the size structures. It assumes sorted structures.
func (v *Volume) MinSize() quantity.Size {
	endVol := quantity.Offset(0)
	for _, s := range v.Structure {
		if s.Offset != nil {
			endVol = *s.Offset + quantity.Offset(s.MinSize)
		} else {
			endVol += quantity.Offset(s.MinSize)
		}
	}

	return quantity.Size(endVol)
}

// StructFromYamlIndex returns the structure defined at a given yaml index from
// the original yaml file.
func (v *Volume) StructFromYamlIndex(yamlIdx int) *VolumeStructure {
	i, err := v.yamlIdxToStructureIdx(yamlIdx)
	if err != nil {
		return nil
	}
	return &v.Structure[i]
}

// yamlIdxToStructureIdx returns the index to Volume.Structure that matches the
// yaml index from the original yaml file.
func (v *Volume) yamlIdxToStructureIdx(yamlIdx int) (int, error) {
	for i := range v.Structure {
		if v.Structure[i].YamlIndex == yamlIdx {
			return i, nil
		}
	}

	return -1, fmt.Errorf("structure with yaml index %d not found", yamlIdx)
}

// Copy makes a deep copy of the volume structure.
func (vs *VolumeStructure) Copy() *VolumeStructure {
	newVs := *vs
	if vs.Offset != nil {
		newVs.Offset = asOffsetPtr(*vs.Offset)
	}
	if vs.OffsetWrite != nil {
		offsetWr := *vs.OffsetWrite
		newVs.OffsetWrite = &offsetWr
	}
	if vs.Content != nil {
		newVs.Content = make([]VolumeContent, len(vs.Content))
		copy(newVs.Content, vs.Content)
		for i, c := range vs.Content {
			if c.Offset != nil {
				newC := &newVs.Content[i]
				newC.Offset = asOffsetPtr(*c.Offset)
			}
		}
	}
	return &newVs
}

// Copy makes a deep copy of the volume.
func (v *Volume) Copy() *Volume {
	newV := *v
	if v.Partial != nil {
		newV.Partial = make([]PartialProperty, len(v.Partial))
		copy(newV.Partial, v.Partial)
	}
	if v.Structure != nil {
		newV.Structure = make([]VolumeStructure, len(v.Structure))
		for i, vs := range v.Structure {
			newVs := vs.Copy()
			newVs.EnclosingVolume = &newV
			newV.Structure[i] = *newVs
		}
	}
	return &newV
}

const GPTPartitionGUIDESP = "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"

// VolumeStructure describes a single structure inside a volume. A structure can
// represent a partition, Master Boot Record, or any other contiguous range
// within the volume.
type VolumeStructure struct {
	// VolumeName is the name of the volume that this structure belongs to.
	VolumeName string `json:"-"`
	// Name, when non empty, provides the name of the structure
	Name string `yaml:"name" json:"name"`
	// Label provides the filesystem label
	Label string `yaml:"filesystem-label" json:"filesystem-label"`
	// Offset defines a starting offset of the structure
	Offset *quantity.Offset `yaml:"offset" json:"offset"`
	// OffsetWrite describes a 32-bit address, within the volume, at which
	// the offset of the current structure will be written. Initially, the
	// position could be specified as a byte offset relative to the start
	// of any named structure in the volume, but now the scope has been
	// limited and the only accepted structure would be one with offset
	// 0. Which implies that actually this offset will be always absolute,
	// which should be fine as the only known use case for this is to set
	// an address in an MBR. Furthermore, writes outside of the first
	// structure are now not allowed.
	OffsetWrite *RelativeOffset `yaml:"offset-write" json:"offset-write"`
	// Minimum size of the structure (optional)
	MinSize quantity.Size `yaml:"min-size" json:"min-size"`
	// Size of the structure
	Size quantity.Size `yaml:"size" json:"size"`
	// Type of the structure, which can be 2-hex digit MBR partition,
	// 36-char GUID partition, comma separated <mbr>,<guid> for hybrid
	// partitioning schemes, or 'bare' when the structure is not considered
	// a partition.
	//
	// For backwards compatibility type 'mbr' is also accepted, and the
	// structure is treated as if it is of role 'mbr'.
	Type string `yaml:"type" json:"type"`
	// Role describes the role of given structure, can be one of
	// 'mbr', 'system-data', 'system-boot', 'system-boot-image',
	// 'system-boot-select' or 'system-recovery-select'. Structures of type 'mbr', must have a
	// size of 446 bytes and must start at 0 offset.
	Role string `yaml:"role" json:"role"`
	// ID is the GPT partition ID, this should always be made upper case for
	// comparison purposes.
	ID string `yaml:"id" json:"id"`
	// Filesystem used for the partition, 'vfat', 'vfat-{16,32}', 'ext4' or 'none' for
	// structures of type 'bare'. 'vfat' is a synonymous for 'vfat-32'.
	Filesystem string `yaml:"filesystem" json:"filesystem"`
	// Content of the structure
	Content []VolumeContent `yaml:"content" json:"content"`
	Update  VolumeUpdate    `yaml:"update" json:"update"`

	// Note that the Device field will never be part of the yaml
	// and just used as part of the POST /systems/<label> API that
	// is used by an installer.
	Device string `yaml:"-" json:"device,omitempty"`

	// Index of the structure definition in gadget YAML, note this starts at 0.
	YamlIndex int `yaml:"-" json:"-"`
	// EnclosingVolume is a pointer to the enclosing Volume, and should be used
	// exclusively to check for partial information that affects the
	// structure properties.
	EnclosingVolume *Volume `yaml:"-" json:"-"`
}

// SetEnclosingVolumeInStructs is a helper that sets the pointer to
// the Volume in all VolumeStructure objects it contains.
func SetEnclosingVolumeInStructs(vv map[string]*Volume) {
	for _, v := range vv {
		for sidx := range v.Structure {
			v.Structure[sidx].EnclosingVolume = v
		}
	}
}

// IsRoleMBR tells us if v has MBR role or not.
func (v *VolumeStructure) IsRoleMBR() bool {
	return v.Role == schemaMBR
}

// HasFilesystem tells us if the structure definition expects a filesystem.
func (vs *VolumeStructure) HasFilesystem() bool {
	switch {
	case vs.Filesystem != "none" && vs.Filesystem != "":
		return true
	case vs.Type == "bare" || vs.Type == "mbr":
		return false
	default:
		return vs.EnclosingVolume.HasPartial(PartialFilesystem)
	}
}

// IsPartition returns true when the structure describes a partition in a block
// device.
func (vs *VolumeStructure) IsPartition() bool {
	return vs.Type != "bare" && vs.Role != schemaMBR
}

// LinuxFilesystem returns the linux filesystem that corresponds to the
// one specified in the gadget.
func (vs *VolumeStructure) LinuxFilesystem() string {
	switch vs.Filesystem {
	case "vfat-16", "vfat-32":
		return "vfat"
	default:
		return vs.Filesystem
	}
}

// HasLabel checks if label matches the VolumeStructure label. It ignores
// capitals if the structure has a fat filesystem.
func (vs *VolumeStructure) HasLabel(label string) bool {
	if vs.LinuxFilesystem() == "vfat" {
		return strings.EqualFold(vs.Label, label)
	}
	return vs.Label == label
}

// isFixedSize tells us if size is fixed or if there is range.
func (vs *VolumeStructure) isFixedSize() bool {
	if vs.hasPartialSize() {
		return false
	}

	return vs.Size == vs.MinSize
}

// hasPartialSize tells us if the structure has partially defined size.
func (vs *VolumeStructure) hasPartialSize() bool {
	if !vs.EnclosingVolume.HasPartial(PartialSize) {
		return false
	}

	return vs.Size == 0
}

// minStructureOffset works out the minimum start offset of an structure, which
// depends on previous volume structures.
func minStructureOffset(vss []VolumeStructure, idx int) quantity.Offset {
	if vss[idx].Offset != nil {
		return *vss[idx].Offset
	}
	// Move to lower indices in the slice for minimum: the minimum offset
	// will be the first fixed offset that we find plus all the minimum
	// sizes of the structures up to that point.
	min := quantity.Offset(0)
	othersSz := quantity.Size(0)
	for i := idx - 1; i >= 0; i-- {
		othersSz += vss[i].MinSize
		if vss[i].Offset != nil {
			min = *vss[i].Offset + quantity.Offset(othersSz)
			break
		}
	}
	return min
}

// maxStructureOffset works out the maximum start offset of an structure, which
// depends on surrounding volume structures.
func maxStructureOffset(vss []VolumeStructure, idx int) quantity.Offset {
	if vss[idx].Offset != nil {
		return *vss[idx].Offset
	}
	// There are two restrictions on the maximum:
	// 1. There is an implicit assumption that structures are contiguous if
	//    no offset is specified, so the max offset would be the first fixed
	//    offset while moving to previous structures in the slice plus the
	//    (max) size of each structure up to that point.
	// 2. There is also a restriction if we find a fixed offset in following
	//    structures in the slice - in that case the maximum offset needs to
	//    be smaller than that offset minus all the minimum sizes of
	//    structures up to that point.
	// The final max offset will be the smaller of the two.

	// Move backwards in the slice for the first restriction
	max := quantity.Offset(0)
	othersSz := quantity.Size(0)
	for i := idx - 1; i >= 0; i-- {
		if vss[i].hasPartialSize() {
			// If a previous partition has not a defined size, the
			// allowed offset is not really bounded.
			max = UnboundedStructureOffset
			break
		}
		othersSz += vss[i].Size
		if vss[i].Offset != nil {
			max = *vss[i].Offset + quantity.Offset(othersSz)
			break
		}
	}

	// Move forward in the slice for the second restriction
	maxFw := UnboundedStructureOffset
	downSz := quantity.Size(0)
	for i := idx; i < len(vss); i++ {
		if vss[i].Offset != nil {
			maxFw = *vss[i].Offset - quantity.Offset(downSz)
			break
		}
		downSz += vss[i].MinSize
	}

	if maxFw < max {
		max = maxFw
	}

	return max
}

type invalidOffsetError struct {
	offset     quantity.Offset
	lowerBound quantity.Offset
	upperBound quantity.Offset
}

func (e *invalidOffsetError) Error() string {
	maxDesc := "unbounded"
	if e.upperBound != UnboundedStructureOffset {
		maxDesc = fmt.Sprintf("%d (%s)", e.upperBound, e.upperBound.IECString())
	}
	return fmt.Sprintf("offset %d (%s) is not in the valid gadget interval (min: %d (%s): max: %s)",
		e.offset, e.offset.IECString(), e.lowerBound, e.lowerBound.IECString(), maxDesc)
}

// CheckValidStartOffset returns an error if the input offset is not valid for
// the structure at idx, nil otherwise.
func CheckValidStartOffset(off quantity.Offset, vss []VolumeStructure, idx int) error {
	min := minStructureOffset(vss, idx)
	max := maxStructureOffset(vss, idx)
	if min <= off && off <= max {
		return nil
	}
	return &invalidOffsetError{offset: off, lowerBound: min, upperBound: max}
}

// VolumeContent defines the contents of the structure. The content can be
// either files within a filesystem described by the structure or raw images
// written into the area of a bare structure.
type VolumeContent struct {
	// UnresovedSource is the data of the partition relative to
	// the gadget base directory
	UnresolvedSource string `yaml:"source" json:"source"`
	// Target is the location of the data inside the root filesystem
	Target string `yaml:"target" json:"target"`

	// Image names the image, relative to gadget base directory, to be used
	// for a 'bare' type structure
	Image string `yaml:"image" json:"image"`
	// Offset the image is written at
	Offset *quantity.Offset `yaml:"offset" json:"offset"`
	// Size of the image, when empty size is calculated by looking at the
	// image
	Size quantity.Size `yaml:"size" json:"size"`

	Unpack bool `yaml:"unpack" json:"unpack"`
}

func (vc VolumeContent) String() string {
	if vc.Image != "" {
		return fmt.Sprintf("image:%s", vc.Image)
	}
	return fmt.Sprintf("source:%s", vc.UnresolvedSource)
}

type VolumeUpdate struct {
	Edition  edition.Number `yaml:"edition" json:"edition"`
	Preserve []string       `yaml:"preserve" json:"preserve"`
}

// DiskVolumeDeviceTraits is a set of traits about a disk that were measured at
// a previous point in time on the same device, and is used primarily to try and
// map a volume in the gadget.yaml to a physical device on the system after the
// initial installation is done. We don't have a steadfast and predictable way
// to always find the device again, so we need to do a search, trying to find a
// device which matches each trait in turn, and verify it matches the  physical
// structure layout and if not move on to using the next trait.
type DiskVolumeDeviceTraits struct {
	// each member here is presented in descending order of certainty about the
	// likelihood of being compatible if a candidate physical device matches the
	// member. I.e. OriginalDevicePath is more trusted than OriginalKernelPath is
	// more trusted than DiskID is more trusted than using the MappedStructures

	// OriginalDevicePath is the device path in sysfs and in /dev/disk/by-path
	// the volume was measured and observed at during UC20+ install mode.
	OriginalDevicePath string `json:"device-path"`

	// OriginalKernelPath is the device path like /dev/vda the volume was
	// measured and observed at during UC20+ install mode.
	OriginalKernelPath string `json:"kernel-path"`

	// DiskID is the disk's identifier, it is a UUID for GPT disks or an
	// unsigned integer for DOS disks encoded as a string in hexadecimal as in
	// "0x1212e868".
	DiskID string `json:"disk-id"`

	// Size is the physical size of the disk, regardless of usable space
	// considerations.
	Size quantity.Size `json:"size"`

	// SectorSize is the physical sector size of the disk, typically 512 or
	// 4096.
	SectorSize quantity.Size `json:"sector-size"`

	// Schema is the disk schema, either dos or gpt in lowercase.
	Schema string `json:"schema"`

	// Structure contains trait information about each individual structure in
	// the volume that may be useful in identifying whether a disk matches a
	// volume or not.
	Structure []DiskStructureDeviceTraits `json:"structure"`

	// StructureEncryption is the set of partitions that are encrypted on the
	// volume - this should only ever have ubuntu-data or ubuntu-save keys for
	// now in the map. The value indicates parameters of the encryption present
	// that enable matching/identifying encrypted structures with their laid out
	// counterparts in the gadget.yaml.
	StructureEncryption map[string]StructureEncryptionParameters `json:"structure-encryption"`
}

// StructureEncryptionParameters contains information about an encrypted
// structure, used to match encrypted structures on disk with their abstract,
// laid out counterparts in the gadget.yaml.
type StructureEncryptionParameters struct {
	// Method is the method of encryption used, currently only EncryptionLUKS is
	// recognized.
	Method DiskEncryptionMethod `json:"method"`

	// unknownKeys is used to log messages about unknown, unrecognized keys that
	// we may encounter and may be used in the future
	unknownKeys map[string]string
}

func (s *StructureEncryptionParameters) UnmarshalJSON(b []byte) error {
	m := map[string]string{}

	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}

	for key, val := range m {
		if key == "method" {
			s.Method = DiskEncryptionMethod(val)
		} else {
			if s.unknownKeys == nil {
				s.unknownKeys = make(map[string]string)
			}
			s.unknownKeys[key] = val
		}
	}

	return nil
}

// DiskStructureDeviceTraits is a similar to DiskVolumeDeviceTraits, but is a
// set of traits for a specific structure on a disk rather than the full disk
// itself. Structures can be full partitions or just raw slices on a disk like
// the "BIOS Boot" structure on default amd64 grub Ubuntu Core systems.
type DiskStructureDeviceTraits struct {
	// OriginalDevicePath is the device path in sysfs and in /dev/disk/by-path the
	// partition was measured and observed at during UC20+ install mode.
	OriginalDevicePath string `json:"device-path"`
	// OriginalKernelPath is the device path like /dev/vda1 the partition was
	// measured and observed at during UC20+ install mode.
	OriginalKernelPath string `json:"kernel-path"`
	// PartitionUUID is the partuuid as defined by i.e. /dev/disk/by-partuuid
	PartitionUUID string `json:"partition-uuid"`
	// PartitionLabel is the label of the partition for GPT disks, i.e.
	// /dev/disk/by-partlabel
	PartitionLabel string `json:"partition-label"`
	// PartitionType is the type of the partition i.e. 0x83 for a
	// Linux native partition on DOS, or
	// 0FC63DAF-8483-4772-8E79-3D69D8477DE4 for a Linux filesystem
	// data partition on GPT.
	PartitionType string `json:"partition-type"`
	// FilesystemUUID is the UUID of the filesystem on the partition, i.e.
	// /dev/disk/by-uuid
	FilesystemUUID string `json:"filesystem-uuid"`
	// FilesystemLabel is the label of the filesystem for structures that have
	// filesystems, i.e. /dev/disk/by-label
	FilesystemLabel string `json:"filesystem-label"`
	// FilesystemType is the type of the filesystem, i.e. vfat or ext4, etc.
	FilesystemType string `json:"filesystem-type"`
	// Offset is the offset of the structure
	Offset quantity.Offset `json:"offset"`
	// Size is the size of the structure
	Size quantity.Size `json:"size"`
}

// SaveDiskVolumesDeviceTraits saves the mapping of volume names to volume /
// device traits to a file inside the provided directory on disk for
// later loading and verification.
func SaveDiskVolumesDeviceTraits(dir string, mapping map[string]DiskVolumeDeviceTraits) error {
	b, err := json.Marshal(mapping)
	if err != nil {
		return err
	}

	filename := filepath.Join(dir, "disk-mapping.json")

	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return err
	}
	return osutil.AtomicWriteFile(filename, b, 0644, 0)
}

// LoadDiskVolumesDeviceTraits loads the mapping of volumes to disk traits if
// there is any. If there is no file with the mapping available, nil is
// returned.
func LoadDiskVolumesDeviceTraits(dir string) (map[string]DiskVolumeDeviceTraits, error) {
	var mapping map[string]DiskVolumeDeviceTraits

	filename := filepath.Join(dir, "disk-mapping.json")
	if !osutil.FileExists(filename) {
		return nil, nil
	}

	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	if len(b) == 0 {
		// if the file is empty, it is safe to ignore it
		logger.Noticef("WARNING: ignoring zero sized device traits file\n")
		return nil, nil
	}

	if err := json.Unmarshal(b, &mapping); err != nil {
		return nil, err
	}

	return mapping, nil
}

// AllDiskVolumeDeviceTraits takes a mapping of volume name to Volume
// and produces a map of volume name to DiskVolumeDeviceTraits. Since
// doing so uses DiskVolumeDeviceTraitsForDevice, it will also
// validate that disk devices identified for the volume are compatible
// and matching before returning.
func AllDiskVolumeDeviceTraits(allVols map[string]*Volume, optsPerVolume map[string]*DiskVolumeValidationOptions) (map[string]DiskVolumeDeviceTraits, error) {
	// build up the mapping of volumes to disk device traits

	allTraits := map[string]DiskVolumeDeviceTraits{}

	// find all devices which map to volumes to save the current state of the
	// system
	for name, vol := range allVols {
		// try to find a device for a structure inside the volume, we have a
		// loop to attempt to use all structures in the volume in case there are
		// partitions we can't map to a device directly at first using the
		// device symlinks that FindDeviceForStructure uses
		dev := ""
		for _, vs := range vol.Structure {
			// TODO: This code works for volumes that have at least one
			// partition (i.e. not type: bare structure), but does not work for
			// volumes which contain only type: bare structures with no other
			// structures on them. It is entirely unclear how to identify such
			// a volume, since there is no information on the disk about where
			// such raw structures begin and end and thus no way to validate
			// that a given disk "has" such raw structures at particular
			// locations, aside from potentially reading and comparing the bytes
			// at the expected locations, but that is probably fragile and very
			// non-performant.

			if !vs.IsPartition() {
				// skip trying to find non-partitions on disk, it won't work
				continue
			}

			structureDevice, err := FindDeviceForStructure(&vs)
			if err != nil && err != ErrDeviceNotFound {
				return nil, err
			}
			if structureDevice != "" {
				// we found a device for this structure, get the parent disk
				// and save that as the device for this volume
				disk, err := disks.DiskFromPartitionDeviceNode(structureDevice)
				if err != nil {
					return nil, err
				}

				dev = disk.KernelDeviceNode()
				break
			}
		}

		if dev == "" {
			return nil, fmt.Errorf("cannot find disk for volume %s from gadget", name)
		}

		// now that we have a candidate device for this disk, build up the
		// traits for it, this will also validate concretely that the
		// device we picked and the volume are compatible
		opts := optsPerVolume[name]
		if opts == nil {
			opts = &DiskVolumeValidationOptions{}
		}
		traits, err := DiskTraitsFromDeviceAndValidate(vol, dev, opts)
		if err != nil {
			return nil, fmt.Errorf("cannot gather disk traits for device %s to use with volume %s: %v", dev, name, err)
		}

		allTraits[name] = traits
	}

	return allTraits, nil
}

// GadgetConnect describes an interface connection requested by the gadget
// between seeded snaps. The syntax is of a mapping like:
//
//	plug: (<plug-snap-id>|system):plug
//	[slot: (<slot-snap-id>|system):slot]
//
// "system" indicates a system plug or slot.
// Fully omitting the slot part indicates a system slot with the same name
// as the plug.
type Connection struct {
	Plug ConnectionPlug `yaml:"plug"`
	Slot ConnectionSlot `yaml:"slot"`
}

type ConnectionPlug struct {
	SnapID string
	Plug   string
}

func (gcplug *ConnectionPlug) Empty() bool {
	return gcplug.SnapID == "" && gcplug.Plug == ""
}

func (gcplug *ConnectionPlug) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	snapID, name, err := parseSnapIDColonName(s)
	if err != nil {
		return fmt.Errorf("in gadget connection plug: %v", err)
	}
	gcplug.SnapID = snapID
	gcplug.Plug = name
	return nil
}

type ConnectionSlot struct {
	SnapID string
	Slot   string
}

func (gcslot *ConnectionSlot) Empty() bool {
	return gcslot.SnapID == "" && gcslot.Slot == ""
}

func (gcslot *ConnectionSlot) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	snapID, name, err := parseSnapIDColonName(s)
	if err != nil {
		return fmt.Errorf("in gadget connection slot: %v", err)
	}
	gcslot.SnapID = snapID
	gcslot.Slot = name
	return nil
}

func parseSnapIDColonName(s string) (snapID, name string, err error) {
	parts := strings.Split(s, ":")
	if len(parts) == 2 {
		snapID = parts[0]
		name = parts[1]
	}
	if snapID == "" || name == "" {
		return "", "", fmt.Errorf(`expected "(<snap-id>|system):name" not %q`, s)
	}
	return snapID, name, nil
}

func systemOrSnapID(s string) bool {
	if s != "system" && naming.ValidateSnapID(s) != nil {
		return false
	}
	return true
}

// Model carries characteristics about the model that are relevant to gadget.
// Note *asserts.Model implements this, and that's the expected use case.
type Model interface {
	Classic() bool
	Grade() asserts.ModelGrade
}

func classicOrUndetermined(m Model) bool {
	return m == nil || m.Classic()
}

func hasGrade(m Model) bool {
	return m != nil && m.Grade() != asserts.ModelGradeUnset
}

func compatWithPibootOrIndeterminate(m Model) bool {
	return m == nil || m.Grade() != asserts.ModelGradeUnset
}

// Ancillary structs to sort volume structures. We split volumes in
// contiguousStructs slices, with each of these beginning with a structure with
// a known fixed offset, followed by structures for which the offset is unknown
// so we can know for sure that they appear after the first structure in
// contiguousStruct. The unknown offsets appear because of min-size use. The
// contiguousStructs are the things that we need to order, as all have a known
// starting offset, which is not true for all the volume structures.
type contiguousStructs struct {
	// vss contains contiguous structures with the first one
	// containing a valid Offset
	vss []VolumeStructure
}

type contiguousStructsSet []*contiguousStructs

func (scss contiguousStructsSet) Len() int {
	return len(scss)
}

func (scss contiguousStructsSet) Less(i, j int) bool {
	return *scss[i].vss[0].Offset < *scss[j].vss[0].Offset
}

func (scss contiguousStructsSet) Swap(i, j int) {
	scss[i], scss[j] = scss[j], scss[i]
}

func orderStructuresByOffset(vss []VolumeStructure) []VolumeStructure {
	if vss == nil {
		return nil
	}

	// Build contiguous structures
	scss := contiguousStructsSet{}
	var currentCont *contiguousStructs
	for _, s := range vss {
		// If offset is set we can start a new "block", otherwise the
		// structure goes right after the latest structure of the
		// current block. Note that currentCont will never be accessed
		// as nil as necessarily the first structure in gadget.yaml will
		// have offset explicitly or implicitly (the only way for a
		// structure to have nil offset is when the current structure
		// does not have explicit offset and the previous one either
		// does not have itself offset or has min-size set).
		if s.Offset != nil {
			currentCont = &contiguousStructs{}
			scss = append(scss, currentCont)
		}

		currentCont.vss = append(currentCont.vss, s)
	}

	sort.Sort(scss)

	// Build plain list of structures now
	ordVss := []VolumeStructure{}
	for _, cs := range scss {
		ordVss = append(ordVss, cs.vss...)
	}
	return ordVss
}

func validatePartial(v *Volume) error {
	var foundCache [len(validPartialProperties)]bool
	for _, p := range v.Partial {
		found := false
		for vpvIdx, valid := range validPartialProperties {
			if p == valid {
				if foundCache[vpvIdx] == true {
					return fmt.Errorf("partial value %q is repeated", p)
				}
				foundCache[vpvIdx] = true
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%q is not a valid partial value", p)
		}
	}

	return nil
}

// InfoFromGadgetYaml parses the provided gadget metadata.
// If model is nil only self-consistency checks are performed.
// If model is not nil implied values for filesystem labels will be set
// as well, based on whether the model is for classic, UC16/18 or UC20.
// UC gadget metadata is expected to have volumes definitions.
func InfoFromGadgetYaml(gadgetYaml []byte, model Model) (*Info, error) {
	var gi Info

	if err := yaml.Unmarshal(gadgetYaml, &gi); err != nil {
		return nil, fmt.Errorf("cannot parse gadget metadata: %v", err)
	}

	for k, v := range gi.Defaults {
		if !systemOrSnapID(k) {
			return nil, fmt.Errorf(`default stanza not keyed by "system" or snap-id: %s`, k)
		}
		dflt, err := metautil.NormalizeValue(v)
		if err != nil {
			return nil, fmt.Errorf("default value %q of %q: %v", v, k, err)
		}
		gi.Defaults[k] = dflt.(map[string]interface{})
	}

	for i, gconn := range gi.Connections {
		if gconn.Plug.Empty() {
			return nil, errors.New("gadget connection plug cannot be empty")
		}
		if gconn.Slot.Empty() {
			gi.Connections[i].Slot.SnapID = "system"
			gi.Connections[i].Slot.Slot = gconn.Plug.Plug
		}
	}

	if len(gi.Volumes) == 0 && classicOrUndetermined(model) {
		// volumes can be left out on classic
		// can still specify defaults though
		return &gi, nil
	}

	// basic validation
	var bootloadersFound int
	for name := range gi.Volumes {
		v := gi.Volumes[name]
		if v == nil {
			return nil, fmt.Errorf("volume %q stanza is empty", name)
		}

		if err := validatePartial(v); err != nil {
			return nil, err
		}

		// set the VolumeName for the volume
		v.Name = name

		// Set values that are implicit in gadget.yaml.
		if err := setImplicitForVolume(v, model); err != nil {
			return nil, fmt.Errorf("invalid volume %q: %v", name, err)
		}

		// Note that after this call we assume always ordered structures
		v.Structure = orderStructuresByOffset(v.Structure)

		if err := validateVolume(v); err != nil {
			return nil, fmt.Errorf("invalid volume %q: %v", name, err)
		}

		switch v.Bootloader {
		case "":
			// pass
		case "grub", "u-boot", "android-boot", "lk":
			bootloadersFound += 1
		case "piboot":
			if !compatWithPibootOrIndeterminate(model) {
				return nil, errors.New("piboot bootloader valid only for UC20 onwards")
			}
			bootloadersFound += 1
		default:
			return nil, errors.New("bootloader must be one of grub, u-boot, android-boot, piboot or lk")
		}
	}
	switch {
	case bootloadersFound == 0:
		return nil, errors.New("bootloader not declared in any volume")
	case bootloadersFound > 1:
		return nil, fmt.Errorf("too many (%d) bootloaders declared", bootloadersFound)
	}

	return &gi, nil
}

type volRuleset int

const (
	volRulesetUnknown volRuleset = iota
	volRuleset16
	volRuleset20
)

func whichVolRuleset(model Model) volRuleset {
	if model == nil {
		return volRulesetUnknown
	}
	if model.Grade() != asserts.ModelGradeUnset {
		return volRuleset20
	}
	return volRuleset16
}

func setKnownLabel(label, filesystem string, knownFsLabels, knownVfatFsLabels map[string]bool) (unique bool) {
	lowerLabel := strings.ToLower(label)
	if seen := knownVfatFsLabels[lowerLabel]; seen {
		return false
	}
	if filesystem == "vfat" {
		// labels with same name (ignoring capitals) as an already
		// existing fat label are not allowed
		for knownLabel := range knownFsLabels {
			if lowerLabel == strings.ToLower(knownLabel) {
				return false
			}
		}
		knownVfatFsLabels[lowerLabel] = true
	} else {
		if seen := knownFsLabels[label]; seen {
			return false
		}
		knownFsLabels[label] = true
	}

	return true
}

func asOffsetPtr(offs quantity.Offset) *quantity.Offset {
	return &offs
}

func setImplicitForVolume(vol *Volume, model Model) error {
	rs := whichVolRuleset(model)
	if vol.HasPartial(PartialSchema) {
		if vol.Schema != "" {
			return fmt.Errorf("partial schema is set but schema is still specified as %q", vol.Schema)
		}
	} else if vol.Schema == "" {
		// default for schema is gpt
		vol.Schema = schemaGPT
	}

	// for uniqueness of filesystem labels
	knownFsLabels := make(map[string]bool, len(vol.Structure))
	knownVfatFsLabels := make(map[string]bool, len(vol.Structure))
	for _, s := range vol.Structure {
		if s.Label != "" {
			if !setKnownLabel(s.Label, s.LinuxFilesystem(), knownFsLabels, knownVfatFsLabels) {
				return fmt.Errorf("filesystem label %q is not unique", s.Label)
			}
		}
	}

	previousEnd := asOffsetPtr(0)
	for i := range vol.Structure {
		// set the VolumeName for the structure from the volume itself
		vol.Structure[i].VolumeName = vol.Name
		// Store index as we will reorder later
		vol.Structure[i].YamlIndex = i
		// MinSize is Size if not explicitly set
		if vol.Structure[i].MinSize == 0 {
			vol.Structure[i].MinSize = vol.Structure[i].Size
		}
		// Set the pointer to the volume
		vol.Structure[i].EnclosingVolume = vol

		// set other implicit data for the structure
		if err := setImplicitForVolumeStructure(&vol.Structure[i], rs, knownFsLabels, knownVfatFsLabels); err != nil {
			return err
		}

		// Set offset if it was not set (must be after setImplicitForVolumeStructure
		// so roles are good). This is possible only if the previous structure had
		// a well-defined end.
		if vol.Structure[i].Offset == nil && previousEnd != nil {
			var start quantity.Offset
			if vol.Structure[i].Role != schemaMBR && *previousEnd < NonMBRStartOffset {
				start = NonMBRStartOffset
			} else {
				start = *previousEnd
			}
			vol.Structure[i].Offset = &start
		}
		// We know the end of the structure only if we could define an offset
		// and the size is fixed.
		if vol.Structure[i].Offset != nil && vol.Structure[i].isFixedSize() {
			previousEnd = asOffsetPtr(*vol.Structure[i].Offset +
				quantity.Offset(vol.Structure[i].Size))
		} else {
			previousEnd = nil
		}
	}

	return nil
}

func setImplicitForVolumeStructure(vs *VolumeStructure, rs volRuleset, knownFsLabels, knownVfatFsLabels map[string]bool) error {
	if vs.Role == "" && vs.Type == schemaMBR {
		vs.Role = schemaMBR
		return nil
	}
	if rs == volRuleset16 && vs.Role == "" && vs.Label == SystemBoot {
		// legacy behavior, for gadgets that only specify a filesystem-label, eg. pc
		vs.Role = SystemBoot
		return nil
	}
	if vs.Label == "" {
		var implicitLabel string
		switch {
		case rs == volRuleset16 && vs.Role == SystemData:
			implicitLabel = implicitSystemDataLabel
		case rs == volRuleset20 && vs.Role == SystemData:
			implicitLabel = ubuntuDataLabel
		case rs == volRuleset20 && vs.Role == SystemSeed:
			implicitLabel = ubuntuSeedLabel
		case rs == volRuleset20 && vs.Role == SystemSeedNull:
			implicitLabel = ubuntuSeedLabel
		case rs == volRuleset20 && vs.Role == SystemBoot:
			implicitLabel = ubuntuBootLabel
		case rs == volRuleset20 && vs.Role == SystemSave:
			implicitLabel = ubuntuSaveLabel
		}
		if implicitLabel != "" {
			if !setKnownLabel(implicitLabel, vs.LinuxFilesystem(), knownFsLabels, knownVfatFsLabels) {
				return fmt.Errorf("filesystem label %q is implied by %s role but was already set elsewhere", implicitLabel, vs.Role)
			}
			vs.Label = implicitLabel
		}
	}
	return nil
}

func readInfo(f func(string) ([]byte, error), gadgetYamlFn string, model Model) (*Info, error) {
	gmeta, err := f(gadgetYamlFn)
	if classicOrUndetermined(model) && os.IsNotExist(err) {
		// gadget.yaml is optional for classic gadgets
		return &Info{}, nil
	}
	if err != nil {
		return nil, err
	}

	return InfoFromGadgetYaml(gmeta, model)
}

// ReadInfo reads the gadget specific metadata from meta/gadget.yaml in the snap
// root directory.
// See ReadInfoAndValidate for a variant that does role-usage consistency
// validation like Validate.
func ReadInfo(gadgetSnapRootDir string, model Model) (*Info, error) {
	gadgetYamlFn := filepath.Join(gadgetSnapRootDir, "meta", "gadget.yaml")
	ginfo, err := readInfo(ioutil.ReadFile, gadgetYamlFn, model)
	if err != nil {
		return nil, err
	}
	return ginfo, nil
}

// ReadInfoAndValidate reads the gadget specific metadata from
// meta/gadget.yaml in the snap root directory.
// It also performs role-usage consistency validation as Validate does
// using the given constraints. See ReadInfo for a variant that does not.
// See also ValidateContent for further validating the content itself
// instead of the metadata.
func ReadInfoAndValidate(gadgetSnapRootDir string, model Model, validationConstraints *ValidationConstraints) (*Info, error) {
	ginfo, err := ReadInfo(gadgetSnapRootDir, model)
	if err != nil {
		return nil, err
	}
	if err := Validate(ginfo, model, validationConstraints); err != nil {
		return nil, err
	}
	return ginfo, err
}

// ReadInfoFromSnapFile reads the gadget specific metadata from
// meta/gadget.yaml in the given snap container.
// It also performs role-usage consistency validation as Validate does.
// See ReadInfoFromSnapFileNoValidate for a variant that does not.
func ReadInfoFromSnapFile(snapf snap.Container, model Model) (*Info, error) {
	ginfo, err := ReadInfoFromSnapFileNoValidate(snapf, model)
	if err != nil {
		return nil, err
	}
	if err := Validate(ginfo, model, nil); err != nil {
		return nil, err
	}
	return ginfo, nil
}

// ReadInfoFromSnapFileNoValidate reads the gadget specific metadata from
// meta/gadget.yaml in the given snap container.
// See ReadInfoFromSnapFile for a variant that does role-usage consistency
// validation like Validate as well.
func ReadInfoFromSnapFileNoValidate(snapf snap.Container, model Model) (*Info, error) {
	gadgetYamlFn := "meta/gadget.yaml"
	ginfo, err := readInfo(snapf.ReadFile, gadgetYamlFn, model)
	if err != nil {
		return nil, err
	}
	return ginfo, nil
}

func fmtIndexAndName(idx int, name string) string {
	if name != "" {
		return fmt.Sprintf("#%v (%q)", idx, name)
	}
	return fmt.Sprintf("#%v", idx)
}

func validateVolume(vol *Volume) error {
	if !validVolumeName.MatchString(vol.Name) {
		return errors.New("invalid name")
	}
	if !vol.HasPartial(PartialSchema) && vol.Schema != schemaGPT && vol.Schema != schemaMBR {
		return fmt.Errorf("invalid schema %q", vol.Schema)
	}

	// named structures, to check that names are not repeated
	knownStructures := make(map[string]bool, len(vol.Structure))

	// TODO: should we also validate that if there is a system-recovery-select
	// role there should also be at least 2 system-recovery-image roles and
	// same for system-boot-select and at least 2 system-boot-image roles?
	for idx, s := range vol.Structure {
		if err := validateVolumeStructure(&s, vol); err != nil {
			return fmt.Errorf("invalid structure %v: %v", fmtIndexAndName(idx, s.Name), err)
		}

		if vol.Schema == schemaGPT && s.Offset != nil {
			// If the block size is 512, the First Usable LBA must be greater than or equal to
			// 34 (allowing 1 block for the Protective MBR, 1 block for the Partition Table
			// Header, and 32 blocks for the GPT Partition Entry Array); if the logical block
			// size is 4096, the First Useable LBA must be greater than or equal to 6 (allowing
			// 1 block for the Protective MBR, 1 block for the GPT Header, and 4
			// blocks for the GPT Partition Entry Array)
			// Since we are not able to know the block size when building gadget snap, so we
			// are not able to easily know whether the structure defined in gadget.yaml will
			// overlap GPT header or GPT partition table, thus we only return error if the
			// structure overlap the interval [4096, 512*34), which is the intersection between
			// the GPT data for 512 bytes block size, which occupies [512, 512*34) and the GPT
			// for 4096 block size, which is [4096, 4096*6), and we print warning only if there
			// is some data in the union - intersection of the described GPT segments, which
			// might be fine but is suspicious.
			start := *s.Offset
			// MinSize instead of Size as we warn only if we are
			// sure that there will be a problem (that is, the
			// problem will happen even for the smallest structure
			// case).
			end := start + quantity.Offset(s.MinSize)
			if start < 512*34 && end > 4096 {
				logger.Noticef("WARNING: invalid structure: GPT header or GPT partition table overlapped with structure %q\n", s.Name)
			} else if start < 4096*6 && end > 512 {
				logger.Noticef("WARNING: GPT header or GPT partition table might be overlapped with structure %q", s.Name)
			}
		}

		if s.Name != "" {
			if _, ok := knownStructures[s.Name]; ok {
				return fmt.Errorf("structure name %q is not unique", s.Name)
			}
			// keep track of named structures
			knownStructures[s.Name] = true
		}
	}

	return validateCrossVolumeStructure(vol)
}

// isMBR returns whether the structure is the MBR and can be used before setImplicitForVolume
func isMBR(vs *VolumeStructure) bool {
	if vs.Role == schemaMBR {
		return true
	}
	if vs.Role == "" && vs.Type == schemaMBR {
		return true
	}
	return false
}

func validateCrossVolumeStructure(vol *Volume) error {
	previousEnd := quantity.Offset(0)
	// cross structure validation:
	// - relative offsets that reference other structures by name
	// - structure overlap
	for pidx, ps := range vol.Structure {
		if isMBR(&ps) {
			if ps.Offset == nil || *(ps.Offset) != 0 {
				return fmt.Errorf(`structure %q has "mbr" role and must start at offset 0`, ps.Name)
			}
		}
		if err := validateOffsetWrite(&ps, &vol.Structure[0], vol.MinSize()); err != nil {
			return err
		}
		// We are assuming ordered structures
		if ps.Offset != nil {
			if *(ps.Offset) < previousEnd {
				return fmt.Errorf("structure %q overlaps with the preceding structure %q", ps.Name, vol.Structure[pidx-1].Name)
			}
			previousEnd = *(ps.Offset) + quantity.Offset(ps.Size)
		} else {
			previousEnd += quantity.Offset(ps.Size)

		}
	}
	return nil
}

func validateOffsetWrite(s, firstStruct *VolumeStructure, volSize quantity.Size) error {
	if s.OffsetWrite == nil {
		return nil
	}

	if s.OffsetWrite.RelativeTo != "" {
		// offset-write using a named structure can only refer to
		// the first volume structure
		if s.OffsetWrite.RelativeTo != firstStruct.Name {
			return fmt.Errorf("structure %q refers to an unexpected structure %q",
				s.Name, s.OffsetWrite.RelativeTo)
		}
		if firstStruct.Offset == nil || *(firstStruct.Offset) != 0 {
			return fmt.Errorf("structure %q refers to structure %q, which does not have 0 offset",
				s.Name, s.OffsetWrite.RelativeTo)
		}
		if quantity.Size(s.OffsetWrite.Offset)+SizeLBA48Pointer > firstStruct.MinSize {
			return fmt.Errorf("structure %q wants to write offset of %d bytes to %d, outside of referred structure %q",
				s.Name, SizeLBA48Pointer, s.OffsetWrite.Offset, s.OffsetWrite.RelativeTo)
		}
	} else if quantity.Size(s.OffsetWrite.Offset)+SizeLBA48Pointer > volSize {
		return fmt.Errorf("structure %q wants to write offset of %d bytes to %d, outside of volume of min size %d",
			s.Name, SizeLBA48Pointer, s.OffsetWrite.Offset, volSize)
	}

	return nil
}

func validateVolumeStructure(vs *VolumeStructure, vol *Volume) error {
	if !vs.hasPartialSize() {
		if vs.Size == 0 {
			return errors.New("missing size")
		}
		if vs.MinSize > vs.Size {
			return fmt.Errorf("min-size (%d) is bigger than size (%d)",
				vs.MinSize, vs.Size)
		}
	}
	if err := validateStructureType(vs.Type, vol); err != nil {
		return fmt.Errorf("invalid type %q: %v", vs.Type, err)
	}
	if err := validateRole(vs); err != nil {
		var what string
		if vs.Role != "" {
			what = fmt.Sprintf("role %q", vs.Role)
		} else {
			what = fmt.Sprintf("implicit role %q", vs.Type)
		}
		return fmt.Errorf("invalid %s: %v", what, err)
	}
	if vs.Filesystem != "" && !strutil.ListContains([]string{"ext4", "vfat", "vfat-16", "vfat-32", "none"}, vs.Filesystem) {
		return fmt.Errorf("invalid filesystem %q", vs.Filesystem)
	}

	var contentChecker func(*VolumeContent) error

	if vs.HasFilesystem() {
		contentChecker = validateFilesystemContent
	} else {
		contentChecker = validateBareContent
	}
	for i, c := range vs.Content {
		if err := contentChecker(&c); err != nil {
			return fmt.Errorf("invalid content #%v: %v", i, err)
		}
	}

	if err := validateStructureUpdate(vs, vol); err != nil {
		return err
	}

	// TODO: validate structure size against sector-size; ubuntu-image uses
	// a tmp file to find out the default sector size of the device the tmp
	// file is created on
	return nil
}

func validateStructureType(s string, vol *Volume) error {
	// Type can be one of:
	// - "mbr" (backwards compatible)
	// - "bare"
	// - [0-9A-Z]{2} - MBR type
	// - GPT UUID
	// - hybrid ID
	//
	// Hybrid ID is 2 hex digits of MBR type, followed by 36 GUUID
	// example: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B

	if s == "" {
		return errors.New(`type is not specified`)
	}

	if s == "bare" {
		// unknonwn blob
		return nil
	}

	if s == schemaMBR {
		// backward compatibility for type: mbr
		return nil
	}

	var isGPT, isMBR, isHybrid bool

	idx := strings.IndexRune(s, ',')
	if idx == -1 {
		// just ID
		switch {
		case validTypeID.MatchString(s):
			isMBR = true
		case validGUUID.MatchString(s):
			isGPT = true
		default:
			return fmt.Errorf("invalid format")
		}
	} else {
		// hybrid ID
		isHybrid = true
		code := s[:idx]
		guid := s[idx+1:]
		if len(code) != 2 || len(guid) != 36 || !validTypeID.MatchString(code) || !validGUUID.MatchString(guid) {
			return fmt.Errorf("invalid format of hybrid type")
		}
	}

	if vol.HasPartial(PartialSchema) {
		if !isHybrid {
			return fmt.Errorf("both MBR type and GUID structure type needs to be defined on partial schemas")
		}
	} else {
		schema := vol.Schema
		if schema != schemaGPT && isGPT {
			// type: <uuid> is only valid for GPT volumes
			return fmt.Errorf("GUID structure type with non-GPT schema %q", vol.Schema)
		}
		if schema != schemaMBR && isMBR {
			return fmt.Errorf("MBR structure type with non-MBR schema %q", vol.Schema)
		}
	}

	return nil
}

func validateRole(vs *VolumeStructure) error {
	if vs.Type == "bare" {
		if vs.Role != "" && vs.Role != schemaMBR {
			return fmt.Errorf("conflicting type: %q", vs.Type)
		}
	}
	vsRole := vs.Role
	if vs.Type == schemaMBR {
		if vsRole != "" && vsRole != schemaMBR {
			return fmt.Errorf(`conflicting legacy type: "mbr"`)
		}
		// backward compatibility
		vsRole = schemaMBR
	}

	switch vsRole {
	case SystemData, SystemSeed, SystemSeedNull, SystemSave:
		// roles have cross dependencies, consistency checks are done at
		// the volume level
	case schemaMBR:
		if vs.Size > SizeMBR {
			return errors.New("mbr structures cannot be larger than 446 bytes")
		}
		if vs.Offset != nil && *vs.Offset != 0 {
			return errors.New("mbr structure must start at offset 0")
		}
		if vs.ID != "" {
			return errors.New("mbr structure must not specify partition ID")
		}
		if vs.Filesystem != "" && vs.Filesystem != "none" {
			return errors.New("mbr structures must not specify a file system")
		}
	case SystemBoot, bootImage, bootSelect, seedBootSelect, seedBootImage, "":
		// noop
	case legacyBootImage, legacyBootSelect:
		// noop
		// legacy role names were added in 2.42 can be removed
		// on snapd epoch bump
	default:
		return fmt.Errorf("unsupported role")
	}
	return nil
}

func validateBareContent(vc *VolumeContent) error {
	if vc.UnresolvedSource != "" || vc.Target != "" {
		return fmt.Errorf("cannot use non-image content for bare file system")
	}
	if vc.Image == "" {
		return fmt.Errorf("missing image file name")
	}
	return nil
}

func validateFilesystemContent(vc *VolumeContent) error {
	if vc.Image != "" || vc.Offset != nil || vc.Size != 0 {
		return fmt.Errorf("cannot use image content for non-bare file system")
	}
	if vc.UnresolvedSource == "" {
		return fmt.Errorf("missing source")
	}
	if vc.Target == "" {
		return fmt.Errorf("missing target")
	}
	return nil
}

func validateStructureUpdate(vs *VolumeStructure, v *Volume) error {
	if !vs.HasFilesystem() && len(vs.Update.Preserve) > 0 {
		return errors.New("preserving files during update is not supported for non-filesystem structures")
	}

	names := make(map[string]bool, len(vs.Update.Preserve))
	for _, n := range vs.Update.Preserve {
		if names[n] {
			return fmt.Errorf(`duplicate "preserve" entry %q`, n)
		}
		names[n] = true
	}
	return nil
}

const (
	// SizeMBR is the maximum byte size of a structure of role 'mbr'
	SizeMBR = quantity.Size(446)
	// SizeLBA48Pointer is the byte size of a pointer value written at the
	// location described by 'offset-write'
	SizeLBA48Pointer = quantity.Size(4)
)

// RelativeOffset describes an offset where structure data is written at.
// The position can be specified as byte-offset relative to the start of another
// named structure.
type RelativeOffset struct {
	// RelativeTo names the structure relative to which the location of the
	// address write will be calculated.
	RelativeTo string `yaml:"relative-to" json:"relative-to"`
	// Offset is a 32-bit value
	Offset quantity.Offset `yaml:"offset" json:"offset"`
}

func (r *RelativeOffset) String() string {
	if r == nil {
		return "unspecified"
	}
	if r.RelativeTo != "" {
		return fmt.Sprintf("%s+%d", r.RelativeTo, r.Offset)
	}
	return fmt.Sprintf("%d", r.Offset)
}

// parseRelativeOffset parses a string describing an offset that can be
// expressed relative to a named structure, with the format: [<name>+]<offset>.
func parseRelativeOffset(grs string) (*RelativeOffset, error) {
	toWhat := ""
	offsSpec := grs
	if idx := strings.IndexRune(grs, '+'); idx != -1 {
		toWhat, offsSpec = grs[:idx], grs[idx+1:]
		if toWhat == "" {
			return nil, errors.New("missing volume name")
		}
	}
	if offsSpec == "" {
		return nil, errors.New("missing offset")
	}

	offset, err := quantity.ParseOffset(offsSpec)
	if err != nil {
		return nil, fmt.Errorf("cannot parse offset %q: %v", offsSpec, err)
	}
	if offset > 4*1024*quantity.OffsetMiB {
		return nil, fmt.Errorf("offset above 4G limit")
	}

	return &RelativeOffset{
		RelativeTo: toWhat,
		Offset:     offset,
	}, nil
}

func (s *RelativeOffset) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var grs string
	if err := unmarshal(&grs); err != nil {
		return errors.New(`cannot unmarshal gadget relative offset`)
	}

	ro, err := parseRelativeOffset(grs)
	if err != nil {
		return fmt.Errorf("cannot parse relative offset %q: %v", grs, err)
	}
	*s = *ro
	return nil
}

// IsCompatible checks whether the current and an update are compatible. Returns
// nil or an error describing the incompatibility.
// TODO: make this reasonably consistent with Update for multi-volume scenarios
func IsCompatible(current, new *Info) error {
	// XXX: the only compatibility we have now is making sure that the new
	// layout can be used on an existing volume
	if len(new.Volumes) > 1 {
		return fmt.Errorf("gadgets with multiple volumes are unsupported")
	}

	// XXX: the code below errors out with more than 1 volume in the current
	// gadget, we allow this scenario in update but better bail out here and
	// have users fix their gadgets
	currentVol, newVol, err := resolveVolume(current, new)
	if err != nil {
		return err
	}

	if err := isLayoutCompatible(currentVol, newVol); err != nil {
		return fmt.Errorf("incompatible layout change: %v", err)
	}
	return nil
}

// checkCompatibleSchema checks if the schema in a new volume we are
// updating to is compatible with the old volume.
func checkCompatibleSchema(old, new *Volume) error {
	// If old schema is partial, any schema in new will be fine
	if !old.HasPartial(PartialSchema) {
		if new.HasPartial(PartialSchema) {
			return fmt.Errorf("new schema is partial, while old was not")
		}
		if old.Schema != new.Schema {
			return fmt.Errorf("incompatible schema change from %v to %v",
				old.Schema, new.Schema)
		}
	}
	return nil
}

// LaidOutVolumesFromGadget takes gadget volumes, gadget and kernel rootdirs
// and lays out the partitions on all volumes as specified. It returns the
// volumes mentioned in the gadget.yaml and their laid out representations.
func LaidOutVolumesFromGadget(vols map[string]*Volume, gadgetRoot, kernelRoot string, encType secboot.EncryptionType, volToGadgetToDiskStruct map[string]map[int]*OnDiskStructure) (all map[string]*LaidOutVolume, err error) {

	all = make(map[string]*LaidOutVolume)
	// layout all volumes saving them
	opts := &LayoutOptions{
		GadgetRootDir: gadgetRoot,
		KernelRootDir: kernelRoot,
		EncType:       encType,
	}

	for name, vol := range vols {
		gadgetToDiskStruct, ok := volToGadgetToDiskStruct[name]
		if !ok {
			return nil, fmt.Errorf("internal error: volume %q does not have a map of gadget to disk partitions", name)
		}
		lvol, err := LayoutVolume(vol, gadgetToDiskStruct, opts)
		if err != nil {
			return nil, err
		}
		all[name] = lvol
	}

	return all, nil
}

// FindBootVolume returns the volume that contains the system-boot partition.
func FindBootVolume(vols map[string]*Volume) (*Volume, error) {
	for _, vol := range vols {
		for _, structure := range vol.Structure {
			if structure.Role == SystemBoot {
				return vol, nil
			}
		}
	}
	return nil, fmt.Errorf("no volume has system-boot role")
}

func flatten(path string, cfg interface{}, out map[string]interface{}) {
	if cfgMap, ok := cfg.(map[string]interface{}); ok {
		for k, v := range cfgMap {
			p := k
			if path != "" {
				p = path + "." + k
			}
			flatten(p, v, out)
		}
	} else {
		out[path] = cfg
	}
}

// SystemDefaults returns default system configuration from gadget defaults.
func SystemDefaults(gadgetDefaults map[string]map[string]interface{}) map[string]interface{} {
	for _, systemSnap := range []string{"system", naming.WellKnownSnapID("core")} {
		if defaults, ok := gadgetDefaults[systemSnap]; ok {
			coreDefaults := map[string]interface{}{}
			flatten("", defaults, coreDefaults)
			return coreDefaults
		}
	}
	return nil
}

// See https://www.kernel.org/doc/html/latest/admin-guide/kernel-parameters.html
var disallowedKernelArguments = []string{
	"root", "nfsroot",
	"init",
}

var allowedSnapdKernelArguments = []string{
	"snapd_system_disk",
	"snapd.debug",
}

// isKernelArgumentAllowed checks whether the kernel command line argument is
// allowed. Prohibits all arguments listed explicitly in
// disallowedKernelArguments list and those prefixed with snapd, with exception
// of snapd.debug. All other arguments are allowed.
func isKernelArgumentAllowed(arg string) bool {
	if strutil.ListContains(allowedSnapdKernelArguments, arg) {
		return true
	}
	if strings.HasPrefix(arg, "snapd") {
		return false
	}
	if strutil.ListContains(disallowedKernelArguments, arg) {
		return false
	}
	return true
}

// KernelCommandLineFromGadget returns the desired kernel command line provided by the
// gadget. The full flag indicates whether the gadget provides a full command
// line or just the extra parameters that will be appended to the static ones.
// A model is neededed to know how to interpret the gadget yaml from the gadget.
func KernelCommandLineFromGadget(gadgetDirOrSnapPath string, model Model) (cmdline string, full bool, removeArgs []kcmdline.ArgumentPattern, err error) {
	sf, err := snapfile.Open(gadgetDirOrSnapPath)
	if err != nil {
		return "", false, []kcmdline.ArgumentPattern{}, fmt.Errorf("cannot open gadget snap: %v", err)
	}

	info, err := ReadInfoFromSnapFileNoValidate(sf, model)
	if err != nil {
		return "", false, []kcmdline.ArgumentPattern{}, fmt.Errorf("Cannot read snap info: %v", err)
	}

	if len(info.KernelCmdline.Append) > 0 || len(info.KernelCmdline.Remove) > 0 {
		var asStr []string
		for _, cmd := range info.KernelCmdline.Append {
			value := cmd.String()
			split := strings.SplitN(value, "=", 2)
			if !isKernelArgumentAllowed(split[0]) {
				return "", false, []kcmdline.ArgumentPattern{}, fmt.Errorf("kernel parameter '%s' is not allowed", value)
			}
			asStr = append(asStr, value)
		}

		return strutil.JoinNonEmpty(asStr, " "), false, info.KernelCmdline.Remove, nil
	}

	// Backward compatibility
	contentExtra, err := sf.ReadFile("cmdline.extra")
	if err != nil && !os.IsNotExist(err) {
		return "", false, []kcmdline.ArgumentPattern{}, err
	}
	// TODO: should we enforce the maximum kernel command line for cmdline.full?
	contentFull, err := sf.ReadFile("cmdline.full")
	if err != nil && !os.IsNotExist(err) {
		return "", false, []kcmdline.ArgumentPattern{}, err
	}
	content := contentExtra
	whichFile := "cmdline.extra"
	switch {
	case contentExtra != nil && contentFull != nil:
		return "", false, []kcmdline.ArgumentPattern{}, fmt.Errorf("cannot support both extra and full kernel command lines")
	case contentExtra == nil && contentFull == nil:
		return "", false, []kcmdline.ArgumentPattern{}, nil
	case contentFull != nil:
		content = contentFull
		whichFile = "cmdline.full"
		full = true
	}
	parsed, err := parseCommandLineFromGadget(content)
	if err != nil {
		return "", full, []kcmdline.ArgumentPattern{}, fmt.Errorf("invalid kernel command line in %v: %v", whichFile, err)
	}
	return parsed, full, []kcmdline.ArgumentPattern{}, nil
}

// parseCommandLineFromGadget parses the command line file and returns a
// reassembled kernel command line as a single string. The file can be multi
// line, where only lines stating with # are treated as comments, eg.
//
// foo
// # this is a comment
//
// According to https://elixir.bootlin.com/linux/latest/source/Documentation/admin-guide/kernel-parameters.txt
// the # character can appear as part of a valid kernel command line argument,
// specifically in the following argument:
//
//	memmap=nn[KMG]#ss[KMG]
//	memmap=100M@2G,100M#3G,1G!1024G
//
// Thus a lone # or a token starting with # are treated as errors.
func parseCommandLineFromGadget(content []byte) (string, error) {
	s := bufio.NewScanner(bytes.NewBuffer(content))
	filtered := &bytes.Buffer{}
	for s.Scan() {
		line := s.Text()
		if len(line) > 0 && line[0] == '#' {
			// comment
			continue
		}
		filtered.WriteRune(' ')
		filtered.WriteString(line)
	}
	if err := s.Err(); err != nil {
		return "", err
	}
	kargs, err := kcmdline.Split(filtered.String())
	if err != nil {
		return "", err
	}
	for _, argValue := range kargs {
		if strings.HasPrefix(argValue, "#") {
			return "", fmt.Errorf("unexpected or invalid use of # in argument %q", argValue)
		}
		split := strings.SplitN(argValue, "=", 2)
		if !isKernelArgumentAllowed(split[0]) {
			return "", fmt.Errorf("disallowed kernel argument %q", argValue)
		}
	}
	return strings.Join(kargs, " "), nil
}

// HasRole reads the gadget specific metadata from meta/gadget.yaml in the snap
// root directory with minimal validation and checks whether any volume
// structure has one of the given roles returning it, otherwhise it returns the
// empty string.
// This is mainly intended to avoid compatibility issues from snap-bootstrap
// but could be used on any known to be properly installed gadget.
func HasRole(gadgetSnapRootDir string, roles []string) (foundRole string, err error) {
	gadgetYamlFn := filepath.Join(gadgetSnapRootDir, "meta", "gadget.yaml")
	gadgetYaml, err := ioutil.ReadFile(gadgetYamlFn)
	if err != nil {
		return "", err
	}
	var minInfo struct {
		Volumes map[string]struct {
			Structure []struct {
				Role string `yaml:"role"`
			} `yaml:"structure"`
		} `yaml:"volumes"`
	}
	if err := yaml.Unmarshal(gadgetYaml, &minInfo); err != nil {
		return "", fmt.Errorf("cannot minimally parse gadget metadata: %v", err)
	}
	for _, vol := range minInfo.Volumes {
		for _, s := range vol.Structure {
			if strutil.ListContains(roles, s.Role) {
				return s.Role, nil
			}
		}
	}
	return "", nil
}
