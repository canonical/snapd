// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package agent

import (
	"fmt"
	"os/user"
	"syscall"
	"time"
)

var (
	SessionInfoCmd                = sessionInfoCmd
	ServiceControlCmd             = serviceControlCmd
	PendingRefreshNotificationCmd = pendingRefreshNotificationCmd
)

func MockStopTimeouts(stop, kill time.Duration) (restore func()) {
	oldStopTimeout := stopTimeout
	stopTimeout = stop
	oldKillWait := killWait
	killWait = kill
	return func() {
		stopTimeout = oldStopTimeout
		killWait = oldKillWait
	}
}

func MockUcred(ucred *syscall.Ucred, err error) (restore func()) {
	old := sysGetsockoptUcred
	sysGetsockoptUcred = func(fd, level, opt int) (*syscall.Ucred, error) {
		return ucred, err
	}
	return func() {
		sysGetsockoptUcred = old
	}
}

func MockUserCurrent(f func() (*user.User, error)) (restore func()) {
	old := userCurrent
	userCurrent = f
	return func() {
		userCurrent = old
	}
}

// when export_test.go is included in tests, override userCurrent to return an
// error so we don't accidentally operate on the host's user dirs
func init() {
	MockUserCurrent(func() (*user.User, error) {
		return nil, fmt.Errorf("user.Current not mocked in a test yet")
	})
}
