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
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/udev/netlink"
)

type triggerProvider interface {
	Open(filter triggerEventFilter, node string) (triggerDevice, error)
	FindMatchingDevices(filter triggerEventFilter) ([]triggerDevice, error)
}

type triggerDevice interface {
	WaitForTrigger(chan keyEvent)
	String() string
	Close()
}

type ueventConnection interface {
	Connect(mode netlink.Mode) error
	Close() error
	Monitor(queue chan netlink.UEvent, errors chan error, matcher netlink.Matcher) func(time.Duration) bool
}

var (
	// trigger mechanism
	trigger       triggerProvider
	getUEventConn = func() ueventConnection {
		return &netlink.UEventConn{}
	}

	// wait for '1' to be pressed
	triggerFilter = triggerEventFilter{Key: "KEY_1"}

	ErrTriggerNotDetected     = errors.New("trigger not detected")
	ErrNoMatchingInputDevices = errors.New("no matching input devices")
)

// Wait waits for a trigger on the available trigger devices for a given amount
// of time. Returns nil if one was detected, ErrTriggerNotDetected if timeout
// was hit, or other non-nil error.
func Wait(timeout time.Duration, deviceTimeout time.Duration) error {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGUSR1)
	conn := getUEventConn()
	mylog.Check(conn.Connect(netlink.UdevEvent))

	defer conn.Close()

	add := "add"
	matcher := &netlink.RuleDefinitions{
		Rules: []netlink.RuleDefinition{
			{
				Action: &add,
				Env: map[string]string{
					"SUBSYSTEM":         "input",
					"ID_INPUT_KEYBOARD": "1",
					"DEVNAME":           ".*",
				},
			},
		},
	}

	ueventQueue := make(chan netlink.UEvent)
	ueventErrors := make(chan error)
	conn.Monitor(ueventQueue, ueventErrors, matcher)

	if trigger == nil {
		logger.Panicf("trigger is unset")
	}

	devices := mylog.Check2(trigger.FindMatchingDevices(triggerFilter))

	if devices == nil {
		devices = make([]triggerDevice, 0)
	}

	logger.Noticef("waiting for trigger key: %v", triggerFilter.Key)

	detectKeyCh := make(chan keyEvent, len(devices))
	for _, dev := range devices {
		go dev.WaitForTrigger(detectKeyCh)
		defer dev.Close()
	}
	foundDevice := len(devices) != 0

	start := time.Now()
	for {
		timePassed := time.Now().Sub(start)
		relTimeout := timeout - timePassed
		relDeviceTimeout := deviceTimeout - timePassed
		select {
		case kev := <-detectKeyCh:
			if kev.Err != nil {
				return kev.Err
			}
			// channel got closed without an error
			logger.Noticef("%s: + got trigger key %v", kev.Dev, triggerFilter.Key)
			return nil
		case <-time.After(relTimeout):
			return ErrTriggerNotDetected
		case <-time.After(relDeviceTimeout):
			if !foundDevice {
				return ErrNoMatchingInputDevices
			}
		case uevent := <-ueventQueue:
			dev := mylog.Check2(trigger.Open(triggerFilter, uevent.Env["DEVNAME"]))

		case <-sigs:
			logger.Noticef("Switching root")
			mylog.Check(syscall.Chdir("/sysroot"))
			mylog.Check(syscall.Chroot("/sysroot"))
			mylog.Check(syscall.Chdir("/"))

		}
	}
}
