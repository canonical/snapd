// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/metautil"
	"github.com/snapcore/snapd/strutil"
)

// The fixed length of valid snap IDs.
const validSnapIDLength = 32

const (
	// MBR identifies a Master Boot Record partitioning schema, or an MBR like role
	MBR = "mbr"
	// GPT identifies a GUID Partition Table partitioning schema
	GPT = "gpt"
	// Hybrid identifies a hybrid MBR/GPT schema
	Hybrid = "mbr,gpt"

	SystemBoot = "system-boot"
	SystemData = "system-data"
	SystemSeed = "system-seed"

	BootImage  = "system-boot-image"
	BootSelect = "system-boot-select"

	// ImplicitSystemDataLabel is the implicit filesystem label of structure
	// of system-data role
	ImplicitSystemDataLabel = "writable"

	// only supported for legacy reasons
	LegacyBootImage  = "bootimg"
	LegacyBootSelect = "bootselect"
)

var (
	validVolumeName = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9-]+$")
	validTypeID     = regexp.MustCompile("^[0-9A-F]{2}$")
	validGUUID      = regexp.MustCompile("^(?i)[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}$")
)

type Info struct {
	Volumes map[string]Volume `yaml:"volumes,omitempty"`

	// Default configuration for snaps (snap-id => key => value).
	Defaults map[string]map[string]interface{} `yaml:"defaults,omitempty"`

	Connections []Connection `yaml:"connections"`
}

// ModelConstraints defines rules to be followed when reading the gadget metadata.
type ModelConstraints struct {
	// Classic rules (i.e. content/presence of gadget.yaml is fully optional)
	Classic bool
	// A system-seed partition (aka recovery partion) is expected (Core 20)
	SystemSeed bool
}

// Volume defines the structure and content for the image to be written into a
// block device.
type Volume struct {
	// Schema describes the schema used for the volume
	Schema string `yaml:"schema"`
	// Bootloader names the bootloader used by the volume
	Bootloader string `yaml:"bootloader"`
	//  ID is a 2-hex digit disk ID or GPT GUID
	ID string `yaml:"id"`
	// Structure describes the structures that are part of the volume
	Structure []VolumeStructure `yaml:"structure"`
}

func (v *Volume) EffectiveSchema() string {
	if v.Schema == "" {
		return GPT
	}
	return v.Schema
}

// VolumeStructure describes a single structure inside a volume. A structure can
// represent a partition, Master Boot Record, or any other contiguous range
// within the volume.
type VolumeStructure struct {
	// Name, when non empty, provides the name of the structure
	Name string `yaml:"name"`
	// Label provides the filesystem label
	Label string `yaml:"filesystem-label"`
	// Offset defines a starting offset of the structure
	Offset *Size `yaml:"offset"`
	// OffsetWrite describes a 32-bit address, within the volume, at which
	// the offset of current structure will be written. The position may be
	// specified as a byte offset relative to the start of a named structure
	OffsetWrite *RelativeOffset `yaml:"offset-write"`
	// Size of the structure
	Size Size `yaml:"size"`
	// Type of the structure, which can be 2-hex digit MBR partition,
	// 36-char GUID partition, comma separated <mbr>,<guid> for hybrid
	// partitioning schemes, or 'bare' when the structure is not considered
	// a partition.
	//
	// For backwards compatibility type 'mbr' is also accepted, and the
	// structure is treated as if it is of role 'mbr'.
	Type string `yaml:"type"`
	// Role describes the role of given structure, can be one of 'mbr',
	// 'system-data', 'system-boot', 'bootimg', 'bootselect'. Structures of type 'mbr', must have a
	// size of 446 bytes and must start at 0 offset.
	Role string `yaml:"role"`
	// ID is the GPT partition ID
	ID string `yaml:"id"`
	// Filesystem used for the partition, 'vfat', 'ext4' or 'none' for
	// structures of type 'bare'
	Filesystem string `yaml:"filesystem"`
	// Content of the structure
	Content []VolumeContent `yaml:"content"`
	Update  VolumeUpdate    `yaml:"update"`
}

// HasFilesystem returns true if the structure is using a filesystem.
func (vs *VolumeStructure) HasFilesystem() bool {
	return vs.Filesystem != "none" && vs.Filesystem != ""
}

