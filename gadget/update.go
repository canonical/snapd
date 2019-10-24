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

	"github.com/snapcore/snapd/logger"
)

var (
	ErrNoUpdate = errors.New("nothing to update")
)

var (
	// default positioning constraints that match ubuntu-image
	defaultConstraints = LayoutConstraints{
		NonMBRStartOffset: 1 * SizeMiB,
		SectorSize:        512,
	}
)

// GadgetData holds references to a gadget revision metadata and its data directory.
type GadgetData struct {
	// Info is the gadget metadata
	Info *Info
	// RootDir is the root directory of gadget snap data
	RootDir string
}

// UpdatePolicyFunc is a callback that evaluates the provided pair of structures
// and returns true when the pair should be part of an update.
type UpdatePolicyFunc func(from, to *LaidOutStructure) bool

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
func Update(old, new GadgetData, rollbackDirPath string, updatePolicy UpdatePolicyFunc) error {
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

	// layout old partially, without going deep into the layout of structure
	// content
	pOld, err := LayoutVolumePartially(oldVol, defaultConstraints)
	if err != nil {
		return fmt.Errorf("cannot lay out the old volume: %v", err)
	}

	// layout new
	pNew, err := LayoutVolume(new.RootDir, newVol, defaultConstraints)
	if err != nil {
		return fmt.Errorf("cannot lay out the new volume: %v", err)
	}

	if err := canUpdateVolume(pOld, pNew); err != nil {
		return fmt.Errorf("cannot apply update to volume: %v", err)
	}

	if updatePolicy == nil {
		updatePolicy = defaultPolicy
	}
	// now we know which structure is which, find which ones need an update
	updates, err := resolveUpdate(pOld, pNew, updatePolicy)
	if err != nil {
		return err
	}
	if len(updates) == 0 {
		// nothing to update
		return ErrNoUpdate
	}

	// can update old layout to new layout
	for _, update := range updates {
		if err := canUpdateStructure(update.from, update.to, pNew.EffectiveSchema()); err != nil {
			return fmt.Errorf("cannot update volume structure %v: %v", update.to, err)
		}
	}

	return applyUpdates(new, updates, rollbackDirPath)
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

	return &oldV, &newV, nil
}

func isSameOffset(one *Size, two *Size) bool {
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
	return from.Type == MBR && to.EffectiveRole() == MBR
}

func canUpdateStructure(from *LaidOutStructure, to *LaidOutStructure, schema string) error {
	if schema == GPT && from.Name != to.Name {
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
	if from.EffectiveRole() != to.EffectiveRole() {
		return fmt.Errorf("cannot change structure role from %q to %q", from.EffectiveRole(), to.EffectiveRole())
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
		if from.EffectiveFilesystemLabel() != to.EffectiveFilesystemLabel() {
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
	if from.EffectiveSchema() != to.EffectiveSchema() {
		return fmt.Errorf("cannot change volume schema from %q to %q", from.EffectiveSchema(), to.EffectiveSchema())
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

func defaultPolicy(from, to *LaidOutStructure) bool {
	return to.Update.Edition > from.Update.Edition
}

// RemodelUpdatePolicy implements the update policy of a remodel scenario. The
// policy selects all non-MBR structures for the update.
func RemodelUpdatePolicy(from, _ *LaidOutStructure) bool {
	if from.EffectiveRole() == MBR {
		return false
	}
	return true
}

func resolveUpdate(oldVol *PartiallyLaidOutVolume, newVol *LaidOutVolume, policy UpdatePolicyFunc) (updates []updatePair, err error) {
	if len(oldVol.LaidOutStructure) != len(newVol.LaidOutStructure) {
		return nil, errors.New("internal error: the number of structures in new and old volume definitions is different")
	}
	for j, oldStruct := range oldVol.LaidOutStructure {
		newStruct := newVol.LaidOutStructure[j]
		// update only when new edition is higher than the old one; boot
		// assets are assumed to be backwards compatible, once deployed
		// are not rolled back or replaced unless a higher edition is
		// available
		if policy(&oldStruct, &newStruct) {
			updates = append(updates, updatePair{
				from: &oldVol.LaidOutStructure[j],
				to:   &newVol.LaidOutStructure[j],
			})
		}
	}
	return updates, nil
}

type Updater interface {
	// Update applies the update or errors out on failures
	Update() error
	// Backup prepares a backup copy of data that will be modified by
	// Update()
	Backup() error
	// Rollback restores data modified by update
	Rollback() error
}

func applyUpdates(new GadgetData, updates []updatePair, rollbackDir string) error {
	updaters := make([]Updater, len(updates))

	for i, one := range updates {
		up, err := updaterForStructure(one.to, new.RootDir, rollbackDir)
		if err != nil {
			return fmt.Errorf("cannot prepare update for volume structure %v: %v", one.to, err)
		}
		updaters[i] = up
	}

	for i, one := range updaters {
		if err := one.Backup(); err != nil {
			return fmt.Errorf("cannot backup volume structure %v: %v", updates[i].to, err)
		}
	}

	var updateErr error
	var updateLastAttempted int
	for i, one := range updaters {
		updateLastAttempted = i
		if err := one.Update(); err != nil {
			updateErr = fmt.Errorf("cannot update volume structure %v: %v", updates[i].to, err)
			break
		}
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

	return updateErr
}

var updaterForStructure = updaterForStructureImpl

func updaterForStructureImpl(ps *LaidOutStructure, newRootDir, rollbackDir string) (Updater, error) {
	var updater Updater
	var err error
	if !ps.HasFilesystem() {
		updater, err = NewRawStructureUpdater(newRootDir, ps, rollbackDir, FindDeviceForStructureWithFallback)
	} else {
		updater, err = NewMountedFilesystemUpdater(newRootDir, ps, rollbackDir, FindMountPointForStructure)
	}
	return updater, err
}

// MockUpdaterForStructure replace internal call with a mocked one, for use in tests only
func MockUpdaterForStructure(mock func(ps *LaidOutStructure, rootDir, rollbackDir string) (Updater, error)) (restore func()) {
	old := updaterForStructure
	updaterForStructure = mock
	return func() {
		updaterForStructure = old
	}
}
