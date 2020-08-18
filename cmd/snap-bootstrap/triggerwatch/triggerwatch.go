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

package triggerwatch

import (
	"errors"
	"fmt"
	"time"

	"github.com/snapcore/snapd/logger"
)

type triggerProvider interface {
	FindMatchingDevices(filter triggerEventFilter) ([]triggerDevice, error)
}

type triggerDevice interface {
	WaitForTrigger(chan keyEvent)
	String() string
	Close()
}

var (
	// trigger mechanism
	trigger triggerProvider

	// wait for '1' to be pressed
	triggerFilter = triggerEventFilter{Key: "KEY_1"}

	ErrTriggerNotDetected     = errors.New("trigger not detected")
	ErrNoMatchingInputDevices = errors.New("no matching input devices")
)

// Wait waits for a trigger on the available trigger devices for a given amount
// of time. Returns nil if one was detected, ErrTriggerNotDetected if timeout
// was hit, or other non-nil error.
func Wait(timeout time.Duration) error {
	if trigger == nil {
		logger.Panicf("trigger is unset")
	}

	devices, err := trigger.FindMatchingDevices(triggerFilter)
	if err != nil {
		return fmt.Errorf("cannot list trigger devices: %v", err)
	}
	if devices == nil {
		return ErrNoMatchingInputDevices
	}

	logger.Noticef("waiting for trigger key: %v", triggerFilter.Key)

	detectKeyCh := make(chan keyEvent, len(devices))
	for _, dev := range devices {
		go dev.WaitForTrigger(detectKeyCh)
		defer dev.Close()
	}

	select {
	case kev := <-detectKeyCh:
		if kev.Err != nil {
			return err
		}
		// channel got closed without an error
		logger.Noticef("%s: + got trigger key %v", kev.Dev, triggerFilter.Key)
	case <-time.After(timeout):
		return ErrTriggerNotDetected
	}

	return nil
}
