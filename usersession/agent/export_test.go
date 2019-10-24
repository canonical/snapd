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
	"syscall"
	"time"
)

var (
	SessionInfoCmd = sessionInfoCmd
	ServicesCmd    = servicesCmd
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
