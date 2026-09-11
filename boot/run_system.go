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
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
)

const BootedRunSystemMarker = "booted-run-seed"

type RunSystemState struct {
	Current string
	Try     string
}

func runSystemBootloader() (bootloader.Bootloader, error) {
	return bootloader.Find("", &bootloader.Options{Role: bootloader.RoleRunMode})
}

func CurrentRunSystem() (*RunSystemState, error) {
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

func BootedRunSystem() (string, error) {
	marker := filepath.Join(dirs.SnapRunDir, BootedRunSystemMarker)
	data, err := os.ReadFile(marker)
	if err != nil {
		return "", err
	}
	label := strings.TrimSpace(string(data))
	if err := asserts.IsValidSystemLabel(label); err != nil {
		return "", err
	}
	return label, nil
}

func PromoteTriedRunSystem(systemLabel string) error {
	if err := asserts.IsValidSystemLabel(systemLabel); err != nil {
		return err
	}
	booted, err := BootedRunSystem()
	if err != nil {
		return fmt.Errorf("cannot identify booted run system: %v", err)
	}
	if booted != systemLabel {
		return fmt.Errorf("cannot promote run system %q while booted into %q", systemLabel, booted)
	}
	bl, err := runSystemBootloader()
	if err != nil {
		return err
	}
	return bl.SetBootVars(map[string]string{
		"run_system":     systemLabel,
		"try_run_system": "",
	})
}

func ClearTryRunSystem(systemLabel string) error {
	if systemLabel != "" {
		if err := asserts.IsValidSystemLabel(systemLabel); err != nil {
			return err
		}
	}
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