// IsPartition returns true when the structure describes a partition in a block
// device.
func (vs *VolumeStructure) IsPartition() bool {
	return vs.Type != "bare" && vs.EffectiveRole() != MBR
}

// EffectiveRole returns the role of given structure
func (vs *VolumeStructure) EffectiveRole() string {
	if vs.Role != "" {
		return vs.Role
	}
	if vs.Role == "" && vs.Type == MBR {
		return MBR
	}
	if vs.Label == SystemBoot {
		// for gadgets that only specify a filesystem-label, eg. pc
		return SystemBoot
	}
	return ""
}

// EffectiveFilesystemLabel returns the effective filesystem label, either
// explicitly provided or implied by the structure's role
func (vs *VolumeStructure) EffectiveFilesystemLabel() string {
	if vs.EffectiveRole() == SystemData {
		return ImplicitSystemDataLabel
	}
	return vs.Label
}

// VolumeContent defines the contents of the structure. The content can be
// either files within a filesystem described by the structure or raw images
// written into the area of a bare structure.
type VolumeContent struct {
	// Source is the data of the partition relative to the gadget base
	// directory
	Source string `yaml:"source"`
	// Target is the location of the data inside the root filesystem
	Target string `yaml:"target"`

	// Image names the image, relative to gadget base directory, to be used
	// for a 'bare' type structure
	Image string `yaml:"image"`
	// Offset the image is written at
	Offset *Size `yaml:"offset"`
	// OffsetWrite describes a 32-bit address, within the volume, at which
	// the offset of current image will be written. The position may be
	// specified as a byte offset relative to the start of a named structure
	OffsetWrite *RelativeOffset `yaml:"offset-write"`
	// Size of the image, when empty size is calculated by looking at the
	// image
	Size Size `yaml:"size"`

	Unpack bool `yaml:"unpack"`
}

func (vc VolumeContent) String() string {
	if vc.Image != "" {
		return fmt.Sprintf("image:%s", vc.Image)
	}
	return fmt.Sprintf("source:%s", vc.Source)
}

type VolumeUpdate struct {
	Edition  editionNumber `yaml:"edition"`
	Preserve []string      `yaml:"preserve"`
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
	if s != "system" && len(s) != validSnapIDLength {
		return false
	}
	return true
}

