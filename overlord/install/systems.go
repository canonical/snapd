// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package install

import (
	"fmt"
	"os"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/seed"
)

func MaybeReadModeenv() (*boot.Modeenv, error) {
	modeenv, err := boot.ReadModeenv("")
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot read modeenv: %v", err)
	}
	return modeenv, nil
}

type SystemAction struct {
	Title string
	Mode  string
}

type System struct {
	// Current is true when the system running now was installed from that
	// seed
	Current bool
	// Label of the seed system
	Label string
	// Model assertion of the system
	Model *asserts.Model
	// Brand information
	Brand *asserts.Account
	// Actions available for this system
	Actions []SystemAction
}

var defaultSystemActions = []SystemAction{
	{Title: "Install", Mode: "install"},
}
var currentSystemActions = []SystemAction{
	{Title: "Reinstall", Mode: "install"},
	{Title: "Recover", Mode: "recover"},
	{Title: "Factory reset", Mode: "factory-reset"},
	{Title: "Run normally", Mode: "run"},
}
var recoverSystemActions = []SystemAction{
	{Title: "Reinstall", Mode: "install"},
	{Title: "Factory reset", Mode: "factory-reset"},
	{Title: "Run normally", Mode: "run"},
}

type SeededSystem struct {
	// System carries the recovery system label that was used to seed the
	// current system
	System string `json:"system"`
	Model  string `json:"model"`
	// BrandID is the brand account ID
	BrandID string `json:"brand-id"`
	// Revision of the model assertion
	Revision int `json:"revision"`
	// Timestamp of model assertion
	Timestamp time.Time `json:"timestamp"`
	// SeedTime holds the timestamp when the system was seeded
	SeedTime time.Time `json:"seed-time"`
}

func (s *SeededSystem) SameAs(other *SeededSystem) bool {
	// in theory the system labels are unique, however be extra paranoid and
	// check all model related fields too
	return s.System == other.System &&
		s.Model == other.Model &&
		s.BrandID == other.BrandID &&
		s.Revision == other.Revision
}

func SystemFromSeed(label string, current *CurrentSystem) (*System, error) {
	_, sys, err := LoadSeedAndSystem(label, current)
	return sys, err
}

func LoadSeedAndSystem(label string, current *CurrentSystem) (seed.Seed, *System, error) {
	s, err := seedOpen(dirs.SnapSeedDir, label)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open: %v", err)
	}
	if err := s.LoadAssertions(nil, nil); err != nil {
		return nil, nil, fmt.Errorf("cannot load assertions for label %q: %v", label, err)
	}
	// get the model
	model := s.Model()
	brand, err := s.Brand()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot obtain brand: %v", err)
	}
	system := &System{
		Current: false,
		Label:   label,
		Model:   model,
		Brand:   brand,
		Actions: defaultSystemActions,
	}
	if current.SameAs(system) {
		system.Current = true
		system.Actions = current.actions
	}
	return s, system, nil
}

type CurrentSystem struct {
	*SeededSystem
	actions []SystemAction
}

func (c *CurrentSystem) SameAs(other *System) bool {
	return c != nil &&
		c.System == other.Label &&
		c.Model == other.Model.Model() &&
		c.BrandID == other.Brand.AccountID()
}

func CurrentSystemForMode(st *state.State, mode string) (*CurrentSystem, error) {
	var system *SeededSystem
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
	currentSys := &CurrentSystem{
		SeededSystem: system,
		actions:      actions,
	}
	return currentSys, nil
}

func currentSeededSystem(st *state.State) (*SeededSystem, error) {
	st.Lock()
	defer st.Unlock()

	var whatseeded []SeededSystem
	if err := st.Get("seeded-systems", &whatseeded); err != nil {
		return nil, err
	}
	if len(whatseeded) == 0 {
		// unexpected
		return nil, state.ErrNoState
	}
	// seeded systems are prepended to the list, so the most recently seeded
	// one comes first
	return &whatseeded[0], nil
}

func seededSystemFromModeenv() (*SeededSystem, error) {
	modeEnv, err := MaybeReadModeenv()
	if err != nil {
		return nil, err
	}
	if modeEnv == nil {
		return nil, fmt.Errorf("internal error: modeenv does not exist")
	}
	if modeEnv.RecoverySystem == "" {
		return nil, fmt.Errorf("internal error: recovery system is unset")
	}

	system, err := SystemFromSeed(modeEnv.RecoverySystem, nil)
	if err != nil {
		return nil, err
	}
	seededSys := &SeededSystem{
		System:    modeEnv.RecoverySystem,
		Model:     system.Model.Model(),
		BrandID:   system.Model.BrandID(),
		Revision:  system.Model.Revision(),
		Timestamp: system.Model.Timestamp(),
		// SeedTime is intentionally left unset
	}
	return seededSys, nil
}
