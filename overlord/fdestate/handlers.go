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

	"github.com/snapcore/snapd/overlord/state"
	"gopkg.in/tomb.v2"
)

func (m *FDEManager) doAddRecoveryKeys(t *state.Task, _ *tomb.Tomb) error {
	// TODO:FDEM: implement recovery key addition, this is currently only a
	// mock task for testing.

	// TODO:FDEM:
	//   - this might be a re-run, make task idempotent to be reselient to
	//     abrupt reboot/shutdown.
	//   - important to detect absence of recovery key ID and do cleanup
	//     of added key slots on re-run and returning an error.
	//   - distinguish between errors (undo) and pure-reboots (re-run).
	//   - conflict detection for key slot tasks is important because it
	//     reduces the possible states we could end up in.
	return nil
}

func (m *FDEManager) doRemoveKeys(t *state.Task, _ *tomb.Tomb) error {
	// TODO:FDEM: implement recovery key removal, this is currently only a
	// mock task for testing.

	// TODO:FDEM:
	//   - this might be a re-run, make task idempotent to be reselient to
	//     abrupt reboot/shutdown.
	//   - distinguish between errors (undo) and pure-reboots (re-run).
	//   - conflict detection for key slot tasks is important because it
	//     reduces the possible states we could end up in.
	return nil
}

func (m *FDEManager) doRenameKeys(t *state.Task, _ *tomb.Tomb) error {
	// TODO:FDEM: implement recovery key renaming, this is currently only a
	// mock task for testing.

	// TODO:FDEM:
	//   - this might be a re-run, make task idempotent to be reselient to
	//     abrupt reboot/shutdown.
	//   - distinguish between errors (undo) and pure-reboots (re-run).
	//   - conflict detection for key slot tasks is important because it
	//     reduces the possible states we could end up in.
	return nil
}

func getCachedChangeAuthOptionsOnce(st *state.State) (*changeAuthOptions, error) {
	cached := st.Cached(changeAuthOptionsKey{})
	if cached == nil {
		return nil, errors.New("no entry found in cache")
	}
	st.Cache(changeAuthOptionsKey{}, nil)
	var ok bool
	opts, ok := cached.(*changeAuthOptions)
	if !ok {
		return nil, fmt.Errorf("internal error: wrong data type under changeAuthOptionsKey")
	}
	return opts, nil
}

func (m *FDEManager) doChangePassphrase(t *state.Task, _ *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	var keyslotRefs []KeyslotRef
	if err := t.Get("keyslots", &keyslotRefs); err != nil {
		return err
	}

	opts, err := getCachedChangeAuthOptionsOnce(m.state)
	if err != nil {
		return fmt.Errorf("failed to find cached authentication options: %v", err)
	}

	// XXX: unlock state and let conflict detection handle the rest?

	currentKeyslots, missing, err := m.GetKeyslots(keyslotRefs)
	if err != nil {
		return fmt.Errorf("failed to find key slots: %v", err)
	}
	if len(missing) != 0 {
		return &KeyslotRefsNotFoundError{KeyslotRefs: missing}
	}

	for _, keyslot := range currentKeyslots {
		kd, err := keyslot.KeyData()
		if err != nil {
			return fmt.Errorf("failed to read key data for %s: %v", keyslot.Ref().String(), err)
		}

		if err := kd.ChangePassphrase(opts.oldPassphrase, opts.newPassphrase); err != nil {
			return fmt.Errorf("failed to change passphrase for %s: %v", keyslot.Ref().String(), err)
		}
		if err := kd.WriteTokenAtomic(keyslot.devPath, keyslot.Name); err != nil {
			return fmt.Errorf("failed to write key data for %s: %v", keyslot.Ref().String(), err)
		}
	}

	return nil
}
