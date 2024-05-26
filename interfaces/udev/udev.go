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

	"github.com/ddkwork/golibrary/mylog"
)

// udevadmTrigger runs "udevadm trigger" but ignores an non-zero exit codes.
// udevadm only started reporting errors in systemd 248 and in order to
// work correctly in LXD these errors need to be ignored. See
// https://github.com/systemd/systemd/pull/18684 for some more background
// (and https://github.com/lxc/lxd/issues/9526)
func udevadmTrigger(args ...string) error {
	args = append([]string{"trigger"}, args...)
	output := mylog.Check2(exec.Command("udevadm", args...).CombinedOutput())
	// can happen when events for some of the devices or all of
	// them could not be triggered, but we cannot distinguish which of
	// those happened, in any case snapd invoked udevadm and tried its
	// best
	if exitErr, ok := err.(*exec.ExitError); ok {
		// ignore "normal" exit codes but report e.g. segfaults
		// that are reported as -1
		if exitErr.ExitCode() > 0 {
			return nil
		}
	}

	return nil
}

// reloadRules runs three commands that reload udev rule database.
//
// The commands are: udevadm control --reload-rules
//
//	udevadm trigger --subsystem-nomatch=input
//	udevadm settle --timeout=3
//
// and optionally trigger other subsystems as defined in the interfaces. Eg:
//
//	udevadm trigger --subsystem-match=input
//	udevadm trigger --property-match=ID_INPUT_JOYSTICK=1
func (b *Backend) reloadRules(subsystemTriggers []string) error {
	if b.preseed {
		return nil
	}

	output := mylog.Check2(exec.Command("udevadm", "control", "--reload-rules").CombinedOutput())
	mylog.Check(

		// By default, trigger for all events except the input subsystem since
		// it can cause noticeable blocked input on, for example, classic
		// desktop.
		udevadmTrigger("--subsystem-nomatch=input"))

	mustTriggerForInputSubsystem := false
	mustTriggerForInputKeys := false

	for _, subsystem := range subsystemTriggers {
		if subsystem == "input/key" {
			mustTriggerForInputKeys = true
		} else if subsystem == "input" {
			mustTriggerForInputSubsystem = true
		}
		// no `else` branch: we already triggered udevadm for all other
		// subsystems before by running it with the `--subsystem-nomatch=input`
		// option, so there's no need to do anything here.
	}

	if mustTriggerForInputSubsystem {
		mylog.Check(
			// Trigger for the whole input subsystem
			udevadmTrigger("--subsystem-match=input"))
	} else {
		// More specific triggers, to avoid blocking keyboards and mice

		if mustTriggerForInputKeys {
			mylog.Check(
				// If one of the interfaces said it uses the input
				// subsystem for input keys, then trigger the keys
				// events in a way that is specific to input keys
				// to not block other inputs.
				udevadmTrigger("--property-match=ID_INPUT_KEY=1", "--property-match=ID_INPUT_KEYBOARD!=1"))
		}
		mylog.Check(
			// FIXME: if not already triggered, trigger the joystick property if it
			// wasn't already since we are not able to detect interfaces that are
			// removed and set subsystemTriggers correctly. When we can, remove
			// this. Allows joysticks to be removed from the device cgroup on
			// interface disconnect.
			udevadmTrigger("--property-match=ID_INPUT_JOYSTICK=1"))

	}

	// give our triggered events a chance to be handled before exiting.
	// Ignore errors since we don't want to error on still pending events.
	_ = exec.Command("udevadm", "settle", "--timeout=10").Run()

	return nil
}
