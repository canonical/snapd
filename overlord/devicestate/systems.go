// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package devicestate

import (
	"fmt"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/seed"
)

func checkSystemRequestConflict(st *state.State, systemLabel string) error {
	st.Lock()
	defer st.Unlock()

	var seeded bool
	if err := st.Get("seeded", &seeded); err != nil && err != state.ErrNoState {
		return err
	}
	if seeded {
		// the system is fully seeded already
		return nil
	}

	// inspect the current system which is stored in modeenv, note we are
	// holding the state lock so there is no race against mark-seeded
	// clearing recovery system; recovery system is not cleared when seeding
	// fails
	modeEnv, err := maybeReadModeenv()
	if err != nil {
		return err
	}
	if modeEnv == nil {
		// non UC20 systems do not support actions, no conflict can
		// happen
		return nil
	}

	// not yet fully seeded, hold off requests for the system that is being
	// seeded, but allow requests for other systems
	if modeEnv.RecoverySystem == systemLabel {
		return &snapstate.ChangeConflictError{
			ChangeKind: "seed",
			Message:    "cannot request system action, system is seeding",
		}
	}
	return nil
}

func systemFromSeed(label string, current *currentSystem) (*System, error) {
	s, err := seed.Open(dirs.SnapSeedDir, label)
	if err != nil {
		return nil, fmt.Errorf("cannot open: %v", err)
	}
	if err := s.LoadAssertions(nil, nil); err != nil {
		return nil, fmt.Errorf("cannot load assertions: %v", err)
	}
	// get the model
	model := s.Model()
	brand, err := s.Brand()
	if err != nil {
		return nil, fmt.Errorf("cannot obtain brand: %v", err)
	}
	system := &System{
		Current: false,
		Label:   label,
		Model:   model,
		Brand:   brand,
		Actions: defaultSystemActions,
	}
	if current.sameAs(system) {
		system.Current = true
		system.Actions = current.actions
	}
	return system, nil
}

type currentSystem struct {
	*seededSystem
	actions []SystemAction
}

func (c *currentSystem) sameAs(other *System) bool {
	return c != nil &&
		c.System == other.Label &&
		c.Model == other.Model.Model() &&
		c.BrandID == other.Brand.AccountID()
}

func currentSystemForMode(st *state.State, mode string) (*currentSystem, error) {
	var system *seededSystem
	var actions []SystemAction
	var err error

	switch mode {
	case "run":
		actions = currentSystemActions
		system, err = currentSeededSystem(st)
	case "install":
		// there is no current system for install mode
		return nil, nil
	case "recover":
		actions = recoverSystemActions
		// recover mode uses modeenv for reference
		system, err = seededSystemFromModeenv()
	default:
		return nil, fmt.Errorf("internal error: cannot identify current system for unsupported mode %q", mode)
	}
	if err != nil {
		return nil, err
	}
	currentSys := &currentSystem{
		seededSystem: system,
		actions:      actions,
	}
	return currentSys, nil
}

func currentSeededSystem(st *state.State) (*seededSystem, error) {
	st.Lock()
	defer st.Unlock()

	var whatseeded []seededSystem
	if err := st.Get("seeded-systems", &whatseeded); err != nil {
		return nil, err
	}
	if len(whatseeded) == 0 {
		// unexpected
		return nil, state.ErrNoState
	}
	return &whatseeded[0], nil
}

func seededSystemFromModeenv() (*seededSystem, error) {
	modeEnv, err := maybeReadModeenv()
	if err != nil {
		return nil, err
	}
	if modeEnv == nil {
		return nil, fmt.Errorf("internal error: modeenv does not exist")
	}
	if modeEnv.RecoverySystem == "" {
		return nil, fmt.Errorf("internal error: recovery system is unset")
	}

	system, err := systemFromSeed(modeEnv.RecoverySystem, nil)
	if err != nil {
		return nil, err
	}
	seededSys := &seededSystem{
		System:    modeEnv.RecoverySystem,
		Model:     system.Model.Model(),
		BrandID:   system.Model.BrandID(),
		Revision:  system.Model.Revision(),
		Timestamp: system.Model.Timestamp(),
		// SeedTime is intentionally left unset
	}
	return seededSys, nil
}
