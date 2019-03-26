// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package snap

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/strutil"
)

var (
	validVolumeName = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9-]+$")
	validTypeID     = regexp.MustCompile("^[0-9A-F]{2}$")
	validGUUID      = regexp.MustCompile("^[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}$")
)

type GadgetInfo struct {
	Volumes map[string]GadgetVolume `yaml:"volumes,omitempty"`

	// Default configuration for snaps (snap-id => key => value).
	Defaults map[string]map[string]interface{} `yaml:"defaults,omitempty"`

	Connections []GadgetConnection `yaml:"connections"`
}

type GadgetVolume struct {
	Schema     string            `yaml:"schema"`
	Bootloader string            `yaml:"bootloader"`
	ID         string            `yaml:"id"`
	Structure  []VolumeStructure `yaml:"structure"`
}

// TODO Offsets and sizes are strings to support unit suffixes.
// Is that a good idea? *2^N or *10^N? We'll probably want a richer
// type when we actually handle these.

type VolumeStructure struct {
	Name        string               `yaml:"name"`
	Label       string               `yaml:"filesystem-label"`
	Offset      GadgetSize           `yaml:"offset"`
	OffsetWrite GadgetRelativeOffset `yaml:"offset-write"`
	Size        GadgetSize           `yaml:"size"`
	Type        string               `yaml:"type"`
	Role        string               `yaml:"role"`
	ID          string               `yaml:"id"`
	Filesystem  string               `yaml:"filesystem"`
	Content     []VolumeContent      `yaml:"content"`
	Update      VolumeUpdate         `yaml:"update"`
}

// IsBare returns true if the structure is not using a filesystem.
func (vs *VolumeStructure) IsBare() bool {
	return vs.Filesystem == "none" || vs.Filesystem == ""
}

