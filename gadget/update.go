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

	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
)

var (
	ErrNoUpdate = errors.New("nothing to update")
)

var (
	// default positioning constraints that match ubuntu-image
	defaultConstraints = LayoutConstraints{
		NonMBRStartOffset: 1 * quantity.OffsetMiB,
		SectorSize:        512,
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

// UpdatePolicyFunc is a callback that evaluates the provided pair of structures
// and returns true when the pair should be part of an update.
type UpdatePolicyFunc func(from, to *LaidOutStructure) bool

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
func Update(old, new GadgetData, rollbackDirPath string, updatePolicy UpdatePolicyFunc, observer ContentUpdateObserver) error {
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
	pOld, err := LayoutVolumePartially(oldVol, defaultConstraints)
	if err != nil {
		return fmt.Errorf("cannot lay out the old volume: %v", err)
	}

	// layout new
	pNew, err := LayoutVolume(new.RootDir, new.KernelRootDir, newVol, defaultConstraints)
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

func defaultPolicy(from, to *LaidOutStructure) bool {
	return to.Update.Edition > from.Update.Edition
}

// RemodelUpdatePolicy implements the update policy of a remodel scenario. The
// policy selects all non-MBR structures for the update.
func RemodelUpdatePolicy(from, _ *LaidOutStructure) bool {
	if from.Role == schemaMBR {
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
