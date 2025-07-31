// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
package fdestate

import (
	"errors"
	"fmt"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
)

var (
	secbootAddContainerRecoveryKey = secboot.AddContainerRecoveryKey
	secbootDeleteContainerKey      = secboot.DeleteContainerKey
	secbootRenameContainerKey      = secboot.RenameContainerKey
)

func (m *FDEManager) doAddRecoveryKeys(t *state.Task, tomb *tomb.Tomb) (err error) {
	m.state.Lock()
	defer m.state.Unlock()

	var keyslotRefs []KeyslotRef
	if err := t.Get("keyslots", &keyslotRefs); err != nil {
		return err
	}

	var recoveryKeyID string
	if err := t.Get("recovery-key-id", &recoveryKeyID); err != nil {
		return err
	}

	// XXX: unlock state and let conflict detection handle the rest?

	containers, err := m.GetEncryptedContainers()
	if err != nil {
		return err
	}
	containerDevicePath := make(map[string]string, len(containers))
	for _, container := range containers {
		containerDevicePath[container.ContainerRole()] = container.DevPath()
	}

	// IMPORTANT: this clean up must be declared as early as possible
	// to account for real errors and potential re-runs (that will fail
	// anyway as soon as the recovery key ID is reused).
	var addedKeyslots []KeyslotRef
	defer func() {
		if err == nil {
			return
		}
		// TODO:FDEM: a dedicated clean up for stray tmp key slots (recovery or not)
		// is needed to account for left-over tmp key slot from a failed re-run for
		// example.
		for _, keyslotRef := range addedKeyslots {
			devicePath := containerDevicePath[keyslotRef.ContainerRole]
			if err := secbootDeleteContainerKey(devicePath, keyslotRef.Name); err != nil {
				// best effort deletion, log errors only
				logger.Noticef("cannot delete %s during clean up: %v", keyslotRef.String(), err)
			}
		}
	}()

	_, missingRefs, err := m.GetKeyslots(keyslotRefs)
	if err != nil {
		return fmt.Errorf("cannot get key slots: %v", err)
	}
	if len(missingRefs) == 0 {
		// this could be re-run and all key slots were already added, do nothing
		return nil
	}

	rkeyInfo, err := m.recoveryKeyCache.Key(recoveryKeyID)
	if err != nil {
		return fmt.Errorf("cannot find recovery key with id %q: %v", recoveryKeyID, err)
	}
	if rkeyInfo.Expired(time.Now()) {
		return errors.New("recovery key has expired")
	}

	// we only care about missing key slots because this might be
	// a re-run due a force reboot or abrupt shutdown, so we want
	// to continue adding the remaining key slots.
	for _, ref := range missingRefs {
		devicePath := containerDevicePath[ref.ContainerRole]
		if err := secbootAddContainerRecoveryKey(devicePath, ref.Name, rkeyInfo.Key); err != nil {
			return fmt.Errorf("cannot add recovery key slot %s: %v", ref.String(), err)
		}

		addedKeyslots = append(addedKeyslots, ref)
	}
	// avoid re-runs in case of abrupt shutdown since all key slots are now added.
	t.SetStatus(state.DoneStatus)
	m.recoveryKeyCache.RemoveKey(recoveryKeyID)

	return nil
}

func (m *FDEManager) doRemoveKeys(t *state.Task, tomb *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	var keyslotRefs []KeyslotRef
	if err := t.Get("keyslots", &keyslotRefs); err != nil {
		return err
	}

	// XXX: unlock state and let conflict detection handle the rest?

	// we only care about current key slots because this might be
	// a re-run due a force reboot or abrupt shutdown, so we want
	// to continue deleting the remaining key slots.
	currentKeyslots, _, err := m.GetKeyslots(keyslotRefs)
	if err != nil {
		return fmt.Errorf("cannot get key slots: %v", err)
	}

	for _, keyslot := range currentKeyslots {
		// TODO:FDEM: do not permit deleting the last key slot associated
		// with a role that includes “recover” mode from any container.
		if err := secbootDeleteContainerKey(keyslot.devPath, keyslot.Name); err != nil {
			return fmt.Errorf("cannot remove key slot %s: %v", keyslot.Ref().String(), err)
		}
	}
	// avoid re-runs in case of abrupt shutdown since all key slots are now removed.
	t.SetStatus(state.DoneStatus)

	// XXX: request reboot to account for the case where the unlock key
	// in the kernel keyring is one of the deleted key slots?
	return nil
}

