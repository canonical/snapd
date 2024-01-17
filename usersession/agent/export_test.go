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
)

var (
	SessionInfoCmd                = sessionInfoCmd
	ServiceControlCmd             = serviceControlCmd
	ServiceStatusCmd              = serviceStatusCmd
	PendingRefreshNotificationCmd = pendingRefreshNotificationCmd
	FinishRefreshNotificationCmd  = finishRefreshNotificationCmd
	GuessAppIcon                  = guessAppIcon
)

func MockUcred(ucred *syscall.Ucred, err error) (restore func()) {
	old := sysGetsockoptUcred
	sysGetsockoptUcred = func(fd, level, opt int) (*syscall.Ucred, error) {
		return ucred, err
	}
	return func() {
		sysGetsockoptUcred = old
	}
}

// MockNoBus temporarily unsets the D-Bus connection of a SessionAgent
func MockNoBus(agent *SessionAgent) (restore func()) {
	bus := agent.bus
	agent.bus = nil
	return func() {
		agent.bus = bus
	}
}
