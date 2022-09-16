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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/gadget/edition"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/metautil"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
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

	SystemBoot = "system-boot"
	SystemData = "system-data"
	SystemSeed = "system-seed"
	SystemSave = "system-save"

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
)

var (
	validVolumeName = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9-]+$")
	validTypeID     = regexp.MustCompile("^[0-9A-F]{2}$")
	validGUUID      = regexp.MustCompile("^(?i)[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}$")
)

type Info struct {
	Volumes map[string]*Volume `yaml:"volumes,omitempty"`

	// Default configuration for snaps (snap-id => key => value).
	Defaults map[string]map[string]interface{} `yaml:"defaults,omitempty"`

	Connections []Connection `yaml:"connections"`
}

// Volume defines the structure and content for the image to be written into a
// block device.
type Volume struct {
	// Schema describes the schema used for the volume
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
	// the offset of current structure will be written. The position may be
	// specified as a byte offset relative to the start of a named structure
	OffsetWrite *RelativeOffset `yaml:"offset-write" json:"offset-write"`
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
	// Filesystem used for the partition, 'vfat', 'ext4' or 'none' for
	// structures of type 'bare'
	Filesystem string `yaml:"filesystem" json:"filesystem"`
	// Content of the structure
	Content []VolumeContent `yaml:"content" json:"content"`
	Update  VolumeUpdate    `yaml:"update" json:"update"`

	// Note that the Device and UnencryptedDevice fields will never
	// be part of the yaml and just used as part of the POST
	// /systems/<label> API that is used by an installer.
	Device            string `yaml:"-" json:"device,omitempty"`
	UnencryptedDevice string `yaml:"-" json:"unencrypted-device,omitempty"`
}

// HasFilesystem returns true if the structure is using a filesystem.
func (vs *VolumeStructure) HasFilesystem() bool {
	return vs.Filesystem != "none" && vs.Filesystem != ""
}

// IsPartition returns true when the structure describes a partition in a block
// device.
func (vs *VolumeStructure) IsPartition() bool {
	return vs.Type != "bare" && vs.Role != schemaMBR
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
	// OffsetWrite describes a 32-bit address, within the volume, at which
	// the offset of current image will be written. The position may be
	// specified as a byte offset relative to the start of a named structure
	OffsetWrite *RelativeOffset `yaml:"offset-write" json:"offset-write"`
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

	if err := json.Unmarshal(b, &mapping); err != nil {
		return nil, err
	}

	return mapping, nil
}