func (m *FDEManager) doRenameKeys(t *state.Task, tomb *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	var keyslotRefs []KeyslotRef
	if err := t.Get("keyslots", &keyslotRefs); err != nil {
		return err
	}

	var renames map[string]string
	if err := t.Get("renames", &renames); err != nil {
		return err
	}

	for _, keyslotRef := range keyslotRefs {
		if _, ok := renames[keyslotRef.String()]; !ok {
			return fmt.Errorf("internal error: cannot find mapping for %s", keyslotRef.String())
		}
	}

	// XXX: unlock state and let conflict detection handle the rest?

	// we only care about current key slots because this might be
	// a re-run due a force reboot or abrupt shutdown, so we want
	// to continue renaming the remaining key slots.
	currentKeyslots, _, err := m.GetKeyslots(keyslotRefs)
	if err != nil {
		return fmt.Errorf("cannot get key slots: %v", err)
	}

	// check that all remaining renames do not already exist to
	// prevent failing midway when doing the actual renaming below.
	if len(currentKeyslots) != 0 {
		var renamedKeyslotRefs []KeyslotRef
		for _, keyslot := range currentKeyslots {
			refKey := keyslot.Ref().String()
			renamedRef := KeyslotRef{ContainerRole: keyslot.ContainerRole, Name: renames[refKey]}
			renamedKeyslotRefs = append(renamedKeyslotRefs, renamedRef)
		}
		currentRenamedKeyslots, _, err := m.GetKeyslots(renamedKeyslotRefs)
		if err != nil {
			return fmt.Errorf("cannot get key slots: %v", err)
		}
		if len(currentRenamedKeyslots) != 0 {
			return &keyslotsAlreadyExistsError{keyslots: currentRenamedKeyslots}
		}
	}

	for _, keyslot := range currentKeyslots {
		refKey := keyslot.Ref().String()
		if err := secbootRenameContainerKey(keyslot.devPath, keyslot.Name, renames[refKey]); err != nil {
			return fmt.Errorf("cannot rename key slot %s to %q: %v", keyslot.Ref().String(), renames[refKey], err)
		}
	}
	// avoid re-runs in case of abrupt shutdown since all key slots are now renamed.
	t.SetStatus(state.DoneStatus)

	return nil
}

func (m *FDEManager) doAddProtectedKeys(t *state.Task, _ *tomb.Tomb) (err error) {
	// TODO:FDEM: implement handler
	return nil
}

func (m *FDEManager) doChangeAuth(t *state.Task, _ *tomb.Tomb) (err error) {
	m.state.Lock()
	defer m.state.Unlock()

	var keyslotRefs []KeyslotRef
	if err := t.Get("keyslots", &keyslotRefs); err != nil {
		return err
	}

	var authMode device.AuthMode
	if err := t.Get("auth-mode", &authMode); err != nil {
		return err
	}

	cached := m.state.Cached(changeAuthOptionsKey{})
	if cached == nil {
		return errors.New("cannot find authentication options in memory: unexpected snapd restart")
	}
	var ok bool
	opts, ok := cached.(*changeAuthOptions)
	if !ok {
		return fmt.Errorf("internal error: wrong data type under changeAuthOptionsKey: %T", cached)
	}

	if opts.old == opts.new {
		// optimally, this check should be done in ChangeAuth before the change
		// is created but it is done here to avoid breaking the API, as on success
		// it expects an async response with a change ID, and if we have an empty
		// change, it stays at "Hold" status forever, so it is more for convenience.
		return nil
	}

	changeOneKeyslot := func(keyslot Keyslot, old, new string) error {
		kd, err := keyslot.KeyData()
		if err != nil {
			return fmt.Errorf("cannot read key data for %s: %v", keyslot.Ref().String(), err)
		}

		switch authMode {
		case device.AuthModePassphrase:
			if err := kd.ChangePassphrase(old, new); err != nil {
				return fmt.Errorf("cannot change passphrase for %s: %v", keyslot.Ref().String(), err)
			}
		case device.AuthModePIN:
			return fmt.Errorf("internal error: changing PINs is not implemented")
		default:
			return fmt.Errorf("internal error: unexpected auth-mode %q", authMode)
		}

		if err := kd.WriteTokenAtomic(keyslot.devPath, keyslot.Name); err != nil {
			return fmt.Errorf("cannot write key data for %s: %v", keyslot.Ref().String(), err)
		}

		return nil
	}

	var changedKeyslots []KeyslotRef
	defer func() {
		if err == nil || len(changedKeyslots) == 0 {
			return
		}
		keyslots, _, err := m.GetKeyslots(changedKeyslots)
		if err != nil {
			logger.Noticef("cannot get key slots for cleanup: %v", err)
			return
		}
		for _, keyslot := range keyslots {
			// best effort cleanup, log errors only
			if err := changeOneKeyslot(keyslot, opts.new, opts.old); err != nil {
				logger.Noticef("cannot cleanup: %v", err)
			}
		}
	}()

	// XXX: unlock state and let conflict detection handle the rest?

	currentKeyslots, missing, err := m.GetKeyslots(keyslotRefs)
	if err != nil {
		return fmt.Errorf("cannot get key slots: %v", err)
	}
	if len(missing) != 0 {
		return &KeyslotRefsNotFoundError{KeyslotRefs: missing}
	}

	for _, keyslot := range currentKeyslots {
		if err := changeOneKeyslot(keyslot, opts.old, opts.new); err != nil {
			return err
		}

		changedKeyslots = append(changedKeyslots, keyslot.Ref())
	}
	// avoid re-runs in case of abrupt shutdown since all key slots are now updated.
	t.SetStatus(state.DoneStatus)
	m.state.Cache(changeAuthOptionsKey{}, nil)

	return nil
}
