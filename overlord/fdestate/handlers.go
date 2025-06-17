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
	"fmt"

	"gopkg.in/tomb.v2"

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

	// IMPORTANT: this clean up must be decalred as early as possible
	// to account for real errors and potential re-runs (that will fail
	// anyway as soon as the recovery key ID is reused).
	defer func() {
		if err == nil {
			return
		}
		for _, keyslotRef := range keyslotRefs {
			devicePath := containerDevicePath[keyslotRef.ContainerRole]
			if err := secbootDeleteContainerKey(devicePath, keyslotRef.Name); err != nil {
				// best effort deletion, log errors only
				logger.Noticef("failed to delete %s during clean up: %v", keyslotRef.String(), err)
			}
		}
	}()

	rkey, err := m.getRecoveryKey(recoveryKeyID)
	if err != nil {
		// most likely a re-run as keys expire after first use and
		// clean up is needed.
		return fmt.Errorf("failed to find recovery key with id %q: %v", recoveryKeyID, err)
	}

	currentKeyslots, _, err := m.GetKeyslots(keyslotRefs)
	if err != nil {
		return fmt.Errorf("failed to find key slots: %v", err)
	}
	if len(currentKeyslots) != 0 {
		return &keyslotsAlreadyExistsError{keyslots: currentKeyslots}
	}

	for _, keyslotRef := range keyslotRefs {
		devicePath := containerDevicePath[keyslotRef.ContainerRole]
		if err := secbootAddContainerRecoveryKey(devicePath, keyslotRef.Name, rkey); err != nil {
			return fmt.Errorf("failed to add recovery key slot %s: %v", keyslotRef.String(), err)
		}
	}

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
		return fmt.Errorf("failed to find key slots: %v", err)
	}

	for _, keyslot := range currentKeyslots {
		if err := secbootDeleteContainerKey(keyslot.devPath, keyslot.Name); err != nil {
			// XXX: keep going and report errors afterwards?
			return fmt.Errorf("failed to remove key slot %s: %v", keyslot.Ref().String(), err)
		}
	}

	// XXX: request reboot to acconut for the case where the unlock key
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
		return fmt.Errorf("failed to find key slots: %v", err)
	}

	for _, keyslot := range currentKeyslots {
		refKey := keyslot.Ref().String()
		if err := secbootRenameContainerKey(keyslot.devPath, keyslot.Name, renames[refKey]); err != nil {
			// XXX: keep going and report errors afterwards?
			return fmt.Errorf("failed to rename key slot %s to %q: %v", keyslot.Ref().String(), renames[refKey], err)
		}
	}

	return nil
}