type VolumeContent struct {
	// filesystem content
	Source string `yaml:"source"`
	Target string `yaml:"target"`

	// bare content
	Image       string               `yaml:"image"`
	Offset      GadgetSize           `yaml:"offset"`
	OffsetWrite GadgetRelativeOffset `yaml:"offset-write"`
	Size        GadgetSize           `yaml:"size"`

	Unpack bool `yaml:"unpack"`
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
type GadgetConnection struct {
	Plug GadgetConnectionPlug `yaml:"plug"`
	Slot GadgetConnectionSlot `yaml:"slot"`
}

type GadgetConnectionPlug struct {
	SnapID string
	Plug   string
}

func (gcplug *GadgetConnectionPlug) Empty() bool {
	return gcplug.SnapID == "" && gcplug.Plug == ""
}

func (gcplug *GadgetConnectionPlug) UnmarshalYAML(unmarshal func(interface{}) error) error {
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

type GadgetConnectionSlot struct {
	SnapID string
	Slot   string
}

func (gcslot *GadgetConnectionSlot) Empty() bool {
	return gcslot.SnapID == "" && gcslot.Slot == ""
}

func (gcslot *GadgetConnectionSlot) UnmarshalYAML(unmarshal func(interface{}) error) error {
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

// ReadGadgetInfo reads the gadget specific metadata from gadget.yaml
// in the snap. classic set to true means classic rules apply,
// i.e. content/presence of gadget.yaml is fully optional.
func ReadGadgetInfo(info *Info, classic bool) (*GadgetInfo, error) {
	const errorFormat = "cannot read gadget snap details: %s"

	if info.Type != TypeGadget {
		return nil, fmt.Errorf(errorFormat, "not a gadget snap")
	}

	var gi GadgetInfo

	gadgetYamlFn := filepath.Join(info.MountDir(), "meta", "gadget.yaml")
	gmeta, err := ioutil.ReadFile(gadgetYamlFn)
	if classic && os.IsNotExist(err) {
		// gadget.yaml is optional for classic gadgets
		return &gi, nil
	}
	if err != nil {
		return nil, fmt.Errorf(errorFormat, err)
	}

	if err := yaml.Unmarshal(gmeta, &gi); err != nil {
		return nil, fmt.Errorf(errorFormat, err)
	}

	for k, v := range gi.Defaults {
		if !systemOrSnapID(k) {
			return nil, fmt.Errorf(`default stanza not keyed by "system" or snap-id: %s`, k)
		}
		dflt, err := normalizeYamlValue(v)
		if err != nil {
			return nil, fmt.Errorf("default value %q of %q: %v", v, k, err)
		}
		gi.Defaults[k] = dflt.(map[string]interface{})
	}

	for i, gconn := range gi.Connections {
		if gconn.Plug.Empty() {
			return nil, fmt.Errorf("gadget connection plug cannot be empty")
		}
		if gconn.Slot.Empty() {
			gi.Connections[i].Slot.SnapID = "system"
			gi.Connections[i].Slot.Slot = gconn.Plug.Plug
		}
	}

	if classic && len(gi.Volumes) == 0 {
		// volumes can be left out on classic
		// can still specify defaults though
		return &gi, nil
	}

	// basic validation
	var bootloadersFound int
	for name, v := range gi.Volumes {
		if err := validateVolume(name, &v); err != nil {
			return nil, fmt.Errorf("invalid volume %q: %v", name, err)
		}

		switch v.Bootloader {
		case "":
			// pass
		case "grub", "u-boot", "android-boot":
			bootloadersFound += 1
		default:
			return nil, fmt.Errorf(errorFormat, "bootloader must be one of grub, u-boot or android-boot")
		}
	}
	switch {
	case bootloadersFound == 0:
		return nil, fmt.Errorf(errorFormat, "bootloader not declared in any volume")
	case bootloadersFound > 1:
		return nil, fmt.Errorf(errorFormat, fmt.Sprintf("too many (%d) bootloaders declared", bootloadersFound))
	}

	return &gi, nil
}

func fmtIndexAndName(idx int, name string) string {
	if name != "" {
		return fmt.Sprintf("#%v (%q)", idx, name)
	}
	return fmt.Sprintf("#%v", idx)
}

func validateVolume(name string, vol *GadgetVolume) error {
	if !validVolumeName.MatchString(name) {
		return errors.New("invalid volume name")
	}
	if vol.Schema != "" && vol.Schema != "gpt" && vol.Schema != "mbr" {
		return fmt.Errorf("invalid volume schema %q", vol.Schema)
	}

	structureNames := make(map[string]bool, len(vol.Structure))
	for idx, s := range vol.Structure {
		if err := validateVolumeStructure(&s, vol); err != nil {
			return fmt.Errorf("invalid structure %s: %v", fmtIndexAndName(idx, s.Name), err)
		}
		if s.Name != "" {
			structureNames[s.Name] = true
		}
	}

	// validate relative offsets that reference other structures by name
	for idx, s := range vol.Structure {
		if s.OffsetWrite.RelativeTo != "" && !structureNames[s.OffsetWrite.RelativeTo] {
			return fmt.Errorf("structure %v refers to an unknown structure %q",
				fmtIndexAndName(idx, s.Name), s.OffsetWrite.RelativeTo)
		}

		if !s.IsBare() {
			// content relative offset only possible if it's a bare structure
			continue
		}
		for cidx, c := range s.Content {
			if c.OffsetWrite.RelativeTo != "" && !structureNames[c.OffsetWrite.RelativeTo] {
				return fmt.Errorf("structure %v, content %v refers to an unknown structure %q",
					fmtIndexAndName(idx, s.Name), fmtIndexAndName(cidx, c.Image), c.OffsetWrite.RelativeTo)
			}
		}
	}
	return nil
}

func validateStructureType(s string, vol *GadgetVolume) error {
	// Type can be one of:
	// - "mbr" (backwards compatible)
	// - "bare"
	// - [0-9A-Z]{2} - MBR type
	// - hybrid ID
	//
	// Hybrid ID is 2 hex digits of MBR type, followed by 36 GUUID
	// example: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B

	schema := vol.Schema
	if schema == "" {
		schema = "gpt"
	}

	if s == "" {
		return errors.New(`type is not specified`)
	}

	if s == "bare" {
		// unknonwn blob
		return nil
	}

	if s == "mbr" {
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
			return fmt.Errorf("invalid format of type ID")
		}
	} else {
		// hybrid ID
		code := s[:idx]
		guid := s[idx+1:]
		if len(code) != 2 || len(guid) != 36 || !validTypeID.MatchString(code) || !validGUUID.MatchString(guid) {
			return fmt.Errorf("invalid format of hybrid type ID")
		}
	}

	if schema != "gpt" && isGPT {
		// type: <uuid> is only valid for GPT volumes
		return fmt.Errorf("GUID structure ID %q with non-GPT schema %q", s, vol.Schema)
	}
	if schema != "mbr" && isMBR {
		return fmt.Errorf("MBR structure ID %q with non-MBR schema %q", s, vol.Schema)
	}

	return nil
}

func validateRole(vs *VolumeStructure, vol *GadgetVolume) error {
	if vs.Type == "bare" {
		if vs.Role != "" && vs.Role != "mbr" {
			return fmt.Errorf("confclicting role/type: %q/%q", vs.Role, vs.Type)
		}
	}
	vsRole := vs.Role
	if vs.Type == "mbr" {
		// backward compatibility
		vsRole = "mbr"
	}

	switch vsRole {
	case "system-data":
		if vs.Label != "" && vs.Label != "writable" {
			return fmt.Errorf(`role %q must have an implicit label or "writable", not %q`, vs.Role, vs.Label)
		}
	case "mbr":
		if vs.Size > 446 {
			return errors.New("mbr structures cannot be larger than 446 bytes")
		}
		if vs.Offset != 0 {
			return errors.New("mbr structure must start at offset 0")
		}
		if vs.ID != "" {
			return errors.New("mbr structure must not specify partition ID")
		}
		if vs.Filesystem != "" && vs.Filesystem != "none" {
			return errors.New("mbr structures must not specify a file system")
		}
	case "system-boot", "":
		// noop
	default:
		return fmt.Errorf("invalid role %q", vs.Role)
	}
	return nil
}

func validateVolumeStructure(vs *VolumeStructure, vol *GadgetVolume) error {
	if err := validateStructureType(vs.Type, vol); err != nil {
		return fmt.Errorf("invalid type %q: %v", vs.Type, err)
	}
	if err := validateRole(vs, vol); err != nil {
		return fmt.Errorf("invalid role %q: %v", vs.Role, err)
	}
	if vs.Filesystem != "" && !strutil.ListContains([]string{"ext4", "vfat", "none"}, vs.Filesystem) {
		return fmt.Errorf("invalid filesystem %q", vs.Filesystem)
	}

	bare := vs.IsBare()
	for i, c := range vs.Content {
		var err error
		if bare {
			err = validateBareContent(&c)
		} else {
			err = validateFilesystemContent(&c)
		}
		if err != nil {
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
	if vc.Image != "" || vc.Offset != 0 || vc.OffsetWrite != (GadgetRelativeOffset{}) || vc.Size != 0 {
		return fmt.Errorf("cannot use image content for non-bare file system")
	}
	if vc.Source == "" || vc.Target == "" {
		return fmt.Errorf("missing source or target")
	}
	return nil
}

func validateStructureUpdate(up *VolumeUpdate, vs *VolumeStructure) error {
	if vs.IsBare() && len(vs.Update.Preserve) > 0 {
		return errors.New("preserving files during update is not supported for non-filesystem structures")
	}

	names := make(map[string]bool, len(vs.Update.Preserve))
	for _, n := range vs.Update.Preserve {
		if names[n] {
			return fmt.Errorf("duplicate preserve entry %q", n)
		}
		names[n] = true
	}
	return nil
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

// GadgetSize describes the size of a structure item or an offset within the
// structure.
type GadgetSize uint64

const (
	SizeMiB = GadgetSize(2 << 20)
	SizeGiB = GadgetSize(2 << 30)
)

func (s *GadgetSize) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var gs string
	if err := unmarshal(&gs); err != nil {
		return errors.New(`failed to unmarshal`)
	}

	var err error
	*s, err = ParseGadgetSize(gs)
	if err != nil {
		return fmt.Errorf("cannot parse size %q: %v", gs, err)
	}
	return err
}

// ParseGadgetSize parses a string expressing size in gadget declaration. The
// accepted format is one of: <bytes> | <bytes/2^20>M | <bytes/2^30>G.
func ParseGadgetSize(gs string) (GadgetSize, error) {
	number, unit, err := strutil.SplitUnit(gs)
	if err != nil {
		return 0, err
	}
	if number < 0 {
		return 0, errors.New("size cannot be negative")
	}
	var size GadgetSize
	switch unit {
	case "M":
		// MiB
		size = GadgetSize(number) * SizeMiB
	case "G":
		// GiB
		size = GadgetSize(number) * SizeGiB
	case "":
		// straight bytes
		size = GadgetSize(number)
	default:
		return 0, fmt.Errorf("invalid suffix %q", unit)
	}
	return size, nil
}

// GadgetRelativeOffset describes an offset where structure data is written at.
// The position can be specified as byte-offset relative to the start of another
// named structure.
type GadgetRelativeOffset struct {
	RelativeTo string
	Offset     GadgetSize
}

// ParseGadgetRelativeOffset parses a string describing an offset that can be
// expressed relative to a named structure, with the format: [<name>+]<size>.
func ParseGadgetRelativeOffset(grs string) (*GadgetRelativeOffset, error) {
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

	size, err := ParseGadgetSize(sizeSpec)
	if err != nil {
		return nil, fmt.Errorf("cannot parse offset %q: %v", sizeSpec, err)
	}
	if size > 4*SizeGiB {
		// above 4GB
		return nil, fmt.Errorf("offset above 4G limit")
	}

	return &GadgetRelativeOffset{
		RelativeTo: toWhat,
		Offset:     size,
	}, nil
}

func (s *GadgetRelativeOffset) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var grs string
	if err := unmarshal(&grs); err != nil {
		return errors.New(`failed to unmarshal`)
	}

	ro, err := ParseGadgetRelativeOffset(grs)
	if err != nil {
		return fmt.Errorf("cannot parse relative offset %q: %v", grs, err)
	}
	*s = *ro
	return nil
}