// AllDiskVolumeDeviceTraits takes a mapping of volume name to LaidOutVolume and
// produces a map of volume name to DiskVolumeDeviceTraits. Since doing so uses
// DiskVolumeDeviceTraitsForDevice, it will also validate that disk devices
// identified for the laid out volume are compatible and matching before
// returning.
func AllDiskVolumeDeviceTraits(allLaidOutVols map[string]*LaidOutVolume, optsPerVolume map[string]*DiskVolumeValidationOptions) (map[string]DiskVolumeDeviceTraits, error) {
	// build up the mapping of volumes to disk device traits

	allVols := map[string]DiskVolumeDeviceTraits{}

	// find all devices which map to volumes to save the current state of the
	// system
	for name, vol := range allLaidOutVols {
		// try to find a device for a structure inside the volume, we have a
		// loop to attempt to use all structures in the volume in case there are
		// partitions we can't map to a device directly at first using the
		// device symlinks that FindDeviceForStructure uses
		dev := ""
		for _, vs := range vol.LaidOutStructure {
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

		allVols[name] = traits
	}

	return allVols, nil
}

// GadgetConnect describes an interface connection requested by the gadget
// between seeded snaps. The syntax is of a mapping like:
//
//  plug: (<plug-snap-id>|system):plug
//  [slot: (<slot-snap-id>|system):slot]
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
	knownFsLabelsPerVolume := make(map[string]map[string]bool, len(gi.Volumes))
	for name := range gi.Volumes {
		v := gi.Volumes[name]
		if v == nil {
			return nil, fmt.Errorf("volume %q stanza is empty", name)
		}
		// set the VolumeName for the volume
		v.Name = name
		if err := validateVolume(v, knownFsLabelsPerVolume); err != nil {
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

	for name, v := range gi.Volumes {
		if err := setImplicitForVolume(v, model, knownFsLabelsPerVolume[name]); err != nil {
			return nil, fmt.Errorf("invalid volume %q: %v", name, err)
		}
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

func setImplicitForVolume(vol *Volume, model Model, knownFsLabels map[string]bool) error {
	rs := whichVolRuleset(model)
	if vol.Schema == "" {
		// default for schema is gpt
		vol.Schema = schemaGPT
	}
	for i := range vol.Structure {
		// set the VolumeName for the structure from the volume itself
		vol.Structure[i].VolumeName = vol.Name
		if err := setImplicitForVolumeStructure(&vol.Structure[i], rs, knownFsLabels); err != nil {
			return err
		}
	}
	return nil
}

func setImplicitForVolumeStructure(vs *VolumeStructure, rs volRuleset, knownFsLabels map[string]bool) error {
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
		case rs == volRuleset20 && vs.Role == SystemBoot:
			implicitLabel = ubuntuBootLabel
		case rs == volRuleset20 && vs.Role == SystemSave:
			implicitLabel = ubuntuSaveLabel
		}
		if implicitLabel != "" {
			if knownFsLabels[implicitLabel] {
				return fmt.Errorf("filesystem label %q is implied by %s role but was already set elsewhere", implicitLabel, vs.Role)
			}
			knownFsLabels[implicitLabel] = true
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

func validateVolume(vol *Volume, knownFsLabelsPerVolume map[string]map[string]bool) error {
	if !validVolumeName.MatchString(vol.Name) {
		return errors.New("invalid name")
	}
	if vol.Schema != "" && vol.Schema != schemaGPT && vol.Schema != schemaMBR {
		return fmt.Errorf("invalid schema %q", vol.Schema)
	}

	// named structures, for cross-referencing relative offset-write names
	knownStructures := make(map[string]*LaidOutStructure, len(vol.Structure))
	// for uniqueness of filesystem labels
	knownFsLabels := make(map[string]bool, len(vol.Structure))
	// for validating structure overlap
	structures := make([]LaidOutStructure, len(vol.Structure))

	if knownFsLabelsPerVolume != nil {
		knownFsLabelsPerVolume[vol.Name] = knownFsLabels
	}

	previousEnd := quantity.Offset(0)
	// TODO: should we also validate that if there is a system-recovery-select
	// role there should also be at least 2 system-recovery-image roles and
	// same for system-boot-select and at least 2 system-boot-image roles?
	for idx, s := range vol.Structure {
		if err := validateVolumeStructure(&s, vol); err != nil {
			return fmt.Errorf("invalid structure %v: %v", fmtIndexAndName(idx, s.Name), err)
		}
		var start quantity.Offset
		if s.Offset != nil {
			start = *s.Offset
		} else {
			start = previousEnd
		}
		end := start + quantity.Offset(s.Size)
		ps := LaidOutStructure{
			VolumeStructure: &vol.Structure[idx],
			StartOffset:     start,
			YamlIndex:       idx,
		}
		structures[idx] = ps
		if s.Name != "" {
			if _, ok := knownStructures[s.Name]; ok {
				return fmt.Errorf("structure name %q is not unique", s.Name)
			}
			// keep track of named structures
			knownStructures[s.Name] = &ps
		}
		if s.Label != "" {
			if seen := knownFsLabels[s.Label]; seen {
				return fmt.Errorf("filesystem label %q is not unique", s.Label)
			}
			knownFsLabels[s.Label] = true
		}

		previousEnd = end
	}

	// sort by starting offset
	sort.Sort(byStartOffset(structures))

	return validateCrossVolumeStructure(structures, knownStructures)
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

func validateCrossVolumeStructure(structures []LaidOutStructure, knownStructures map[string]*LaidOutStructure) error {
	previousEnd := quantity.Offset(0)
	// cross structure validation:
	// - relative offsets that reference other structures by name
	// - laid out structure overlap
	// use structures laid out within the volume
	for pidx, ps := range structures {
		if isMBR(ps.VolumeStructure) {
			if ps.StartOffset != 0 {
				return fmt.Errorf(`structure %v has "mbr" role and must start at offset 0`, ps)
			}
		}
		if ps.OffsetWrite != nil && ps.OffsetWrite.RelativeTo != "" {
			// offset-write using a named structure
			other := knownStructures[ps.OffsetWrite.RelativeTo]
			if other == nil {
				return fmt.Errorf("structure %v refers to an unknown structure %q",
					ps, ps.OffsetWrite.RelativeTo)
			}
		}

		if ps.StartOffset < previousEnd {
			previous := structures[pidx-1]
			return fmt.Errorf("structure %v overlaps with the preceding structure %v", ps, previous)
		}
		previousEnd = ps.StartOffset + quantity.Offset(ps.Size)

		if ps.HasFilesystem() {
			// content relative offset only possible if it's a bare structure
			continue
		}
		for cidx, c := range ps.Content {
			if c.OffsetWrite == nil || c.OffsetWrite.RelativeTo == "" {
				continue
			}
			relativeToStructure := knownStructures[c.OffsetWrite.RelativeTo]
			if relativeToStructure == nil {
				return fmt.Errorf("structure %v, content %v refers to an unknown structure %q",
					ps, fmtIndexAndName(cidx, c.Image), c.OffsetWrite.RelativeTo)
			}
		}
	}
	return nil
}

func validateVolumeStructure(vs *VolumeStructure, vol *Volume) error {
	if vs.Size == 0 {
		return errors.New("missing size")
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
	if vs.Filesystem != "" && !strutil.ListContains([]string{"ext4", "vfat", "none"}, vs.Filesystem) {
		return fmt.Errorf("invalid filesystem %q", vs.Filesystem)
	}

	var contentChecker func(*VolumeContent) error

	if !vs.HasFilesystem() {
		contentChecker = validateBareContent
	} else {
		contentChecker = validateFilesystemContent
	}
	for i, c := range vs.Content {
		if err := contentChecker(&c); err != nil {
			return fmt.Errorf("invalid content #%v: %v", i, err)
		}
	}

	if err := validateStructureUpdate(vs); err != nil {
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

	schema := vol.Schema
	if schema == "" {
		schema = schemaGPT
	}

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

	var isGPT, isMBR bool

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
		code := s[:idx]
		guid := s[idx+1:]
		if len(code) != 2 || len(guid) != 36 || !validTypeID.MatchString(code) || !validGUUID.MatchString(guid) {
			return fmt.Errorf("invalid format of hybrid type")
		}
	}

	if schema != schemaGPT && isGPT {
		// type: <uuid> is only valid for GPT volumes
		return fmt.Errorf("GUID structure type with non-GPT schema %q", vol.Schema)
	}
	if schema != schemaMBR && isMBR {
		return fmt.Errorf("MBR structure type with non-MBR schema %q", vol.Schema)
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
	case SystemData, SystemSeed, SystemSave:
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
	if vc.Image != "" || vc.Offset != nil || vc.OffsetWrite != nil || vc.Size != 0 {
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

func validateStructureUpdate(vs *VolumeStructure) error {
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

	if currentVol.Schema == "" || newVol.Schema == "" {
		return fmt.Errorf("internal error: unset volume schemas: old: %q new: %q", currentVol.Schema, newVol.Schema)
	}

	// layout both volumes partially, without going deep into the layout of
	// structure content, we only want to make sure that structures are
	// comapatible
	pCurrent, err := LayoutVolumePartially(currentVol, DefaultConstraints)
	if err != nil {
		return fmt.Errorf("cannot lay out the current volume: %v", err)
	}
	pNew, err := LayoutVolumePartially(newVol, DefaultConstraints)
	if err != nil {
		return fmt.Errorf("cannot lay out the new volume: %v", err)
	}
	if err := isLayoutCompatible(pCurrent, pNew); err != nil {
		return fmt.Errorf("incompatible layout change: %v", err)
	}
	return nil
}

// LaidOutVolumesFromGadget takes a gadget rootdir and lays out the partitions
// on all volumes as specified. It returns the specific volume on which system-*
// roles/partitions exist, as well as all volumes mentioned in the gadget.yaml
// and their laid out representations. Those volumes are assumed to already be
// flashed and managed separately at image build/flash time, while the system
// volume with all the system-* roles on it can be manipulated during install
// mode.
func LaidOutVolumesFromGadget(gadgetRoot, kernelRoot string, model Model) (system *LaidOutVolume, all map[string]*LaidOutVolume, err error) {
	all = make(map[string]*LaidOutVolume)
	// model should never be nil here
	if model == nil {
		return nil, nil, fmt.Errorf("internal error: must have model to lay out system volumes from a gadget")
	}
	// rely on the basic validation from ReadInfo to ensure that the system-*
	// roles are all on the same volume for example
	info, err := ReadInfoAndValidate(gadgetRoot, model, nil)
	if err != nil {
		return nil, nil, err
	}

	constraints := LayoutConstraints{
		NonMBRStartOffset: 1 * quantity.OffsetMiB,
	}

	// find the volume with the system-boot role on it, we already validated
	// that the system-* roles are all on the same volume
	for name, vol := range info.Volumes {
		// layout all volumes saving them
		lvol, err := LayoutVolume(gadgetRoot, kernelRoot, vol, constraints)
		if err != nil {
			return nil, nil, err
		}
		all[name] = lvol
		// check if this volume is the boot volume using the system-boot volume
		// to identify it
		for _, structure := range vol.Structure {
			if structure.Role == SystemBoot {
				if system != nil {
					// this should be impossible, the validation above should
					// ensure there are not multiple volumes with the same role
					// on them
					return nil, nil, fmt.Errorf("internal error: gadget passed validation but duplicated system-* roles across multiple volumes")
				}
				system = lvol
			}
		}
	}

	if system == nil {
		// this should be impossible, the validation above should ensure this
		return nil, nil, fmt.Errorf("internal error: gadget passed validation but does not have system-* roles on any volume")
	}

	return system, all, nil
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

// isKernelArgumentAllowed checks whether the kernel command line argument is
// allowed. Prohibits all arguments listed explicitly in
// disallowedKernelArguments list and those prefixed with snapd, with exception
// of snapd.debug. All other arguments are allowed.
func isKernelArgumentAllowed(arg string) bool {
	if strings.HasPrefix(arg, "snapd") && arg != "snapd.debug" {
		return false
	}
	if strutil.ListContains(disallowedKernelArguments, arg) {
		return false
	}
	return true
}

var ErrNoKernelCommandline = errors.New("no kernel command line in the gadget")

// KernelCommandLineFromGadget returns the desired kernel command line provided by the
// gadget. The full flag indicates whether the gadget provides a full command
// line or just the extra parameters that will be appended to the static ones.
// An ErrNoKernelCommandline is returned when thea gadget does not set any
// kernel command line.
func KernelCommandLineFromGadget(gadgetDirOrSnapPath string) (cmdline string, full bool, err error) {
	sf, err := snapfile.Open(gadgetDirOrSnapPath)
	if err != nil {
		return "", false, fmt.Errorf("cannot open gadget snap: %v", err)
	}
	contentExtra, err := sf.ReadFile("cmdline.extra")
	if err != nil && !os.IsNotExist(err) {
		return "", false, err
	}
	// TODO: should we enforce the maximum kernel command line for cmdline.full?
	contentFull, err := sf.ReadFile("cmdline.full")
	if err != nil && !os.IsNotExist(err) {
		return "", false, err
	}
	content := contentExtra
	whichFile := "cmdline.extra"
	switch {
	case contentExtra != nil && contentFull != nil:
		return "", false, fmt.Errorf("cannot support both extra and full kernel command lines")
	case contentExtra == nil && contentFull == nil:
		return "", false, ErrNoKernelCommandline
	case contentFull != nil:
		content = contentFull
		whichFile = "cmdline.full"
		full = true
	}
	parsed, err := parseCommandLineFromGadget(content)
	if err != nil {
		return "", full, fmt.Errorf("invalid kernel command line in %v: %v", whichFile, err)
	}
	return parsed, full, nil
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
//   memmap=nn[KMG]#ss[KMG]
//   memmap=100M@2G,100M#3G,1G!1024G
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
	kargs, err := osutil.KernelCommandLineSplit(filtered.String())
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
