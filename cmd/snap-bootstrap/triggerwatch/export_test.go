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
	"time"

	"github.com/snapcore/snapd/osutil/udev/netlink"
	"github.com/snapcore/snapd/testutil"
)

func MockInput(newInput TriggerProvider) (restore func()) {
	return testutil.Mock(&trigger, newInput)
}

type TriggerProvider = triggerProvider
type TriggerDevice = triggerDevice
type TriggerCapabilityFilter = triggerEventFilter
type KeyEvent = keyEvent

type mockUEventConnection struct {
	events chan netlink.UEvent
}

func (m *mockUEventConnection) Connect(mode netlink.Mode) error {
	return nil
}

func (m *mockUEventConnection) Close() error {
	return nil
}

func (m *mockUEventConnection) Monitor(queue chan netlink.UEvent, errors chan error, matcher netlink.Matcher) func(time.Duration) bool {
	go func() {
		for {
			select {
			case event := <-m.events:
				queue <- event
			}
		}
	}()
	return func(time.Duration) bool {
		return true
	}
}

func MockUEventChannel(events chan netlink.UEvent) (restore func()) {
	return testutil.Mock(&getUEventConn, func() ueventConnection {
		return &mockUEventConnection{events}
	})
}

func MockUEvent(events []netlink.UEvent) (restore func()) {
	e := make(chan netlink.UEvent)
	go func() {
		for _, event := range events {
			e <- event
		}
	}()

	return testutil.Mock(&getUEventConn, func() ueventConnection {
		return &mockUEventConnection{e}
	})
}

func MockTimeAfter(f func(d time.Duration) <-chan time.Time) (restore func()) {
	return testutil.Mock(&timeAfter, f)
}
