// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package udev

import (
	"fmt"
	"os/exec"
)

// ReloadRules runs two commands that reload udev rule database.
//
// The commands are: udevadm control --reload-rules
//                   udevadm trigger
func ReloadRules() error {
	output, err := exec.Command("udevadm", "control", "--reload-rules").CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot reload udev rules: %s\nudev output:\n%s", err, string(output))
	}
	output, err = exec.Command("udevadm", "trigger").CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot run udev triggers: %s\nudev output:\n%s", err, string(output))
	}
	return nil
}