// InfoFromGadgetYaml reads the provided gadget metadata. If constraints is nil, only the
// self-consistency checks are performed, otherwise rules for the classic or
// system seed cases are enforced.
func InfoFromGadgetYaml(gadgetYaml []byte, constraints *ModelConstraints) (*Info, error) {
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

	if len(gi.Volumes) == 0 && (constraints == nil || constraints.Classic) {
		// volumes can be left out on classic
		// can still specify defaults though
		return &gi, nil
	}

	// basic validation
	var bootloadersFound int
	for name, v := range gi.Volumes {
		if err := validateVolume(name, &v, constraints); err != nil {
			return nil, fmt.Errorf("invalid volume %q: %v", name, err)
		}

		switch v.Bootloader {
		case "":
			// pass
		case "grub", "u-boot", "android-boot", "lk":
			bootloadersFound += 1
		default:
			return nil, errors.New("bootloader must be one of grub, u-boot, android-boot or lk")
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

// ReadInfo reads the gadget specific metadata from meta/gadget.yaml in the snap
// root directory. If constraints is nil, ReadInfo will just check for
// self-consistency, otherwise rules for the classic or system seed cases are
// enforced.
func ReadInfo(gadgetSnapRootDir string, constraints *ModelConstraints) (*Info, error) {
	gadgetYamlFn := filepath.Join(gadgetSnapRootDir, "meta", "gadget.yaml")
	gmeta, err := ioutil.ReadFile(gadgetYamlFn)
	if (constraints == nil || constraints.Classic) && os.IsNotExist(err) {
		// gadget.yaml is optional for classic gadgets
		return &Info{}, nil
	}
	if err != nil {
		return nil, err
	}

	return InfoFromGadgetYaml(gmeta, constraints)
}

func fmtIndexAndName(idx int, name string) string {
	if name != "" {
		return fmt.Sprintf("#%v (%q)", idx, name)
	}
	return fmt.Sprintf("#%v", idx)
}

type validationState struct {
	SystemSeed *VolumeStructure
	SystemData *VolumeStructure
}

func validateVolume(name string, vol *Volume, constraints *ModelConstraints) error {
	if !validVolumeName.MatchString(name) {
		return errors.New("invalid name")
	}
	if !validateSchema(vol.Schema) {
		return fmt.Errorf("invalid schema %q", vol.Schema)
	}

	// named structures, for cross-referencing relative offset-write names
	knownStructures := make(map[string]*LaidOutStructure, len(vol.Structure))
	// for uniqueness of filesystem labels
	knownFsLabels := make(map[string]bool, len(vol.Structure))
	// for validating structure overlap
	structures := make([]LaidOutStructure, len(vol.Structure))

	state := &validationState{}
	previousEnd := Size(0)
	for idx, s := range vol.Structure {
		if err := validateVolumeStructure(&s, vol); err != nil {
			return fmt.Errorf("invalid structure %v: %v", fmtIndexAndName(idx, s.Name), err)
		}
		var start Size
		if s.Offset != nil {
			start = *s.Offset
		} else {
			start = previousEnd
		}
		end := start + s.Size
		ps := LaidOutStructure{
			VolumeStructure: &vol.Structure[idx],
			StartOffset:     start,
			Index:           idx,
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

		switch s.Role {
		case SystemSeed:
			if state.SystemSeed != nil {
				return fmt.Errorf("cannot have more than one system-data role")
			}
			state.SystemSeed = &vol.Structure[idx]
		case SystemData:
			if state.SystemData != nil {
				return fmt.Errorf("cannot have more than one system-seed role")
			}
			state.SystemData = &vol.Structure[idx]
		}

		previousEnd = end
	}

	if err := ensureVolumeConsistency(state, constraints); err != nil {
		return err
	}

	// sort by starting offset
	sort.Sort(byStartOffset(structures))

	return validateCrossVolumeStructure(structures, knownStructures)
}

func ensureVolumeConsistencyNoConstraints(state *validationState) error {
	switch {
	case state.SystemSeed == nil && state.SystemData == nil:
		return nil
	case state.SystemSeed != nil && state.SystemData == nil:
		return fmt.Errorf("the system-seed role requires system-data to be defined")
	case state.SystemSeed == nil && state.SystemData != nil:
		if state.SystemData.Label != "" && state.SystemData.Label != ImplicitSystemDataLabel {
			return fmt.Errorf("system-data structure must have an implicit label or %q, not %q", ImplicitSystemDataLabel, state.SystemData.Label)
		}
	case state.SystemSeed != nil && state.SystemData != nil:
		if err := ensureSeedDataLabelsUnset(state); err != nil {
			return err
		}
	}
	return nil
}

func ensureVolumeConsistencyWithConstraints(state *validationState, constraints *ModelConstraints) error {
	switch {
	case state.SystemSeed == nil && state.SystemData == nil:
		if constraints.SystemSeed {
			return fmt.Errorf("model requires system-seed partition, but no system-seed or system-data partition found")
		}
		return nil
	case state.SystemSeed != nil && state.SystemData == nil:
		return fmt.Errorf("the system-seed role requires system-data to be defined")
	case state.SystemSeed == nil && state.SystemData != nil:
		// error if we have the SystemSeed constraint but no actual system-seed structure
		if constraints.SystemSeed {
			return fmt.Errorf("model requires system-seed structure, but none was found")
		}
		// without SystemSeed, system-data label must be implicit or writable
		if state.SystemData != nil && state.SystemData.Label != "" && state.SystemData.Label != ImplicitSystemDataLabel {
			return fmt.Errorf("system-data structure must have an implicit label or %q, not %q",
				ImplicitSystemDataLabel, state.SystemData.Label)
		}
	case state.SystemSeed != nil && state.SystemData != nil:
		// error if we don't have the SystemSeed constraint but we have a system-seed structure
		if !constraints.SystemSeed {
			return fmt.Errorf("model does not support the system-seed role")
		}
		if err := ensureSeedDataLabelsUnset(state); err != nil {
			return err
		}
	}
	return nil
}

func ensureVolumeConsistency(state *validationState, constraints *ModelConstraints) error {
	if constraints == nil {
		return ensureVolumeConsistencyNoConstraints(state)
	}
	return ensureVolumeConsistencyWithConstraints(state, constraints)
}

func ensureSeedDataLabelsUnset(state *validationState) error {
	if state.SystemData.Label != "" {
		return fmt.Errorf("system-data structure must not have a label")
	}
	if state.SystemSeed.Label != "" {
		return fmt.Errorf("system-seed structure must not have a label")
	}

	return nil
}

func validateCrossVolumeStructure(structures []LaidOutStructure, knownStructures map[string]*LaidOutStructure) error {
	previousEnd := Size(0)
	// cross structure validation:
	// - relative offsets that reference other structures by name
	// - laid out structure overlap
	// use structures laid out within the volume
	for pidx, ps := range structures {
		if ps.EffectiveRole() == MBR {
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
		previousEnd = ps.StartOffset + ps.Size

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
	if err := validateRole(vs, vol); err != nil {
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

	if err := validateStructureUpdate(&vs.Update, vs); err != nil {
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
		schema = GPT
	}

	if s == "" {
		return errors.New(`type is not specified`)
	}

	if s == "bare" {
		// unknonwn blob
		return nil
	}

	if s == MBR {
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
		isHybrid = true
		// hybrid ID
		code := s[:idx]
		guid := s[idx+1:]
		if len(code) != 2 || len(guid) != 36 || !validTypeID.MatchString(code) || !validGUUID.MatchString(guid) {
			return fmt.Errorf("invalid format of hybrid type")
		}
	}

	// hybrid schema requires a hybrid type, but GPT/MBR schemas accept both
	// schema specific type or a hybrid one, in the latter case the schema
	// specific part of the hybrid type is relevant
	switch {
	case schema == Hybrid && !isHybrid:
		return fmt.Errorf("non-hybrid type with a hybrid schema")
	case schema == GPT && isMBR:
		return fmt.Errorf("MBR structure type with non-MBR schema %q", vol.Schema)
	case schema == MBR && isGPT:
		// type: <uuid> is only valid for GPT volumes
		return fmt.Errorf("GUID structure type with non-GPT schema %q", vol.Schema)
	}

	return nil
}

func validateRole(vs *VolumeStructure, vol *Volume) error {
	if vs.Type == "bare" {
		if vs.Role != "" && vs.Role != MBR {
			return fmt.Errorf("conflicting type: %q", vs.Type)
		}
	}
	vsRole := vs.Role
	if vs.Type == MBR {
		if vsRole != "" && vsRole != MBR {
			return fmt.Errorf(`conflicting legacy type: "mbr"`)
		}
		// backward compatibility
		vsRole = MBR
	}

	switch vsRole {
	case SystemData, SystemSeed:
		// roles have cross dependencies, consistency checks are done at
		// the volume level
	case MBR:
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
	case SystemBoot, BootImage, BootSelect, "":
		// noop
	case LegacyBootImage, LegacyBootSelect:
		// noop
		// legacy role names were added in 2.42 can be removed
		// on snapd epoch bump
	default:
		return fmt.Errorf("unsupported role")
	}
	return nil
}

func validateBareContent(vc *VolumeContent) error {
	if vc.Source != "" || vc.Target != "" {
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
	if vc.Source == "" || vc.Target == "" {
		return fmt.Errorf("missing source or target")
	}
	return nil
}

func validateStructureUpdate(up *VolumeUpdate, vs *VolumeStructure) error {
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

func validateSchema(schema string) bool {
	switch schema {
	case "", GPT, MBR, Hybrid:
		return true
	}
	return false
}

type editionNumber uint32

func (e *editionNumber) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var es string
	if err := unmarshal(&es); err != nil {
		return errors.New(`cannot unmarshal "edition"`)
	}

	u, err := strconv.ParseUint(es, 10, 32)
	if err != nil {
		return fmt.Errorf(`"edition" must be a positive number, not %q`, es)
	}
	*e = editionNumber(u)
	return nil
}

// Size describes the size of a structure item or an offset within the
// structure.
type Size uint64

const (
	SizeKiB = Size(1 << 10)
	SizeMiB = Size(1 << 20)
	SizeGiB = Size(1 << 30)

	// SizeMBR is the maximum byte size of a structure of role 'mbr'
	SizeMBR = Size(446)
	// SizeLBA48Pointer is the byte size of a pointer value written at the
	// location described by 'offset-write'
	SizeLBA48Pointer = Size(4)
)

func (s *Size) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var gs string
	if err := unmarshal(&gs); err != nil {
		return errors.New(`cannot unmarshal gadget size`)
	}

	var err error
	*s, err = ParseSize(gs)
	if err != nil {
		return fmt.Errorf("cannot parse size %q: %v", gs, err)
	}
	return err
}

// ParseSize parses a string expressing size in gadget declaration. The
// accepted format is one of: <bytes> | <bytes/2^20>M | <bytes/2^30>G.
func ParseSize(gs string) (Size, error) {
	number, unit, err := strutil.SplitUnit(gs)
	if err != nil {
		return 0, err
	}
	if number < 0 {
		return 0, errors.New("size cannot be negative")
	}
	var size Size
	switch unit {
	case "M":
		// MiB
		size = Size(number) * SizeMiB
	case "G":
		// GiB
		size = Size(number) * SizeGiB
	case "":
		// straight bytes
		size = Size(number)
	default:
		return 0, fmt.Errorf("invalid suffix %q", unit)
	}
	return size, nil
}

func (s *Size) String() string {
	if s == nil {
		return "unspecified"
	}
	return fmt.Sprintf("%d", *s)
}

// RelativeOffset describes an offset where structure data is written at.
// The position can be specified as byte-offset relative to the start of another
// named structure.
type RelativeOffset struct {
	// RelativeTo names the structure relative to which the location of the
	// address write will be calculated.
	RelativeTo string
	// Offset is a 32-bit value
	Offset Size
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

// ParseRelativeOffset parses a string describing an offset that can be
// expressed relative to a named structure, with the format: [<name>+]<size>.
func ParseRelativeOffset(grs string) (*RelativeOffset, error) {
	toWhat := ""
	sizeSpec := grs
	if idx := strings.IndexRune(grs, '+'); idx != -1 {
		toWhat, sizeSpec = grs[:idx], grs[idx+1:]
		if toWhat == "" {
			return nil, errors.New("missing volume name")
		}
	}
	if sizeSpec == "" {
		return nil, errors.New("missing offset")
	}

	size, err := ParseSize(sizeSpec)
	if err != nil {
		return nil, fmt.Errorf("cannot parse offset %q: %v", sizeSpec, err)
	}
	if size > 4*SizeGiB {
		return nil, fmt.Errorf("offset above 4G limit")
	}

	return &RelativeOffset{
		RelativeTo: toWhat,
		Offset:     size,
	}, nil
}

func (s *RelativeOffset) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var grs string
	if err := unmarshal(&grs); err != nil {
		return errors.New(`cannot unmarshal gadget relative offset`)
	}

	ro, err := ParseRelativeOffset(grs)
	if err != nil {
		return fmt.Errorf("cannot parse relative offset %q: %v", grs, err)
	}
	*s = *ro
	return nil
}

// IsCompatible checks whether the current and an update are compatible. Returns
// nil or an error describing the incompatibility.
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

	// layout both volumes partially, without going deep into the layout of
	// structure content, we only want to make sure that structures are
	// comapatible
	pCurrent, err := LayoutVolumePartially(currentVol, defaultConstraints)
	if err != nil {
		return fmt.Errorf("cannot lay out the current volume: %v", err)
	}
	pNew, err := LayoutVolumePartially(newVol, defaultConstraints)
	if err != nil {
		return fmt.Errorf("cannot lay out the new volume: %v", err)
	}
	if err := isLayoutCompatible(pCurrent, pNew); err != nil {
		return fmt.Errorf("incompatible layout change: %v", err)
	}
	return nil
}

// PositionedVolumeFromGadget takes a gadget rootdir and positions the
// partitions as specified.
func PositionedVolumeFromGadget(gadgetRoot string) (*LaidOutVolume, error) {
	info, err := ReadInfo(gadgetRoot, nil)
	if err != nil {
		return nil, err
	}
	// Limit ourselves to just one volume for now.
	if len(info.Volumes) != 1 {
		return nil, fmt.Errorf("cannot position multiple volumes yet")
	}

	constraints := LayoutConstraints{
		NonMBRStartOffset: 1 * SizeMiB,
		SectorSize:        512,
	}

	for _, vol := range info.Volumes {
		pvol, err := LayoutVolume(gadgetRoot, &vol, constraints)
		if err != nil {
			return nil, err
		}
		// we know  info.Volumes map has size 1 so we can return here
		return pvol, nil
	}
	return nil, fmt.Errorf("internal error in PositionedVolumeFromGadget: this line cannot be reached")
}
