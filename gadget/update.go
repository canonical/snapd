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
	"strings"

	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/logger"
)

var (
	ErrNoUpdate = errors.New("nothing to update")
)

var (
	// default positioning constraints that match ubuntu-image
	DefaultConstraints = LayoutConstraints{
		NonMBRStartOffset: 1 * quantity.OffsetMiB,
	}
)

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

type ContentOperation int
type ContentChangeAction int

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
	// the scenarios, the target path is relative under the root.
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
	Observe(op ContentOperation, sourceStruct *LaidOutStructure,
		targetRootDir, relativeTargetPath string, dataChange *ContentChange) (ContentChangeAction, error)
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

func onDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout *LaidOutVolume, diskLayout *OnDiskVolume, s OnDiskStructure) bool {
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
	// * the size of this structure extends to the end of the disk
	// * there is no system-data role in the gadget.yaml

	// bare structures don't show up on disk, so we can't include them
	// when calculating how many "structures" are in gadgetLayout to
	// ensure that there is only one extra OnDiskStructure at the end
	numPartsInGadget := 0
	for _, s := range gadgetLayout.Structure {
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

	structEnd := quantity.Offset(s.Size) + s.StartOffset
	diskEnd := quantity.Offset(uint64(diskLayout.SectorSize) * diskLayout.UsableSectorsEnd)

	return s.Filesystem == "ext4" &&
		s.Type == "0FC63DAF-8483-4772-8E79-3D69D8477DE4" && // TODO: check hybrid and on MBR/DOS too
		s.Label == "writable" &&
		// StructureIndex is 1-based
		s.StructureIndex == numPartsOnDisk &&
		numPartsInGadget+1 == numPartsOnDisk &&
		structEnd == diskEnd
}
type EnsureLayoutCompatibilityOptions struct {
	AssumeCreatablePartitionsCreated bool
}

func EnsureLayoutCompatibility(gadgetLayout *LaidOutVolume, diskLayout *OnDiskVolume, opts *EnsureLayoutCompatibilityOptions) error {
	if opts == nil {
		opts = &EnsureLayoutCompatibilityOptions{}
	}
	eq := func(ds OnDiskStructure, gs LaidOutStructure) (bool, string) {
		dv := ds.VolumeStructure
		gv := gs.VolumeStructure

		// name mismatch
		if gv.Name != dv.Name {
			// partitions have no names in MBR so bypass the name check
			if gadgetLayout.Schema != "mbr" {
				// don't return a reason if the names don't match
				return false, ""
			}
		}

		// start offset mismatch
		if ds.StartOffset != gs.StartOffset {
			return false, fmt.Sprintf("start offsets do not match (disk: %d (%s) and gadget: %d (%s))",
				ds.StartOffset, ds.StartOffset.IECString(), gs.StartOffset, gs.StartOffset.IECString())
		}

		switch {
		// on disk size too small
		case dv.Size < gv.Size:
			return false, fmt.Sprintf("on disk size %d (%s) is smaller than gadget size %d (%s)",
				dv.Size, dv.Size.IECString(), gv.Size, gv.Size.IECString())

		// on disk size too large
		case dv.Size > gv.Size:
			// larger on disk size is allowed specifically only for system-data
			if gv.Role != SystemData {
				return false, fmt.Sprintf("on disk size %d (%s) is larger than gadget size %d (%s) (and the role should not be expanded)",
					dv.Size, dv.Size.IECString(), gv.Size, gv.Size.IECString())
			}
		}

		// If we got to this point, the structure on disk has the same name,
		// size and offset, so the last thing to check is that the filesystem
		// matches (or that we don't care about the filesystem).

		// TODO: here we need to handle in the strict case partitions which are
		// to be created at install and are encrypted like ubuntu-data as they
		// will not match filesystems exactly and need some massaging

		if opts.AssumeCreatablePartitionsCreated || !IsCreatableAtInstall(gv) {
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
			// image file with a new binary image file.
			if gv.Filesystem != "" {
				// then the gadget specified a filesystem and the on disk needs
				// to match
				if gv.Filesystem != dv.Filesystem {
					// use more specific error message for structures that are
					// not creatable at install
					if !IsCreatableAtInstall(gv) {
						return false, "filesystems do not match and the partition is not creatable at install"
					}
					// otherwise generic
					return false, "filesystems do not match"
				}
			}
		}

		// otherwise if we got here things are matching
		return true, ""
	}

	laidOutContains := func(haystack []LaidOutStructure, needle OnDiskStructure) (bool, string) {
		reasonAbsent := ""
		for _, h := range haystack {
			matches, reasonNotMatches := eq(needle, h)
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
		return false, reasonAbsent
	}

	onDiskContains := func(haystack []OnDiskStructure, needle LaidOutStructure) (bool, string) {
		reasonAbsent := ""
		for _, h := range haystack {
			matches, reasonNotMatches := eq(h, needle)
			if matches {
				return true, ""
			}
			// this has the effect of only returning the last non-empty reason
			// string
			if reasonNotMatches != "" {
				reasonAbsent = reasonNotMatches
			}
		}
		return false, reasonAbsent
	}

	// check size of volumes
	lastUsableByte := quantity.Size(diskLayout.UsableSectorsEnd) * diskLayout.SectorSize
	if gadgetLayout.Size > lastUsableByte {
		return fmt.Errorf("device %v (last usable byte at %s) is too small to fit the requested layout (%s)", diskLayout.Device,
			lastUsableByte.IECString(), gadgetLayout.Size.IECString())
	}

	// check that the sizes of all structures in the gadget are multiples of
	// the disk sector size (unless the structure is the MBR)
	for _, ls := range gadgetLayout.LaidOutStructure {
		if !IsRoleMBR(ls) {
			if ls.Size%diskLayout.SectorSize != 0 {
				return fmt.Errorf("gadget volume structure %v size is not a multiple of disk sector size %v",
					ls, diskLayout.SectorSize)
			}
		}
	}

	// Check if top level properties match
	if !isCompatibleSchema(gadgetLayout.Volume.Schema, diskLayout.Schema) {
		return fmt.Errorf("disk partitioning schema %q doesn't match gadget schema %q", diskLayout.Schema, gadgetLayout.Volume.Schema)
	}
	if gadgetLayout.Volume.ID != "" && gadgetLayout.Volume.ID != diskLayout.ID {
		return fmt.Errorf("disk ID %q doesn't match gadget volume ID %q", diskLayout.ID, gadgetLayout.Volume.ID)
	}

	// Check if all existing device partitions are also in gadget
	for _, ds := range diskLayout.Structure {
		present, reasonAbsent := laidOutContains(gadgetLayout.LaidOutStructure, ds)
		if !present {
			if reasonAbsent != "" {
				// use the right format so that it can be
				// appended to the error message
				reasonAbsent = fmt.Sprintf(": %s", reasonAbsent)
			}
			return fmt.Errorf("cannot find disk partition %s (starting at %d) in gadget%s", ds.Node, ds.StartOffset, reasonAbsent)
		}
	}

	// check all structures in the layout are present in the gadget, or have a
	// valid excuse for absence (i.e. mbr or creatable structures at install)
	for _, gs := range gadgetLayout.LaidOutStructure {
		// we ignore reasonAbsent here since if there was an extra on disk
		// structure that didn't match something in the YAML, we would have
		// caught it above, this loop can only ever identify structures in the
		// YAML that are not on disk at all
		if present, _ := onDiskContains(diskLayout.Structure, gs); present {
			continue
		}

		// otherwise not present, figure out if it has a valid excuse

		if !gs.IsPartition() {
			// raw structures like mbr or other "bare" type will not be
			// identified by linux and thus should be skipped as they will not
			// show up on the disk
			continue
		}

		// allow structures that are creatable during install if we don't assume
		// created partitions to already exist
		if IsCreatableAtInstall(gs.VolumeStructure) && !opts.AssumeCreatablePartitionsCreated {
			continue
		}

		return fmt.Errorf("cannot find gadget structure %s on disk", gs.String())
	}

	return nil
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
//
// The rules for gadget/kernel updates with "$kernel:refs":
//
// 1. When installing a kernel with assets that have "update: true"
//    there *must* be a matching entry in gadget.yaml. If not we risk
//    bricking the system because the kernel tells us that it *needs*
//    this file to boot but without gadget.yaml we would not put it
//    anywhere.
// 2. When installing a gadget with "$kernel:ref" content it is okay
//    if this content cannot get resolved as long as there is no
//    "edition" jump. This means adding new "$kernel:ref" without
//    "edition" updates is always possible.
//
// To add a new "$kernel:ref" to gadget/kernel:
// a. Update gadget and gadget.yaml and add "$kernel:ref" but do not
//    update edition (if edition update is needed, use epoch)
// b. Update kernel and kernel.yaml with new assets.
// c. snapd will refresh gadget (see rule 2) but refuse to take the
//    new kernel (rule 1)
// d. After step (c) is completed the kernel refresh will now also
//    work (no more violation of rule 1)
func Update(model Model, old, new GadgetData, rollbackDirPath string, updatePolicy UpdatePolicyFunc, observer ContentUpdateObserver) error {
	// TODO: support multi-volume gadgets. But for now we simply
	//       do not do any gadget updates on those. We cannot error
	//       here because this would break refreshes of gadgets even
	//       when they don't require any updates.
	if len(new.Info.Volumes) != 1 || len(old.Info.Volumes) != 1 {
		logger.Noticef("WARNING: gadget assests cannot be updated yet when multiple volumes are used")
		return nil
	}

	oldVol, newVol, err := resolveVolume(old.Info, new.Info)
	if err != nil {
		return err
	}

	if oldVol.Schema == "" || newVol.Schema == "" {
		return fmt.Errorf("internal error: unset volume schemas: old: %q new: %q", oldVol.Schema, newVol.Schema)
	}

	// layout old partially, without going deep into the layout of structure
	// content
	pOld, err := LayoutVolumePartially(oldVol, DefaultConstraints)
	if err != nil {
		return fmt.Errorf("cannot lay out the old volume: %v", err)
	}

	// Layout new volume, delay resolving of filesystem content
	constraints := DefaultConstraints
	constraints.SkipResolveContent = true
	pNew, err := LayoutVolume(new.RootDir, new.KernelRootDir, newVol, constraints)
	if err != nil {
		return fmt.Errorf("cannot lay out the new volume: %v", err)
	}

	if err := canUpdateVolume(pOld, pNew); err != nil {
		return fmt.Errorf("cannot apply update to volume: %v", err)
	}

	if updatePolicy == nil {
		updatePolicy = defaultPolicy
	}

	// ensure all required kernel assets are found in the gadget
	kernelInfo, err := kernel.ReadInfo(new.KernelRootDir)
	if err != nil {
		return err
	}
	if err := gadgetVolumeConsumesOneKernelUpdateAsset(pNew.Volume, kernelInfo); err != nil {
		return err
	}

	// now we know which structure is which, find which ones need an update
	updates, err := resolveUpdate(pOld, pNew, updatePolicy, new.RootDir, new.KernelRootDir, kernelInfo)
	if err != nil {
		return err
	}
	if len(updates) == 0 {
		// nothing to update
		return ErrNoUpdate
	}

	// can update old layout to new layout
	for _, update := range updates {
		if err := canUpdateStructure(update.from, update.to, pNew.Schema); err != nil {
			return fmt.Errorf("cannot update volume structure %v: %v", update.to, err)
		}
	}

	return applyUpdates(new, updates, rollbackDirPath, observer)
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

func isSameOffset(one *quantity.Offset, two *quantity.Offset) bool {
	if one == nil && two == nil {
		return true
	}
	if one != nil && two != nil {
		return *one == *two
	}
	return false
}

func isSameRelativeOffset(one *RelativeOffset, two *RelativeOffset) bool {
	if one == nil && two == nil {
		return true
	}
	if one != nil && two != nil {
		return *one == *two
	}
	return false
}

func isLegacyMBRTransition(from *LaidOutStructure, to *LaidOutStructure) bool {
	// legacy MBR could have been specified by setting type: mbr, with no
	// role
	return from.Type == schemaMBR && to.Role == schemaMBR
}

func canUpdateStructure(from *LaidOutStructure, to *LaidOutStructure, schema string) error {
	if schema == schemaGPT && from.Name != to.Name {
		// partition names are only effective when GPT is used
		return fmt.Errorf("cannot change structure name from %q to %q", from.Name, to.Name)
	}
	if from.Size != to.Size {
		return fmt.Errorf("cannot change structure size from %v to %v", from.Size, to.Size)
	}
	if !isSameOffset(from.Offset, to.Offset) {
		return fmt.Errorf("cannot change structure offset from %v to %v", from.Offset, to.Offset)
	}
	if from.StartOffset != to.StartOffset {
		return fmt.Errorf("cannot change structure start offset from %v to %v", from.StartOffset, to.StartOffset)
	}
	// TODO: should this limitation be lifted?
	if !isSameRelativeOffset(from.OffsetWrite, to.OffsetWrite) {
		return fmt.Errorf("cannot change structure offset-write from %v to %v", from.OffsetWrite, to.OffsetWrite)
	}
	if from.Role != to.Role {
		return fmt.Errorf("cannot change structure role from %q to %q", from.Role, to.Role)
	}
	if from.Type != to.Type {
		if !isLegacyMBRTransition(from, to) {
			return fmt.Errorf("cannot change structure type from %q to %q", from.Type, to.Type)
		}
	}
	if from.ID != to.ID {
		return fmt.Errorf("cannot change structure ID from %q to %q", from.ID, to.ID)
	}
	if to.HasFilesystem() {
		if !from.HasFilesystem() {
			return fmt.Errorf("cannot change a bare structure to filesystem one")
		}
		if from.Filesystem != to.Filesystem {
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
	if from.Schema != to.Schema {
		return fmt.Errorf("cannot change volume schema from %q to %q", from.Schema, to.Schema)
	}
	if len(from.LaidOutStructure) != len(to.LaidOutStructure) {
		return fmt.Errorf("cannot change the number of structures within volume from %v to %v", len(from.LaidOutStructure), len(to.LaidOutStructure))
	}
	return nil
}

type updatePair struct {
	from *LaidOutStructure
	to   *LaidOutStructure
}

func defaultPolicy(from, to *LaidOutStructure) (bool, ResolvedContentFilterFunc) {
	return to.Update.Edition > from.Update.Edition, nil
}

// RemodelUpdatePolicy implements the update policy of a remodel scenario. The
// policy selects all non-MBR structures for the update.
func RemodelUpdatePolicy(from, to *LaidOutStructure) (bool, ResolvedContentFilterFunc) {
	if from.Role == schemaMBR {
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
	for _, ct := range to.Content {
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
	for j, oldStruct := range oldVol.LaidOutStructure {
		newStruct := newVol.LaidOutStructure[j]
		// update only when the policy says so; boot assets
		// are assumed to be backwards compatible, once
		// deployed they are not rolled back or replaced unless
		// told by the new policy
		if update, filter := policy(&oldStruct, &newStruct); update {
			// Ensure content is resolved and filtered. Filtering
			// is required for e.g. KernelUpdatePolicy, see above.
			resolvedContent, err := resolveVolumeContent(newGadgetRootDir, newKernelRootDir, kernelInfo, &newStruct, filter)
			if err != nil {
				return nil, err
			}
			// Nothing to do after filtering
			if filter != nil && len(resolvedContent) == 0 && len(newStruct.LaidOutContent) == 0 {
				continue
			}
			newVol.LaidOutStructure[j].ResolvedContent = resolvedContent

			// and add to updates
			updates = append(updates, updatePair{
				from: &oldVol.LaidOutStructure[j],
				to:   &newVol.LaidOutStructure[j],
			})
		}
	}
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

func applyUpdates(new GadgetData, updates []updatePair, rollbackDir string, observer ContentUpdateObserver) error {
	updaters := make([]Updater, len(updates))

	for i, one := range updates {
		up, err := updaterForStructure(one.to, new.RootDir, rollbackDir, observer)
		if err != nil {
			return fmt.Errorf("cannot prepare update for volume structure %v: %v", one.to, err)
		}
		updaters[i] = up
	}

	var backupErr error
	for i, one := range updaters {
		if err := one.Backup(); err != nil {
			backupErr = fmt.Errorf("cannot backup volume structure %v: %v", updates[i].to, err)
			break
		}
	}
	if backupErr != nil {
		if observer != nil {
			if err := observer.Canceled(); err != nil {
				logger.Noticef("cannot observe canceled prepare update: %v", err)
			}
		}
		return backupErr
	}
	if observer != nil {
		if err := observer.BeforeWrite(); err != nil {
			return fmt.Errorf("cannot observe prepared update: %v", err)
		}
	}

	var updateErr error
	var updateLastAttempted int
	var skipped int
	for i, one := range updaters {
		updateLastAttempted = i
		if err := one.Update(); err != nil {
			if err == ErrNoUpdate {
				skipped++
				continue
			}
			updateErr = fmt.Errorf("cannot update volume structure %v: %v", updates[i].to, err)
			break
		}
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
		if err := one.Rollback(); err != nil {
			// TODO: log errors to oplog
			logger.Noticef("cannot rollback volume structure %v update: %v", updates[i].to, err)
		}
	}

	if observer != nil {
		if err := observer.Canceled(); err != nil {
			logger.Noticef("cannot observe canceled update: %v", err)
		}
	}

	return updateErr
}

var updaterForStructure = updaterForStructureImpl

func updaterForStructureImpl(ps *LaidOutStructure, newRootDir, rollbackDir string, observer ContentUpdateObserver) (Updater, error) {
	var updater Updater
	var err error
	if !ps.HasFilesystem() {
		updater, err = newRawStructureUpdater(newRootDir, ps, rollbackDir, findDeviceForStructureWithFallback)
	} else {
		updater, err = newMountedFilesystemUpdater(ps, rollbackDir, findMountPointForStructure, observer)
	}
	return updater, err
}

// MockUpdaterForStructure replace internal call with a mocked one, for use in tests only
func MockUpdaterForStructure(mock func(ps *LaidOutStructure, rootDir, rollbackDir string, observer ContentUpdateObserver) (Updater, error)) (restore func()) {
	old := updaterForStructure
	updaterForStructure = mock
	return func() {
		updaterForStructure = old
	}
}
