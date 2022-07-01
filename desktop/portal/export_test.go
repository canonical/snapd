// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2021 Canonical Ltd
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

package portal

import (
	"os/user"
	"time"

	"github.com/snapcore/snapd/dirs"
)

const (
	DesktopPortalBusName      = desktopPortalBusName
	DesktopPortalObjectPath   = desktopPortalObjectPath
	DesktopPortalOpenURIIface = desktopPortalOpenURIIface
	DesktopPortalRequestIface = desktopPortalRequestIface

	DocumentPortalBusName    = documentPortalBusName
	DocumentPortalObjectPath = documentPortalObjectPath
	DocumentPortalIface      = documentPortalIface
)

func MockPortalTimeout(t time.Duration) (restore func()) {
	old := defaultPortalRequestTimeout
	defaultPortalRequestTimeout = t
	return func() {
		defaultPortalRequestTimeout = old
	}
}

func MockUserCurrent(mock func() (*user.User, error)) func() {
	old := userCurrent
	userCurrent = mock

	return func() { userCurrent = old }
}

func MockXdgRuntimeDir(path string) func() {
	old := dirs.XdgRuntimeDirBase
	dirs.XdgRuntimeDirBase = path
	return func() { dirs.XdgRuntimeDirBase = old }
}
