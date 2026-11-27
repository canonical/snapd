// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
 */

package boot

import (
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
)

type RunSystemState struct {
	Current string
	Try     string
}

func runSystemBootloader() (bootloader.Bootloader, error) {
	return bootloader.Find("", &bootloader.Options{Role: bootloader.RoleRunMode})
}

func CurrentRunSystem() (*RunSystemState, error) {
	modeenvLock()
	defer modeenvUnlock()

	bl, err := runSystemBootloader()
	if err != nil {
		return nil, err
	}

	vars, err := bl.GetBootVars("run_system", "try_run_system")
	if err != nil {
		return nil, err
	}
	return &RunSystemState{Current: vars["run_system"], Try: vars["try_run_system"]}, nil
}

func SetTryRunSystem(systemLabel string) error {
	if err := asserts.IsValidSystemLabel(systemLabel); err != nil {
		return err
	}

	modeenvLock()
	defer modeenvUnlock()

	bl, err := runSystemBootloader()
	if err != nil {
		return err
	}

	vars, err := bl.GetBootVars("run_system", "try_run_system")
	if err != nil {
		return err
	}

	if currentTry := vars["try_run_system"]; currentTry != "" && currentTry != systemLabel {
		return fmt.Errorf("cannot try run system %q while %q is pending", systemLabel, currentTry)
	}

	if vars["run_system"] == systemLabel {
		return bl.SetBootVars(map[string]string{"try_run_system": ""})
	}
	return bl.SetBootVars(map[string]string{"try_run_system": systemLabel})
}

func ClearTryRunSystem(systemLabel string) error {
	if systemLabel != "" {
		if err := asserts.IsValidSystemLabel(systemLabel); err != nil {
			return err
		}
	}

	modeenvLock()
	defer modeenvUnlock()

	bl, err := runSystemBootloader()
	if err != nil {
		return err
	}

	vars, err := bl.GetBootVars("try_run_system")
	if err != nil {
		return err
	}

	if systemLabel != "" && vars["try_run_system"] != "" && vars["try_run_system"] != systemLabel {
		return fmt.Errorf("cannot clear run system %q while %q is pending", systemLabel, vars["try_run_system"])
	}
	return bl.SetBootVars(map[string]string{"try_run_system": ""})
}
