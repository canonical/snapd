// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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
//                   udevadm trigger --subsystem-nomatch=input
//                   udevadm settle --timeout=3
// and optionally trigger other subsystems as defined in the interfaces. Eg:
//                   udevadm trigger --subsystem-match=input
//                   udevadm trigger --property-match=ID_INPUT_JOYSTICK=1
func ReloadRules(subsystemTriggers []string) error {
	output, err := exec.Command("udevadm", "control", "--reload-rules").CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot reload udev rules: %s\nudev output:\n%s", err, string(output))
	}

	// By default, trigger for all events except the input subsystem since
	// it can cause noticeable blocked input on, for example, classic
	// desktop.
	output, err = exec.Command("udevadm", "trigger", "--subsystem-nomatch=input").CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot run udev triggers: %s\nudev output:\n%s", err, string(output))
	}

	// FIXME: track if also should trigger the joystick property if it
	// wasn't already since we are not able to detect interfaces that are
	// removed and set subsystemTriggers correctly. When we can, remove
	// this. Allows joysticks to be removed from the device cgroup on
	// interface disconnect.
	inputJoystickTriggered := false

	for _, subsystem := range subsystemTriggers {
		if subsystem == "input/joystick" {
			// If one of the interfaces said it uses the input
			// subsystem for joysticks, then trigger the joystick
			// events in a way that is specific to joysticks to not
			// block other inputs.
			output, err = exec.Command("udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1").CombinedOutput()
			if err != nil {
				return fmt.Errorf("cannot run udev triggers for joysticks: %s\nudev output:\n%s", err, string(output))
			}
			inputJoystickTriggered = true
		} else if subsystem != "" {
			// If one of the interfaces said it uses a subsystem,
			// then do it too.
			output, err = exec.Command("udevadm", "trigger", "--subsystem-match="+subsystem).CombinedOutput()
			if err != nil {
				return fmt.Errorf("cannot run udev triggers for %s subsystem: %s\nudev output:\n%s", subsystem, err, string(output))
			}

			if subsystem == "input" {
				inputJoystickTriggered = true
			}
		}
	}

	// FIXME: if not already triggered, trigger the joystick property if it
	// wasn't already since we are not able to detect interfaces that are
	// removed and set subsystemTriggers correctly. When we can, remove
	// this. Allows joysticks to be removed from the device cgroup on
	// interface disconnect.
	if !inputJoystickTriggered {
		output, err = exec.Command("udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1").CombinedOutput()
		if err != nil {
			return fmt.Errorf("cannot run udev triggers for joysticks: %s\nudev output:\n%s", err, string(output))
		}
	}

	// give our triggered events a chance to be handled before exiting.
	// Ignore errors since we don't want to error on still pending events.
	_ = exec.Command("udevadm", "settle", "--timeout=10").Run()

	return nil
}
