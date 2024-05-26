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
)

func MockInput(newInput TriggerProvider) (restore func()) {
	oldInput := trigger
	trigger = newInput
	return func() {
		trigger = oldInput
	}
}

type (
	TriggerProvider         = triggerProvider
	TriggerDevice           = triggerDevice
	TriggerCapabilityFilter = triggerEventFilter
	KeyEvent                = keyEvent
)

type mockUEventConnection struct {
	events []netlink.UEvent
}

func (m *mockUEventConnection) Connect(mode netlink.Mode) error {
	return nil
}

func (m *mockUEventConnection) Close() error {
	return nil
}

func (m *mockUEventConnection) Monitor(queue chan netlink.UEvent, errors chan error, matcher netlink.Matcher) func(time.Duration) bool {
	go func() {
		for _, event := range m.events {
			queue <- event
		}
	}()
	return func(time.Duration) bool {
		return true
	}
}

func MockUEvent(events []netlink.UEvent) (restore func()) {
	oldGetUEventConn := getUEventConn
	getUEventConn = func() ueventConnection {
		return &mockUEventConnection{events}
	}

	return func() {
		getUEventConn = oldGetUEventConn
	}
}
